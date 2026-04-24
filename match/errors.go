// Package match validates tokenized command segments against a tool
// policy. It uses strict subcommand routing — at every level that has
// children, the next token must be a known subcommand or matching
// fails fast — and runs argument validation only against the deepest
// matched (leaf) entry. The one cross-cutting rule is
// SubcommandsConfig.DisallowedSubArgs, which is inherited recursively
// into every descendant's effective denylist.
//
// Errors emitted by this package are intentionally rich. The intended
// audience is an AI agent that needs to fix the command in one shot
// without external context: every Error carries the offending token's
// byte position in the original input, a provenance string back to
// the YAML rule that fired, the entry's allowed/sub lists (where
// useful), and a one-line "fix" suggestion. Error() renders all of
// this as a multi-line, human-readable diagnosis.
package match

import (
	"fmt"
	"strings"
)

// ErrorCode classifies match failures so callers can switch on the
// failure mode without string matching.
type ErrorCode string

const (
	// ErrEmptySegment is returned when the matcher is handed a
	// segment with no word tokens. This usually indicates an upstream
	// bug (e.g., a stray pipe `|` produced an empty pipeline stage).
	ErrEmptySegment ErrorCode = "empty_segment"

	// ErrCommandNotAllowed is returned when the first token of a
	// segment does not match any configured top-level command.
	ErrCommandNotAllowed ErrorCode = "command_not_allowed"

	// ErrArgNotAllowed covers two cases that share the same shape:
	//   1. Routing-stage: a token sits where a subcommand was
	//      expected but isn't one of the parent's known children.
	//      Subs is populated; Allowed is not (intermediate levels
	//      have no allowed-args check under strict routing).
	//   2. Validation-stage: at a whitelist-mode leaf, a token isn't
	//      in the leaf's AllowedArgs list. Allowed is populated;
	//      Subs is not.
	// Inspect Subs vs Allowed to disambiguate.
	ErrArgNotAllowed ErrorCode = "arg_not_allowed"

	// ErrArgDenied is returned when an argument matches an entry in
	// the leaf's effective DisallowedArgs (own + every ancestor's
	// SubcommandsConfig.DisallowedSubArgs).
	ErrArgDenied ErrorCode = "arg_denied"

	// ErrMissingRequired is returned when a required argument
	// declared on the leaf entry is not present anywhere in the
	// leaf's argv.
	ErrMissingRequired ErrorCode = "missing_required"
)

// Error is the concrete error type returned by the matcher.
//
// All fields are optional — only the ones relevant to the failure
// mode are populated. Error() handles missing fields gracefully and
// only renders the lines it has data for.
type Error struct {
	Code ErrorCode

	// Command is the dotted command path that governed when the
	// failure was detected. "git" for a top-level failure;
	// "git log" for a subcommand failure.
	Command string

	// Token is the offending argv token's value, when applicable.
	Token string

	// Pos is the byte offset of Token in the original input string.
	// Zero when no specific token is implicated (e.g., missing
	// required arg).
	Pos int

	// Pattern is a human-readable description of the rule that
	// matched (for denials) or the required pattern that wasn't
	// satisfied (for missing). Examples: `exact "--textconv"`,
	// `regex "^--output(=.*)?$"`, `path "/etc/*"`.
	Pattern string

	// Source is the YAML provenance string of the rule that fired,
	// e.g., `git.subcommands.disallowed_sub_args[1] (inherited)`.
	// Empty when the failure isn't traceable to a single rule (e.g.,
	// command_not_allowed).
	Source string

	// Allowed is rendered as the "allowed:" line and lists the
	// human-readable patterns the failing token *could* have matched.
	// Populated for ErrCommandNotAllowed (top-level command names)
	// and for ErrArgNotAllowed at a whitelist leaf (the leaf's
	// allowed_args). Empty for routing-stage ErrArgNotAllowed —
	// intermediate levels don't run an allowed_args check.
	Allowed []string

	// Subs is rendered as the "subs:" line and lists the entry's
	// known subcommand names. Populated only for routing-stage
	// ErrArgNotAllowed, where it tells the caller which tokens
	// *would* have been accepted at this position.
	Subs []string

	// Reason overrides the generic per-code reason when set. Use it
	// to attach a policy-specific explanation (e.g., why a particular
	// flag is dangerous).
	Reason string
}

