package match

import (
	"github.com/devon-caron/opensieve/tool"
)

// Segment is a single command invocation's worth of tokens, ready for
// matching. It corresponds to one pipeline stage.
type Segment struct {
	// Words is the sequence of token values (e.g., argv).
	Words []string

	// Pos is the byte offset of the first token in the original
	// input. Used for error reporting.
	Pos int
}

// Result is the successful outcome of a match. On failure the matcher
// returns a non-nil error (either *Error or Errors) and a nil result.
type Result struct {
	// Entry is the policy entry that governed this segment. For a
	// top-level command, this is the top-level entry. For a
	// subcommand invocation, this is the subcommand entry.
	Entry *tool.Command

	// Path is the dotted command path, e.g., ["git"] or ["git", "log"].
	Path []string

	// Argv is Segment.Words unchanged, provided on the result so the
	// caller has everything needed to exec in one place.
	Argv []string
}

// Matcher validates segments against a prebuilt Index.
type Matcher struct {
	idx *Index
}

// New constructs a Matcher from an already-built Index.
func New(idx *Index) *Matcher {
	return &Matcher{idx: idx}
}

// FromSpec is a convenience constructor that builds the Index from a
// spec and returns a ready-to-use Matcher. It returns an error if the
// spec is invalid.
func FromSpec(spec *tool.ToolSpec) (*Matcher, error) {
	idx, err := BuildIndex(spec)
	if err != nil {
		return nil, err
	}
	return New(idx), nil
}

// Match validates a segment against the configured policy.
//
// On success, it returns a *Result describing which policy entry
// governed the invocation. On failure, it returns a nil *Result and
// an error. When the segment has multiple violations, all of them are
// collected into a single Errors value so the caller can report the
// complete diagnosis; single-violation failures return a *Error
// directly.
func (m *Matcher) Match(seg Segment) (*Result, error) {
	if len(seg.Words) == 0 {
		return nil, &Error{
			Code: ErrEmptySegment,
			Msg:  "segment has no tokens",
		}
	}

	// Step 1: route to the top-level command entry.
	cmdName := seg.Words[0]
	top, ok := m.idx.byName[cmdName]
	if !ok {
		return nil, &Error{
			Code:    ErrCommandNotAllowed,
			Command: cmdName,
			Token:   cmdName,
			Msg:     "command is not in the allowed list",
		}
	}

	// Step 2: route to a subcommand, if applicable.
	governing := top
	path := []string{top.name}
	args := seg.Words[1:]

	if len(top.subByName) > 0 && len(args) > 0 {
		if sub, ok := top.subByName[args[0]]; ok {
			governing = sub
			path = append(path, sub.name)
			args = args[1:]
		}
		// If args[0] is not a subcommand, fall through to the parent
		// for validation. Per the policy model, an unrecognized
		// would-be subcommand is just an arg that the parent must
		// permit (or reject, in whitelist mode).
	}

	// Steps 3-5: validate args against the governing entry.
	errs := validateArgs(governing, path, args)
	if !errs.ok() {
		if len(errs) == 1 {
			return nil, errs[0]
		}
		return nil, errs
	}

	return &Result{
		Entry: governing.raw,
		Path:  path,
		Argv:  seg.Words,
	}, nil
}
