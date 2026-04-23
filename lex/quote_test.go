package lex

import (
	"errors"
	"strings"
	"testing"
)

// TestTokenize_QuoteValid exhaustively verifies that legitimate quoted
// inputs tokenize as expected. The focus is on cases where a bug in
// quote handling would produce a silently-wrong token rather than an
// error — those are the dangerous regressions.
func TestTokenize_QuoteValid(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []Token
	}{
		// ---- Basic shapes ----
		{
			name:  "double-quoted single word",
			input: `"hello"`,
			want: []Token{
				{Kind: TokWord, Value: "hello", Pos: 0},
				{Kind: TokEOF, Value: "", Pos: 7},
			},
		},
		{
			name:  "single-quoted single word",
			input: `'hello'`,
			want: []Token{
				{Kind: TokWord, Value: "hello", Pos: 0},
				{Kind: TokEOF, Value: "", Pos: 7},
			},
		},
		{
			name:  "empty double-quoted string",
			input: `""`,
			want: []Token{
				{Kind: TokWord, Value: "", Pos: 0},
				{Kind: TokEOF, Value: "", Pos: 2},
			},
		},
		{
			name:  "empty single-quoted string",
			input: `''`,
			want: []Token{
				{Kind: TokWord, Value: "", Pos: 0},
				{Kind: TokEOF, Value: "", Pos: 2},
			},
		},
		{
			name:  "quoted string with only spaces",
			input: `"   "`,
			want: []Token{
				{Kind: TokWord, Value: "   ", Pos: 0},
				{Kind: TokEOF, Value: "", Pos: 5},
			},
		},
		{
			name:  "quoted string with only tab",
			input: `"` + "\t" + `"`,
			want: []Token{
				{Kind: TokWord, Value: "\t", Pos: 0},
				{Kind: TokEOF, Value: "", Pos: 3},
			},
		},

		// ---- Quote as isolator: the primary purpose ----
		{
			name:  "pipe inside double quotes is literal",
			input: `rg "a|b"`,
			want: []Token{
				{Kind: TokWord, Value: "rg", Pos: 0},
				{Kind: TokWord, Value: "a|b", Pos: 3},
				{Kind: TokEOF, Value: "", Pos: 8},
			},
		},
		{
			name:  "multiple pipes inside quotes",
			input: `rg "a|b|c|d"`,
			want: []Token{
				{Kind: TokWord, Value: "rg", Pos: 0},
				{Kind: TokWord, Value: "a|b|c|d", Pos: 3},
				{Kind: TokEOF, Value: "", Pos: 12},
			},
		},
		{
			name:  "pipe outside quotes after quoted word",
			input: `rg "a|b" | wc`,
			want: []Token{
				{Kind: TokWord, Value: "rg", Pos: 0},
				{Kind: TokWord, Value: "a|b", Pos: 3},
				{Kind: TokPipe, Value: "|", Pos: 9},
				{Kind: TokWord, Value: "wc", Pos: 11},
				{Kind: TokEOF, Value: "", Pos: 13},
			},
		},
		{
			name:  "pipe immediately after closing quote",
			input: `rg "foo"|wc`,
			want: []Token{
				{Kind: TokWord, Value: "rg", Pos: 0},
				{Kind: TokWord, Value: "foo", Pos: 3},
				{Kind: TokPipe, Value: "|", Pos: 8},
				{Kind: TokWord, Value: "wc", Pos: 9},
				{Kind: TokEOF, Value: "", Pos: 11},
			},
		},

		// ---- Otherwise-forbidden chars are literal inside quotes ----
		// This is a contract: the lexer deliberately permits these
		// inside quoted content because the whole point of quoting
		// is to pass them through opaquely. The *validator* or the
		// *command policy* is responsible for deciding whether the
		// resulting token is acceptable — not the lexer.
		{
			name:  "dollar inside double quotes",
			input: `echo "$HOME"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "$HOME", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 12},
			},
		},
		{
			name:  "dollar paren inside double quotes",
			input: `echo "$(ls)"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "$(ls)", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 12},
			},
		},
		{
			name:  "backticks inside double quotes",
			input: "echo \"`ls`\"",
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "`ls`", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 11},
			},
		},
		{
			name:  "backslash inside double quotes",
			input: `echo "a\b"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: `a\b`, Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 10},
			},
		},
		{
			name:  "semicolon inside double quotes",
			input: `echo "a;b"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "a;b", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 10},
			},
		},
		{
			name:  "ampersand inside double quotes",
			input: `echo "a&&b"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "a&&b", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 11},
			},
		},
		{
			name:  "redirect chars inside double quotes",
			input: `echo "a>b<c"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "a>b<c", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 12},
			},
		},
		{
			name:  "parens inside double quotes",
			input: `echo "(a)(b)"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "(a)(b)", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 13},
			},
		},
		{
			name:  "braces inside double quotes",
			input: `echo "{a,b}"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "{a,b}", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 12},
			},
		},
		{
			name:  "glob chars inside double quotes",
			input: `echo "*.go ?.go [abc]"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "*.go ?.go [abc]", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 22},
			},
		},
		{
			name:  "comment char inside quotes",
			input: `echo "a#b"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "a#b", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 10},
			},
		},
		{
			name:  "bang inside quotes",
			input: `echo "a!b"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "a!b", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 10},
			},
		},
		{
			name:  "tilde inside quotes",
			input: `echo "~/foo"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "~/foo", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 12},
			},
		},
		{
			name:  "kitchen sink of metacharacters",
			input: `echo "$ \ * ? ~ ( ) { } [ ] ! # & ; < >"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: `$ \ * ? ~ ( ) { } [ ] ! # & ; < >`, Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 40},
			},
		},

		// ---- Single-quote isolation properties ----
		// Single and double quotes are equivalent in this lexer:
		// both are literal. These tests lock in that equivalence so
		// a future change that tries to make "double-quote = expand,
		// single-quote = literal" POSIX semantics is caught loudly.
		{
			name:  "dollar inside single quotes",
			input: `echo '$HOME'`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "$HOME", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 12},
			},
		},
		{
			name:  "double quote inside single quotes",
			input: `echo 'a"b"c'`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: `a"b"c`, Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 12},
			},
		},
		{
			name:  "single quote inside double quotes",
			input: `echo "a'b'c"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "a'b'c", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 12},
			},
		},

		// ---- Adjacent quoted tokens ----
		{
			name:  "two adjacent quoted words separated by space",
			input: `"foo" "bar"`,
			want: []Token{
				{Kind: TokWord, Value: "foo", Pos: 0},
				{Kind: TokWord, Value: "bar", Pos: 6},
				{Kind: TokEOF, Value: "", Pos: 11},
			},
		},
		{
			name:  "mixed quote types in sequence",
			input: `"foo" 'bar' "baz"`,
			want: []Token{
				{Kind: TokWord, Value: "foo", Pos: 0},
				{Kind: TokWord, Value: "bar", Pos: 6},
				{Kind: TokWord, Value: "baz", Pos: 12},
				{Kind: TokEOF, Value: "", Pos: 17},
			},
		},

		// ---- UTF-8 inside quotes ----
		// Non-ASCII is forbidden in unquoted words but permitted
		// inside quoted strings, because quoted content is treated
		// as opaque literal. This matters for passing regex patterns
		// or file paths containing UTF-8 to downstream tools.
		{
			name:  "accented letter inside quotes",
			input: `grep "café" menu.txt`,
			want: []Token{
				{Kind: TokWord, Value: "grep", Pos: 0},
				{Kind: TokWord, Value: "café", Pos: 5},
				{Kind: TokWord, Value: "menu.txt", Pos: 13},
				{Kind: TokEOF, Value: "", Pos: 21},
			},
		},
		{
			name:  "cjk inside quotes",
			input: `grep "日本語" file`,
			want: []Token{
				{Kind: TokWord, Value: "grep", Pos: 0},
				{Kind: TokWord, Value: "日本語", Pos: 5},
				{Kind: TokWord, Value: "file", Pos: 17},
				{Kind: TokEOF, Value: "", Pos: 21},
			},
		},
		{
			name:  "emoji inside quotes",
			input: `echo "hello 😀 world"`,
			want: []Token{
				{Kind: TokWord, Value: "echo", Pos: 0},
				{Kind: TokWord, Value: "hello 😀 world", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 23},
			},
		},

		// ---- Realistic agent invocations ----
		{
			name:  "ripgrep alternation pattern",
			input: `rg "auth|login|session" src/`,
			want: []Token{
				{Kind: TokWord, Value: "rg", Pos: 0},
				{Kind: TokWord, Value: "auth|login|session", Pos: 3},
				{Kind: TokWord, Value: "src/", Pos: 24},
				{Kind: TokEOF, Value: "", Pos: 28},
			},
		},
		{
			name:  "grep regex with pipe then real pipe",
			input: `grep "foo|bar" file | wc -l`,
			want: []Token{
				{Kind: TokWord, Value: "grep", Pos: 0},
				{Kind: TokWord, Value: "foo|bar", Pos: 5},
				{Kind: TokWord, Value: "file", Pos: 15},
				{Kind: TokPipe, Value: "|", Pos: 20},
				{Kind: TokWord, Value: "wc", Pos: 22},
				{Kind: TokWord, Value: "-l", Pos: 25},
				{Kind: TokEOF, Value: "", Pos: 27},
			},
		},
		{
			name:  "quoted path with spaces",
			input: `cat "my file.txt"`,
			want: []Token{
				{Kind: TokWord, Value: "cat", Pos: 0},
				{Kind: TokWord, Value: "my file.txt", Pos: 4},
				{Kind: TokEOF, Value: "", Pos: 17},
			},
		},
		{
			name:  "quoted flag value with spaces",
			input: `grep --include="*.go" pattern`,
			// --include="*.go" is a single unquoted-then-quoted
			// compound token as written; the lexer splits at the
			// quote boundary. This documents that behavior.
			want: []Token{
				{Kind: TokWord, Value: "grep", Pos: 0},
				{Kind: TokWord, Value: "--include=", Pos: 5},
				{Kind: TokWord, Value: "*.go", Pos: 15},
				{Kind: TokWord, Value: "pattern", Pos: 22},
				{Kind: TokEOF, Value: "", Pos: 29},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Tokenize(tc.input)
			if err != nil {
				t.Fatalf("unexpected error for input %q: %v",
					tc.input, err)
			}
			if !tokensEqual(got, tc.want) {
				t.Errorf("input %q\n  got:  %s\n  want: %s",
					tc.input, formatTokens(got), formatTokens(tc.want))
			}
		})
	}
}

// TestTokenize_QuoteInvalid verifies all the ways quoted input can go
// wrong. Each case is a specific failure mode a bug could regress.
func TestTokenize_QuoteInvalid(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantCode ErrorCode
	}{
		// ---- Unterminated at every plausible position ----
		{
			name:     "bare opening double quote",
			input:    `"`,
			wantCode: ErrUnterminatedQuote,
		},
		{
			name:     "bare opening single quote",
			input:    `'`,
			wantCode: ErrUnterminatedQuote,
		},
		{
			name:     "opening double after whitespace",
			input:    `   "`,
			wantCode: ErrUnterminatedQuote,
		},
		{
			name:     "unterminated after command",
			input:    `ls "`,
			wantCode: ErrUnterminatedQuote,
		},
		{
			name:     "unterminated after several tokens",
			input:    `ls -la /etc "`,
			wantCode: ErrUnterminatedQuote,
		},
		{
			name:     "unterminated after pipe",
			input:    `ls | grep "`,
			wantCode: ErrUnterminatedQuote,
		},
		{
			name:     "unterminated with content",
			input:    `rg "pattern`,
			wantCode: ErrUnterminatedQuote,
		},
		{
			name: "unterminated with embedded opposite quote",
			// Inner ' is just content; outer " is never closed.
			input:    `rg "has'inside`,
			wantCode: ErrUnterminatedQuote,
		},
		{
			name:     "unterminated with embedded pipe",
			input:    `rg "a|b`,
			wantCode: ErrUnterminatedQuote,
		},
		{
			name:     "looks closed but isn't (wrong quote type)",
			input:    `rg "double' then single close`,
			wantCode: ErrUnterminatedQuote,
		},
		{
			name:     "looks closed but isn't, reversed",
			input:    `rg 'single" then double close`,
			wantCode: ErrUnterminatedQuote,
		},
		{
			name:     "trailing opened quote after valid tokens",
			input:    `cat file.txt "`,
			wantCode: ErrUnterminatedQuote,
		},
		{
			name: "unterminated with empty content",
			// Three quotes: a valid "" followed by an unterminated ".
			input:    `""" `,
			wantCode: ErrUnterminatedQuote,
		},
		{
			name:     "odd number of double quotes",
			input:    `"a" "b" "c`,
			wantCode: ErrUnterminatedQuote,
		},

		// ---- Forbidden chars inside quotes ----
		// Most metachars are allowed inside quotes; these are the
		// exceptions. Newlines, CR, and control chars are *never*
		// permitted, even quoted, to prevent line-injection and
		// smuggling of control sequences to downstream processes.
		{
			name:     "LF inside double quotes",
			input:    "rg \"line1\nline2\"",
			wantCode: ErrForbiddenChar,
		},
		{
			name:     "LF inside single quotes",
			input:    "rg 'line1\nline2'",
			wantCode: ErrForbiddenChar,
		},
		{
			name:     "CR inside double quotes",
			input:    "rg \"line1\rline2\"",
			wantCode: ErrForbiddenChar,
		},
		{
			name:     "CRLF inside quotes",
			input:    "rg \"line1\r\nline2\"",
			wantCode: ErrForbiddenChar,
		},
		{
			name:     "null byte inside quotes",
			input:    "rg \"pre\x00post\"",
			wantCode: ErrForbiddenChar,
		},
		{
			name:     "bell inside quotes",
			input:    "rg \"pre\x07post\"",
			wantCode: ErrForbiddenChar,
		},
		{
			name:     "backspace inside quotes",
			input:    "rg \"pre\x08post\"",
			wantCode: ErrForbiddenChar,
		},
		{
			name:     "vertical tab inside quotes",
			input:    "rg \"pre\x0bpost\"",
			wantCode: ErrForbiddenChar,
		},
		{
			name:     "form feed inside quotes",
			input:    "rg \"pre\x0cpost\"",
			wantCode: ErrForbiddenChar,
		},
		{
			name: "escape char inside quotes",
			// ESC is the start of ANSI escape sequences. Letting it
			// through inside a quoted argument would let an attacker
			// embed terminal escapes in tool output that the agent
			// later displays.
			input:    "rg \"pre\x1bpost\"",
			wantCode: ErrForbiddenChar,
		},
		{
			name:     "DEL inside quotes",
			input:    "rg \"pre\x7fpost\"",
			wantCode: ErrForbiddenChar,
		},
		{
			name:     "control char at very start of quoted content",
			input:    "rg \"\x01rest\"",
			wantCode: ErrForbiddenChar,
		},
		{
			name:     "control char as entire quoted content",
			input:    "rg \"\x01\"",
			wantCode: ErrForbiddenChar,
		},
		{
			name:     "control char right before closing quote",
			input:    "rg \"pre\x01\"",
			wantCode: ErrForbiddenChar,
		},

		// ---- UTF-8 validity inside quotes ----
		// Invalid UTF-8 is rejected up front by the top-level check
		// before any scanning happens, so these never reach the
		// quote logic — but the check needs to cover quoted content
		// too.
		{
			name:     "invalid UTF-8 inside quotes",
			input:    "rg \"pre\xffpost\"",
			wantCode: ErrInvalidUTF8,
		},
		{
			name:     "truncated UTF-8 sequence inside quotes",
			input:    "rg \"pre\xc3post\"",
			wantCode: ErrInvalidUTF8,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Tokenize(tc.input)
			if err == nil {
				t.Fatalf("expected error for input %q, got "+
					"tokens %s", tc.input, formatTokens(got))
			}
			if got != nil {
				t.Errorf("expected nil tokens on error, got %s",
					formatTokens(got))
			}
			var lerr *Error
			if !errors.As(err, &lerr) {
				t.Fatalf("expected *lex.Error, got %T: %v",
					err, err)
			}
			if lerr.Code != tc.wantCode {
				t.Errorf("input %q: want code %s, got %s (%v)",
					tc.input, tc.wantCode, lerr.Code, lerr)
			}
		})
	}
}

