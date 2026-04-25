package match

import (
	"strings"

	"github.com/devon-caron/opensieve/lex"
	"github.com/devon-caron/opensieve/tool"
)

// Segment is a single command invocation's worth of tokens, ready
// for matching. It corresponds to one pipeline stage.
//
// Tokens carries lex word tokens only (pipe/EOF tokens are split
// out upstream by the orchestrator). Each token preserves its byte
// offset in the original input so error messages can pinpoint the
// exact failing position. Raw is the original input slice for this
// segment, provided for downstream caret-rendering — the matcher
// itself does not read it.
type Segment struct {
	Tokens []lex.Token
	Raw    string
}

// Result is the successful outcome of a match. On failure the matcher
// returns a non-nil error (either *Error or Errors) and a nil result.
type Result struct {
	// Entry is the policy entry that governed this segment. For a
	// top-level command this is the top-level entry; for a
	// subcommand invocation, the subcommand entry.
	Entry *tool.Command

	// Path is the dotted command path, e.g., ["git"] or ["git", "log"].
	Path []string

	// Argv is the segment's token values, in order, for callers that
	// need a flat string slice for exec.
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
// spec and returns a ready-to-use Matcher. It returns an error if
// the spec is invalid.
func FromSpec(spec *tool.ToolSpec) (*Matcher, error) {
	idx, err := BuildIndex(spec)
	if err != nil {
		return nil, err
	}
	return New(idx), nil
}

// Match validates a segment against the configured policy.
//
// On success returns a *Result describing which policy entry governed
// the invocation. On failure returns a nil *Result plus an error.
//
// Routing-stage failures (unknown top-level command, or any token at
// an intermediate level that doesn't name a known subcommand) return
// a single *Error, fail-fast. There is no per-level pass-through:
// strict routing requires every intermediate token to be a known sub.
//
// Validation-stage failures run only against the deepest matched
// (leaf) entry: per-token allowed/disallowed checks plus the
// missing-required check. The leaf's effective denylist is its own
// DisallowedArgs unioned with every ancestor's
// SubcommandsConfig.DisallowedSubArgs (recursive). All violations
// from this stage are collected into an Errors so the caller can
// show the agent every problem at once. A single-violation result is
// promoted to *Error so callers can errors.As(*Error{}) for the
// common case.
func (m *Matcher) Match(seg Segment) (*Result, error) {
	if len(seg.Tokens) == 0 {
		return nil, &Error{Code: ErrEmptySegment}
	}

	// Step 1: route to the top-level command entry.
	cmdTok := seg.Tokens[0]
	top, ok := m.idx.byName[cmdTok.Value]
	if !ok {
		return nil, &Error{
			Code:    ErrCommandNotAllowed,
			Token:   cmdTok.Value,
			Pos:     cmdTok.Pos,
			Allowed: append([]string(nil), m.idx.names...),
		}
	}

	// Step 2: route to the deepest subcommand match.
	//
	// At every level that has children, the next token MUST be a
	// known sub. There's no "skip past parent flags" — if you want
	// to permit a parent flag, model it as a sub of its own (see
	// `--no-pager` in read_tool.yaml). The tradeoff of this strict
	// routing is that intermediate levels' allowed/disallowed/
	// required_args are dead config; only the leaf entry validates.
	// In return, every accepted command is one whose full prefix
	// path was explicitly listed in the policy.
	//
	// We descend while the current entry has subs and the args
	// aren't exhausted. The loop terminates when we hit a leaf or
	// run out of tokens.
	governing := top
	path := []string{top.name}
	args := seg.Tokens[1:]

	for len(governing.subByName) > 0 && len(args) > 0 {
		tok := args[0]
		sub, ok := governing.subByName[tok.Value]
		if !ok {
			return nil, &Error{
				Code:    ErrArgNotAllowed,
				Command: strings.Join(path, " "),
				Token:   tok.Value,
				Pos:     tok.Pos,
				Subs:    append([]string(nil), governing.subNames...),
			}
		}
		path = append(path, sub.name)
		args = args[1:]
		governing = sub
	}

	// Step 3: validate args against the governing entry.
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
		Argv:  tokensToValues(seg.Tokens),
	}, nil
}

func tokensToValues(toks []lex.Token) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = t.Value
	}
	return out
}
