// Package match validates tokenized command segments against a tool
// policy, routing to the correct command/subcommand entry and
// checking arguments against allow/deny/required lists.
package match

import "fmt"

// ErrorCode classifies match failures so callers can switch on the
// failure mode without string matching.
type ErrorCode string

const (
	// ErrEmptySegment is returned when the matcher is handed a
	// segment with no words. This indicates an upstream bug; the
	// parser should reject empty segments before dispatch.
	ErrEmptySegment ErrorCode = "empty_segment"

	// ErrCommandNotAllowed is returned when the first token of a
	// segment does not match any configured command.
	ErrCommandNotAllowed ErrorCode = "command_not_allowed"

	// ErrArgNotAllowed is returned when an argument is not in the
	// AllowedArgs list of a whitelist-mode command. Per the policy
	// model, an unrecognized subcommand of a whitelist parent
	// produces this same error — it is, from the parent's
	// perspective, just an arg that wasn't allowed.
	ErrArgNotAllowed ErrorCode = "arg_not_allowed"

	// ErrArgDenied is returned when an argument matches an entry in
	// the DisallowedArgs list of a blacklist-mode command.
	ErrArgDenied ErrorCode = "arg_denied"

	// ErrMissingRequired is returned when a required argument is not
	// present in the segment's argv.
	ErrMissingRequired ErrorCode = "missing_required"
)

// Error is the concrete error type returned by the matcher.
//
// Multiple Errors can be combined into an Errors slice when a single
// segment has multiple violations; see Errors.
type Error struct {
	Code    ErrorCode
	Command string // dotted command path, e.g., "git" or "git log"
	Token   string // the offending argv token, if applicable
	Pattern string // the policy entry that matched (for deny) or
	//                 the required entry that wasn't satisfied
	Msg string
}

func (e *Error) Error() string {
	parts := fmt.Sprintf("match: %s", e.Code)
	if e.Command != "" {
		parts += fmt.Sprintf(" [%s]", e.Command)
	}
	if e.Token != "" {
		parts += fmt.Sprintf(" token=%q", e.Token)
	}
	if e.Pattern != "" {
		parts += fmt.Sprintf(" pattern=%q", e.Pattern)
	}
	if e.Msg != "" {
		parts += ": " + e.Msg
	}
	return parts
}

// Is allows errors.Is comparison against sentinel error-code values.
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
// and a missing required arg), the matcher collects all of them and
// returns an Errors. This gives the agent complete information about
// what needs to be fixed, rather than forcing it to retry iteratively
// on one error at a time.
type Errors []*Error

func (es Errors) Error() string {
	switch len(es) {
	case 0:
		return "match: no errors"
	case 1:
		return es[0].Error()
	default:
		msg := fmt.Sprintf("match: %d errors:", len(es))
		for _, e := range es {
			msg += "\n  - " + e.Error()
		}
		return msg
	}
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
