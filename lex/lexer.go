package lex

import (
	"strings"
	"unicode/utf8"
)

// MaxInputLen is the maximum accepted length of an input string, in
// bytes. Anything longer is rejected without being scanned.
//
// Agent-emitted commands are rarely more than a few hundred bytes;
// 16 KiB is generous and provides a defense against accidental or
// adversarial pathological inputs.
const MaxInputLen = 16 * 1024

// Tokenize converts a command string into a slice of Tokens terminated
// by a TokEOF token.
//
// The returned slice always ends with TokEOF on success. On failure
// it returns a nil slice and a *lex.Error describing the problem.
func Tokenize(input string) ([]Token, error) {
	if len(input) == 0 {
		return nil, newErr(ErrEmptyInput, 0, 0, "input is empty")
	}
	if len(input) > MaxInputLen {
		return nil, newErr(ErrInputTooLong, 0, 0,
			"input exceeds maximum length")
	}
	if !utf8.ValidString(input) {
		return nil, newErr(ErrInvalidUTF8, 0, 0,
			"input is not valid UTF-8")
	}

	l := &lexer{input: input}
	if err := l.run(); err != nil {
		return nil, err
	}
	if !l.hasContent {
		return nil, newErr(ErrEmptyInput, 0, 0,
			"input contains only whitespace")
	}
	l.emit(TokEOF, "", len(input))
	return l.tokens, nil
}

// lexer holds the mutable scan state. It is not exported; all
// interaction goes through Tokenize.
type lexer struct {
	input      string
	pos        int // byte offset of the next rune to examine
	tokens     []Token
	hasContent bool // set once any non-whitespace has been seen
}

// run drives the scan to completion.
func (l *lexer) run() error {
	for l.pos < len(l.input) {
		r, size := utf8.DecodeRuneInString(l.input[l.pos:])

		switch {
		case isSeparator(r):
			l.pos += size

		case isPipe(r):
			l.emit(TokPipe, "|", l.pos)
			l.pos += size
			l.hasContent = true

		case isQuote(r):
			if err := l.scanQuoted(r); err != nil {
				return err
			}
			l.hasContent = true

		case isForbidden(r):
			return newErr(ErrForbiddenChar, l.pos, r,
				forbiddenReason(r))

		case isWordChar(r):
			l.scanUnquotedWord()
			l.hasContent = true

		default:
			// Any rune that isn't a separator, operator, quote,
			// explicitly-forbidden char, or word char falls here.
			// Treat it as forbidden so the policy is default-deny.
			return newErr(ErrForbiddenChar, l.pos, r,
				"character is not permitted in commands")
		}
	}
	return nil
}

// scanUnquotedWord consumes contiguous word characters starting at
// l.pos and emits a single TokWord. l.pos is advanced past the word.
func (l *lexer) scanUnquotedWord() {
	start := l.pos
	var sb strings.Builder
	for l.pos < len(l.input) {
		r, size := utf8.DecodeRuneInString(l.input[l.pos:])
		if !isWordChar(r) {
			break
		}
		sb.WriteRune(r)
		l.pos += size
	}
	l.emit(TokWord, sb.String(), start)
}

// scanQuoted consumes a quoted string delimited by openQuote. The
// opening quote is at l.pos on entry and is consumed. The closing
// quote must match the opening quote exactly.
//
// Quoted content is literal: no escape processing, no variable
// expansion, no interpretation of metacharacters. The only character
// that cannot appear inside a quoted string is the matching closing
// quote itself (to close the string) and the newline/CR (to prevent
// multi-line injection).
func (l *lexer) scanQuoted(openQuote rune) error {
	start := l.pos
	// Consume the opening quote.
	_, size := utf8.DecodeRuneInString(l.input[l.pos:])
	l.pos += size

	var sb strings.Builder
	for l.pos < len(l.input) {
		r, size := utf8.DecodeRuneInString(l.input[l.pos:])

		// Newlines are never permitted, even inside quotes.
		if r == '\n' || r == '\r' {
			return newErr(ErrForbiddenChar, l.pos, r,
				"newlines are not permitted inside quoted strings")
		}

		// Null bytes and other control chars are also rejected.
		if r < 0x20 && r != '\t' {
			return newErr(ErrForbiddenChar, l.pos, r,
				"control characters are not permitted inside "+
					"quoted strings")
		}
		if r == 0x7f {
			return newErr(ErrForbiddenChar, l.pos, r,
				"DEL character is not permitted inside "+
					"quoted strings")
		}

		if r == openQuote {
			// Consume the closing quote and emit the accumulated
			// content as a single word token.
			l.pos += size
			l.emit(TokWord, sb.String(), start)
			return nil
		}

		sb.WriteRune(r)
		l.pos += size
	}

	return newErr(ErrUnterminatedQuote, start, openQuote,
		"quoted string has no closing quote")
}

// emit appends a token to the output slice.
func (l *lexer) emit(kind TokenKind, value string, pos int) {
	l.tokens = append(l.tokens, Token{
		Kind:  kind,
		Value: value,
		Pos:   pos,
	})
}

// forbiddenReason returns a user-facing description of why a specific
// forbidden character is rejected, to make error messages actionable.
func forbiddenReason(r rune) string {
	switch r {
	case '$':
		return "variable expansion is not supported ($ is reserved)"
	case '`':
		return "command substitution is not supported"
	case '<', '>':
		return "redirection is not supported"
	case '(', ')':
		return "subshells and grouping are not supported"
	case '{', '}':
		return "brace expansion and grouping are not supported"
	case '*', '?', '~', '[', ']':
		return "glob expansion is not supported; " +
			"use explicit paths"
	case '\\':
		return "escape sequences are not supported"
	case '!':
		return "history expansion is not supported"
	case '#':
		return "comments are not supported"
	case '&':
		return "background jobs and && are not supported"
	case ';':
		return "command sequencing with ; is not supported"
	case '\n':
		return "newlines are not permitted"
	case '\r':
		return "carriage returns are not permitted"
	}
	if r < 0x20 || r == 0x7f {
		return "control characters are not permitted"
	}
	return "character is not permitted in commands"
}