// TestTokenize_QuoteExploitScenarios exercises specific attack
// patterns. Each case is named for the vulnerability class it guards
// against. These tests document defenses and must never be weakened
// without a corresponding security review.
func TestTokenize_QuoteExploitScenarios(t *testing.T) {
	t.Run("quoted pipe cannot split a pipeline", func(t *testing.T) {
		// The agent emits a regex that contains a pipe. A naive
		// splitter that splits on '|' would produce a corrupted
		// command. Verify the entire pattern ends up as a single
		// token.
		got, err := Tokenize(`rg "evil|payload" file`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 4 { // rg, pattern, file, EOF
			t.Fatalf("expected 4 tokens, got %d: %s",
				len(got), formatTokens(got))
		}
		if got[1].Value != "evil|payload" {
			t.Errorf("pipe split the quoted pattern: got %q",
				got[1].Value)
		}
		// Confirm no TokPipe was emitted.
		for _, tok := range got {
			if tok.Kind == TokPipe {
				t.Errorf("TokPipe emitted inside quoted "+
					"content: %s", formatTokens(got))
				break
			}
		}
	})

	t.Run("quoted semicolon cannot chain commands", func(t *testing.T) {
		// Even if a future version added ';' as an operator, quoted
		// ';' must never become one. Since ';' is currently a
		// forbidden char outside quotes, this asserts it's literal
		// inside quotes.
		got, err := Tokenize(`rg "pattern;rm -rf /" src`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got[1].Value != "pattern;rm -rf /" {
			t.Errorf("semicolon handled specially: got %q",
				got[1].Value)
		}
	})

	t.Run("dollar inside quotes is literal (no expansion)", func(t *testing.T) {
		// The lexer must not interpret $HOME as a variable or strip
		// the dollar. The downstream command receives the literal
		// string "$HOME" — which the child process also will not
		// expand because we never invoke a shell.
		got, err := Tokenize(`echo "$HOME/.ssh/id_rsa"`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got[1].Value != "$HOME/.ssh/id_rsa" {
			t.Errorf("dollar treated specially in quoted content: "+
				"got %q", got[1].Value)
		}
	})

	t.Run("command sub syntax inside quotes is literal", func(t *testing.T) {
		// $(cmd) and `cmd` are expansion syntax in a real shell.
		// Our lexer treats them as opaque content inside quotes.
		// The downstream exec.Cmd receives them as literals.
		cases := []struct {
			input    string
			expected string
		}{
			{`echo "$(rm -rf /)"`, "$(rm -rf /)"},
			{"echo \"`rm -rf /`\"", "`rm -rf /`"},
			{`echo "$(cat /etc/passwd)"`, "$(cat /etc/passwd)"},
		}
		for _, c := range cases {
			got, err := Tokenize(c.input)
			if err != nil {
				t.Errorf("input %q: unexpected error: %v",
					c.input, err)
				continue
			}
			if got[1].Value != c.expected {
				t.Errorf("input %q: command-sub syntax "+
					"transformed: got %q want %q",
					c.input, got[1].Value, c.expected)
			}
		}
	})

	t.Run("no escape processing lets through backslash literally", func(t *testing.T) {
		// A POSIX-style lexer would process \" inside a double-quoted
		// string as an escape for the quote character, potentially
		// letting an attacker smuggle a closing quote. Our lexer does
		// no escape processing, so \ is a literal character and the
		// first unescaped " closes the string.
		//
		// Trace of `echo "a\"b"`:
		//   echo  -> word
		//   "     -> opens quote
		//   a\    -> content (no escape processing)
		//   "     -> closes quote; value is `a\`
		//   b     -> unquoted word
		//   "     -> opens a new quote, never closed
		// Expected result: ErrUnterminatedQuote.
		_, err := Tokenize(`echo "a\"b"`)
		var lerr *Error
		if !errors.As(err, &lerr) {
			t.Fatalf("expected *lex.Error, got %T: %v", err, err)
		}
		if lerr.Code != ErrUnterminatedQuote {
			t.Errorf("expected %s, got %s — backslash may have been "+
				"treated as escape", ErrUnterminatedQuote, lerr.Code)
		}
	})

	t.Run("escape attempt with single quotes also fails", func(t *testing.T) {
		_, err := Tokenize(`echo 'a\'b'`)
		var lerr *Error
		if !errors.As(err, &lerr) {
			t.Fatalf("expected *lex.Error, got %T: %v", err, err)
		}
		if lerr.Code != ErrUnterminatedQuote {
			t.Errorf("expected %s, got %s",
				ErrUnterminatedQuote, lerr.Code)
		}
	})

	t.Run("newline smuggling via quotes is blocked", func(t *testing.T) {
		// An attacker sending a multi-line payload via a quoted
		// string to inject a second command. Newlines are always
		// forbidden, even inside quotes.
		inputs := []string{
			"rg \"legit\ninjected_cmd\" file",
			"rg \"\ninjected\"",
			"rg \"legit\" \"\ninjected\"",
		}
		for _, input := range inputs {
			_, err := Tokenize(input)
			var lerr *Error
			if !errors.As(err, &lerr) {
				t.Errorf("input %q: expected *lex.Error, "+
					"got %T: %v", input, err, err)
				continue
			}
			if lerr.Code != ErrForbiddenChar {
				t.Errorf("input %q: newline smuggling not "+
					"blocked; got %s", input, lerr.Code)
			}
		}
	})

	t.Run("ANSI escape smuggling is blocked", func(t *testing.T) {
		// ESC (0x1b) starts ANSI escape sequences. If the agent's
		// output is displayed to a user's terminal, escape sequences
		// in tool output could be used to forge UI, move cursor, or
		// clear screen. Block at the source.
		_, err := Tokenize("echo \"\x1b[31mRED\x1b[0m\"")
		var lerr *Error
		if !errors.As(err, &lerr) {
			t.Fatalf("expected *lex.Error, got %T: %v", err, err)
		}
		if lerr.Code != ErrForbiddenChar {
			t.Errorf("ANSI escape not blocked inside quotes; "+
				"got %s", lerr.Code)
		}
	})

	t.Run("null byte smuggling is blocked", func(t *testing.T) {
		// C string truncation: many downstream tools treat \x00 as
		// end-of-string even in Go-land libraries that call into C.
		// An argument like "safe\x00dangerous" could look safe to a
		// validator but be interpreted differently by the child.
		_, err := Tokenize("cat \"safe\x00/etc/passwd\"")
		var lerr *Error
		if !errors.As(err, &lerr) {
			t.Fatalf("expected *lex.Error, got %T: %v", err, err)
		}
		if lerr.Code != ErrForbiddenChar {
			t.Errorf("null byte not blocked inside quotes; "+
				"got %s", lerr.Code)
		}
	})

	t.Run("homoglyph in quoted content is allowed but token-isolated", func(t *testing.T) {
		// Non-ASCII is permitted inside quotes because it's opaque
		// content. This test confirms the lexer does NOT reject —
		// it's up to the argument validator / command policy to
		// decide what to do with UTF-8 content. Documented behavior.
		got, err := Tokenize(`grep "сat" file`) // Cyrillic 'с'
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The quoted token should contain the Cyrillic char exactly.
		if got[1].Value != "сat" {
			t.Errorf("homoglyph content altered: got %q",
				got[1].Value)
		}
	})

	t.Run("quote cannot hide length-limit bypass", func(t *testing.T) {
		// A very long quoted string still counts toward MaxInputLen.
		// The length check runs before any scanning.
		input := `ls "` + strings.Repeat("a", MaxInputLen) + `"`
		_, err := Tokenize(input)
		var lerr *Error
		if !errors.As(err, &lerr) {
			t.Fatalf("expected *lex.Error, got %T: %v", err, err)
		}
		if lerr.Code != ErrInputTooLong {
			t.Errorf("length limit bypassed via quotes; got %s",
				lerr.Code)
		}
	})

	t.Run("empty quoted string produces empty-string token", func(t *testing.T) {
		// Worth pinning: "" is a valid token with Value="". A
		// validator treating empty-string arguments as "flag not
		// present" would be buggy, but that's not the lexer's job —
		// the lexer must produce a faithful token.
		got, err := Tokenize(`cmd "" arg`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 4 {
			t.Fatalf("expected 4 tokens, got %d: %s",
				len(got), formatTokens(got))
		}
		if got[1].Kind != TokWord || got[1].Value != "" {
			t.Errorf("empty quoted token malformed: got %+v",
				got[1])
		}
	})

	t.Run("quote at exact boundary positions", func(t *testing.T) {
		// Exercises the edges: quote at position 0, quote at the
		// very last byte, quote immediately before EOF.
		cases := []struct {
			name  string
			input string
			ok    bool
		}{
			{"quote at position 0 closed", `"x"`, true},
			{"quote at end of input unterminated", `x "`, false},
			{"quote followed immediately by EOF closed", `"x"`, true},
		}
		for _, c := range cases {
			_, err := Tokenize(c.input)
			if c.ok && err != nil {
				t.Errorf("%s: unexpected error: %v", c.name, err)
			}
			if !c.ok && err == nil {
				t.Errorf("%s: expected error, got nil", c.name)
			}
		}
	})

	t.Run("re-tokenization would require a second pass", func(t *testing.T) {
		// Sanity check: whatever Token.Value the lexer produces
		// should be treated as opaque by downstream. If some future
		// change accidentally re-lexed token values, a quoted
		// "a|b" could produce a pipe operator at the parse stage.
		// Here we just verify the value comes out intact — a second
		// pass would split it.
		got, err := Tokenize(`rg "a|b|c|d|e" src`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got[1].Kind != TokWord {
			t.Fatalf("expected TokWord for quoted content, got %s",
				got[1].Kind)
		}
		if got[1].Value != "a|b|c|d|e" {
			t.Errorf("quoted pattern split somewhere: got %q",
				got[1].Value)
		}
	})
}
