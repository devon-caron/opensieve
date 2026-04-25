package lex

import "fmt"

// ErrorCode classifies lex failures so callers can switch on the
// failure mode without string matching.
type ErrorCode string

const (
	// ErrForbiddenChar is returned when the input contains a
	// character rejected by the character-level policy.
	ErrForbiddenChar ErrorCode = "forbidden_char"

	// ErrUnterminatedQuote is returned when a quoted string has no
	// matching closing quote before end-of-input.
	ErrUnterminatedQuote ErrorCode = "unterminated_quote"

	// ErrEmptyInput is returned when the input contains no tokens
	// (empty string, or only whitespace).
	ErrEmptyInput ErrorCode = "empty_input"

	// ErrInputTooLong is returned when the input exceeds MaxInputLen.
	ErrInputTooLong ErrorCode = "input_too_long"

	// ErrInvalidUTF8 is returned when the input contains a byte
	// sequence that is not valid UTF-8.
	ErrInvalidUTF8 ErrorCode = "invalid_utf8"
)

// Error is the concrete error type returned by Tokenize.
//
// It carries a machine-readable Code, the byte position of the
// offending rune in the original input, and the offending rune
// itself (zero if not applicable).
type Error struct {
	Code ErrorCode
	Pos  int
	Rune rune
	Msg  string
}

func (e *Error) Error() string {
	if e.Rune != 0 {
		return fmt.Sprintf("lex: %s at position %d (%q): %s",
			e.Code, e.Pos, e.Rune, e.Msg)
	}
	return fmt.Sprintf("lex: %s at position %d: %s",
		e.Code, e.Pos, e.Msg)
}

// Is allows errors.Is to compare against sentinel error-code values.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// newErr constructs an Error at the given byte offset.
func newErr(code ErrorCode, pos int, r rune, msg string) *Error {
	return &Error{Code: code, Pos: pos, Rune: r, Msg: msg}
}
