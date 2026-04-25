package lex

// isWordChar reports whether r is permitted inside an unquoted word.
//
// The set is deliberately narrow: letters, digits, and a small set of
// punctuation that appears in paths, flags, and common argument
// values. Anything outside this set must either be a recognized
// separator (whitespace, '|') or appear inside a quoted string.
func isWordChar(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	}
	switch r {
	// Path and identifier punctuation
	case '-', '_', '.', '/', '=', '+', ':', ',', '@', '%':
		return true
	}
	return false
}

// isSeparator reports whether r ends the current word without being
// itself a token.
func isSeparator(r rune) bool {
	return r == ' ' || r == '\t'
}

// isPipe reports whether r is the pipe operator.
func isPipe(r rune) bool {
	return r == '|'
}

// isQuote reports whether r opens or closes a quoted string.
//
// Both single and double quotes are accepted as opening delimiters.
// Quoted content is treated literally; no escape processing or
// variable expansion is performed.
func isQuote(r rune) bool {
	return r == '\'' || r == '"'
}

// isForbidden reports whether r is a character that must cause a
// lex-time rejection regardless of context.
//
// These are characters with shell metacharacter semantics that the
// parser does not support and must never silently pass through:
//
//   - Command substitution:     $ `
//   - Redirection:              < >
//   - Grouping / subshells:     ( ) { }
//   - Globbing:                 * ? ~ [ ]
//   - Escaping:                 \
//   - History / pager escape:   !
//   - Comments:                 #
//   - Background jobs:          &
//   - Statement separator:      ;
//   - Control characters and DEL
//
// Newlines and carriage returns are also forbidden; a command must
// fit on one line.
func isForbidden(r rune) bool {
	switch r {
	case '$', '`',
		'<', '>',
		'(', ')', '{', '}',
		'*', '?', '~', '[', ']',
		'\\',
		'!', '#',
		'&', ';',
		'\n', '\r':
		return true
	}
	// Reject other control characters and DEL. Tab and space are
	// handled separately as separators; they are the only whitespace
	// we accept inline.
	if r < 0x20 && r != '\t' {
		return true
	}
	if r == 0x7f {
		return true
	}
	return false
}
