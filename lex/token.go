// Package lex tokenizes agent-supplied command strings into a stream
// of typed tokens (words and the pipe operator), rejecting any
// character that falls outside the restricted grammar.
//
// The grammar supported:
//
//	pipeline := segment ('|' segment)*
//	segment  := word+
//	word     := unquoted_word | quoted_word
//
// Only the pipe operator is recognized. Semicolons, ampersands,
// redirections, substitutions, globs, and other shell metacharacters
// are hard-rejected at the character level.
package lex

// TokenKind identifies the syntactic role of a token.
type TokenKind int

const (
	// TokWord is a command name, flag, or argument. Quoted content is
	// emitted as a single TokWord with the quote characters stripped.
	TokWord TokenKind = iota

	// TokPipe is the '|' operator separating pipeline segments.
	TokPipe

	// TokEOF signals the end of the input. Always the final token.
	TokEOF
)

// String returns a human-readable name for the kind, for use in
// error messages and tests.
func (k TokenKind) String() string {
	switch k {
	case TokWord:
		return "word"
	case TokPipe:
		return "pipe"
	case TokEOF:
		return "eof"
	default:
		return "unknown"
	}
}

// Token is a single lexical unit produced by the lexer.
//
// Pos is the byte offset of the token's start in the original input,
// useful for error messages that want to point at the offending
// position.
type Token struct {
	Kind  TokenKind
	Value string
	Pos   int
}