func (e *Error) Error() string {
	var b strings.Builder

	// Header: "match: <code> for command "<path>""
	b.WriteString("match: ")
	b.WriteString(string(e.Code))
	if e.Command != "" {
		b.WriteString(` for command "`)
		b.WriteString(e.Command)
		b.WriteString(`"`)
	}

	// Body lines, each "  <label>: <value>". Keep label widths
	// consistent so the output stays scannable.
	if e.Token != "" || e.Pos != 0 {
		b.WriteString("\n  token:   ")
		fmt.Fprintf(&b, "%q at byte %d", e.Token, e.Pos)
	}
	if e.Source != "" || e.Pattern != "" {
		b.WriteString("\n  rule:    ")
		switch {
		case e.Source != "" && e.Pattern != "":
			fmt.Fprintf(&b, "%s — %s", e.Source, e.Pattern)
		case e.Source != "":
			b.WriteString(e.Source)
		default:
			b.WriteString(e.Pattern)
		}
	}
	if reason := e.reason(); reason != "" {
		b.WriteString("\n  reason:  ")
		b.WriteString(reason)
	}
	if len(e.Allowed) > 0 {
		b.WriteString("\n  allowed: ")
		b.WriteString(strings.Join(e.Allowed, ", "))
	}
	if len(e.Subs) > 0 {
		b.WriteString("\n  subs:    ")
		b.WriteString(strings.Join(e.Subs, ", "))
	}
	if fix := e.fix(); fix != "" {
		b.WriteString("\n  fix:     ")
		b.WriteString(fix)
	}

	return b.String()
}

// reason returns the explanation rendered as the "reason:" line. If
// the caller set a custom Reason, it wins; otherwise we synthesize
// a generic one from the code.
func (e *Error) reason() string {
	if e.Reason != "" {
		return e.Reason
	}
	switch e.Code {
	case ErrEmptySegment:
		return "the matcher received a segment with no tokens."
	case ErrCommandNotAllowed:
		return fmt.Sprintf("%q is not a configured top-level command.", e.Token)
	case ErrArgNotAllowed:
		if len(e.Subs) > 0 {
			return fmt.Sprintf(
				"%q is not in %s's allowed args list and is not an allowed subcommand of %s.",
				e.Token, e.Command, e.Command)
		}
		return fmt.Sprintf("%q is not in %s's allowed args list.",
			e.Token, e.Command)
	case ErrArgDenied:
		return fmt.Sprintf("%q is denied by the policy.", e.Token)
	case ErrMissingRequired:
		return fmt.Sprintf(
			"a token matching %s must be present somewhere in the command.",
			e.Pattern)
	}
	return ""
}

// fix returns the suggested remediation rendered as the "fix:" line.
func (e *Error) fix() string {
	switch e.Code {
	case ErrEmptySegment:
		return ""
	case ErrCommandNotAllowed:
		return "use one of the allowed commands above."
	case ErrArgNotAllowed:
		if len(e.Subs) > 0 {
			return fmt.Sprintf(
				"replace %q with one of the allowed args or known subcommands above.",
				e.Token)
		}
		return fmt.Sprintf(
			"remove %q or replace with one of the allowed args above.",
			e.Token)
	case ErrArgDenied:
		return fmt.Sprintf("remove %q.", e.Token)
	case ErrMissingRequired:
		return fmt.Sprintf("add a token matching %s to the command.", e.Pattern)
	}
	return ""
}

// Is allows errors.Is comparison against sentinel error-code values:
//
//	errors.Is(err, &match.Error{Code: match.ErrArgDenied})
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// Errors is a collection of match errors from a single segment.
//
// When a segment has multiple violations (e.g., two disallowed flags
// and a missing required arg), the matcher collects all of them into
// an Errors so the agent can fix everything in one round-trip rather
// than retrying one-at-a-time.
//
// Single-violation results are returned as a *Error directly, never
// as an Errors of length one.
type Errors []*Error

func (es Errors) Error() string {
	switch len(es) {
	case 0:
		return "match: no errors"
	case 1:
		return es[0].Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "match: %d errors:", len(es))
	for i, e := range es {
		fmt.Fprintf(&b, "\n  %d. ", i+1)
		// Indent the child's multi-line output by 4 spaces past the
		// "  N. " prefix so the labels still line up under "match:".
		child := e.Error()
		child = strings.ReplaceAll(child, "\n", "\n     ")
		b.WriteString(child)
	}
	return b.String()
}

// Is returns true if any constituent error matches the target.
func (es Errors) Is(target error) bool {
	for _, e := range es {
		if e.Is(target) {
			return true
		}
	}
	return false
}

// ok reports whether the slice is empty. An empty Errors is a
// successful match; a non-empty one is a failure.
func (es Errors) ok() bool { return len(es) == 0 }
