package lex

import (
	"errors"
	"strings"
	"testing"
)

func TestTokenize_ValidInputs(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			name:  "single word",
			input: "ls",
			want: []Token{
				{Kind: TokWord, Value: "ls", Pos: 0},
				{Kind: TokEOF, Value: "", Pos: 2},
			},
		},
		{
			name:  "command with flag",
			input: "ls -la",
			want: []Token{
				{Kind: TokWord, Value: "ls", Pos: 0},
				{Kind: TokWord, Value: "-la", Pos: 3},
				{Kind: TokEOF, Value: "", Pos: 6},
			},
		},
		{
			name:  "leading whitespace",
			input: "   ls",
			want: []Token{
				{Kind: TokWord, Value: "ls", Pos: 3},
				{Kind: TokEOF, Value: "", Pos: 5},
			},
		},
		{
			name:  "trailing whitespace",
			input: "ls   ",
			want: []Token{
				{Kind: TokWord, Value: "ls", Pos: 0},
				{Kind: TokEOF, Value: "", Pos: 5},
			},
		},
		{
			name:  "multiple internal spaces",
			input: "ls    -la",
			want: []Token{
				{Kind: TokWord, Value: "ls", Pos: 0},
				{Kind: TokWord, Value: "-la", Pos: 6},
				{Kind: TokEOF, Value: "", Pos: 9},
			},
		},
		{
			name:  "tabs as separators",
			input: "ls\t-la",
			want: []Token{
				{Kind: TokWord, Value: "ls", Pos: 0},
				{Kind: TokWord, Value: "-la", Pos: 3},
				{Kind: TokEOF, Value: "", Pos: 6},
			},
		},
		{
			name:  "single pipe",
			input: "ls | wc",
			want: []Token{
				{Kind: TokWord, Value: "ls", Pos: 0},
				{Kind: TokPipe, Value: "|", Pos: 3},
				{Kind: TokWord, Value: "wc", Pos: 5},
				{Kind: TokEOF, Value: "", Pos: 7},
			},
		},
		{
			name:  "pipe no spaces",
			input: "ls|wc",
			want: []Token{
				{Kind: TokWord, Value: "ls", Pos: 0},
				{Kind: TokPipe, Value: "|", Pos: 2},
				{Kind: TokWord, Value: "wc", Pos: 3},
				{Kind: TokEOF, Value: "", Pos: 5},
			},
		},
		{
			name:  "three-stage pipeline",
			input: "git ls-files | head -50 | wc -l",
			want: []Token{
				{Kind: TokWord, Value: "git", Pos: 0},
				{Kind: TokWord, Value: "ls-files", Pos: 4},
				{Kind: TokPipe, Value: "|", Pos: 13},
				{Kind: TokWord, Value: "head", Pos: 15},
				{Kind: TokWord, Value: "-50", Pos: 20},
				{Kind: TokPipe, Value: "|", Pos: 24},
				{Kind: TokWord, Value: "wc", Pos: 26},
				{Kind: TokWord, Value: "-l", Pos: 29},
				{Kind: TokEOF, Value: "", Pos: 31},
			},
		},
		{
			name:  "path with slashes and dots",
			input: "cat ./src/main.go",
			want: []Token{
				{Kind: TokWord, Value: "cat", Pos: 0},
				{Kind: TokWord, Value: "./src/main.go", Pos: 4},
				{Kind: TokEOF, Value: "", Pos: 17},
			},
		},
		{
			name:  "flag with equals",
			input: "rg --type=py pattern",
			want: []Token{
				{Kind: TokWord, Value: "rg", Pos: 0},
				{Kind: TokWord, Value: "--type=py", Pos: 3},
				{Kind: TokWord, Value: "pattern", Pos: 13},
				{Kind: TokEOF, Value: "", Pos: 20},
			},
		},
		{
			name:  "double-quoted regex with pipe",
			input: `rg "auth|login" src`,
			want: []Token{
				{Kind: TokWord, Value: "rg", Pos: 0},
				{Kind: TokWord, Value: "auth|login", Pos: 3},
				{Kind: TokWord, Value: "src", Pos: 16},
				{Kind: TokEOF, Value: "", Pos: 19},
			},
		},
		{
			name:  "single-quoted regex with pipe",
			input: `rg 'auth|login' src`,
			want: []Token{
				{Kind: TokWord, Value: "rg", Pos: 0},
				{Kind: TokWord, Value: "auth|login", Pos: 3},
				{Kind: TokWord, Value: "src", Pos: 16},
				{Kind: TokEOF, Value: "", Pos: 19},
			},
		},
		{
			name:  "quoted content with spaces",
			input: `grep "hello world" file`,
			want: []Token{
				{Kind: TokWord, Value: "grep", Pos: 0},
				{Kind: TokWord, Value: "hello world", Pos: 5},
				{Kind: TokWord, Value: "file", Pos: 19},
				{Kind: TokEOF, Value: "", Pos: 23},
			},
		},
		{
			name:  "empty quoted string",
			input: `grep "" file`,
			want: []Token{
				{Kind: TokWord, Value: "grep", Pos: 0},
				{Kind: TokWord, Value: "", Pos: 5},
				{Kind: TokWord, Value: "file", Pos: 8},
				{Kind: TokEOF, Value: "", Pos: 12},
			},
		},
		{
			name:  "pipe inside quotes is literal",
			input: `rg "a|b|c" path | wc -l`,
			want: []Token{
				{Kind: TokWord, Value: "rg", Pos: 0},
				{Kind: TokWord, Value: "a|b|c", Pos: 3},
				{Kind: TokWord, Value: "path", Pos: 11},
				{Kind: TokPipe, Value: "|", Pos: 16},
				{Kind: TokWord, Value: "wc", Pos: 18},
				{Kind: TokWord, Value: "-l", Pos: 21},
				{Kind: TokEOF, Value: "", Pos: 23},
			},
		},
		{
			name:  "mixed quote types",
			input: `rg "a|b" 'c|d'`,
			want: []Token{
				{Kind: TokWord, Value: "rg", Pos: 0},
				{Kind: TokWord, Value: "a|b", Pos: 3},
				{Kind: TokWord, Value: "c|d", Pos: 9},
				{Kind: TokEOF, Value: "", Pos: 14},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Tokenize(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tokensEqual(got, tc.want) {
				t.Errorf("input %q\n  got:  %s\n  want: %s",
					tc.input, formatTokens(got), formatTokens(tc.want))
			}
		})
	}
}

func TestTokenize_ForbiddenCharacters(t *testing.T) {
	// Every character in this list must cause Tokenize to return
	// ErrForbiddenChar.
	cases := []struct {
		name  string
		input string
	}{
		{"dollar sign", `echo $HOME`},
		{"dollar brace", `echo ${HOME}`},
		{"command sub parens", `cat $(ls)`},
		{"backticks", "cat `ls`"},
		{"redirect out", `ls > file`},
		{"redirect in", `cat < file`},
		{"append redirect", `ls >> file`},
		{"here doc", `cat <<EOF`},
		{"open paren", `(ls)`},
		{"close paren", `ls)`},
		{"open brace", `{ls}`},
		{"close brace", `ls}`},
		{"asterisk glob", `ls *.go`},
		{"question mark glob", `ls a?.go`},
		{"tilde home", `ls ~/src`},
		{"square bracket glob", `ls [abc].go`},
		{"backslash escape", `ls foo\ bar`},
		{"bang history", `!ls`},
		{"hash comment", `ls # comment`},
		{"ampersand bg", `ls &`},
		{"double ampersand", `ls && wc`},
		{"semicolon seq", `ls; wc`},
		{"newline", "ls\nwc"},
		{"carriage return", "ls\rwc"},
		{"null byte", "ls\x00wc"},
		{"bell", "ls\x07wc"},
		{"DEL", "ls\x7fwc"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Tokenize(tc.input)
			if err == nil {
				t.Fatalf("expected error for input %q, got nil",
					tc.input)
			}
			var lerr *Error
			if !errors.As(err, &lerr) {
				t.Fatalf("expected *lex.Error, got %T", err)
			}
			if lerr.Code != ErrForbiddenChar {
				t.Errorf("input %q: expected %s, got %s",
					tc.input, ErrForbiddenChar, lerr.Code)
			}
		})
	}
}

func TestTokenize_UnterminatedQuote(t *testing.T) {
	cases := []string{
		`rg "unterminated`,
		`rg 'unterminated`,
		`ls "`,
		`ls '`,
		`echo "hello`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			_, err := Tokenize(input)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			var lerr *Error
			if !errors.As(err, &lerr) {
				t.Fatalf("expected *lex.Error, got %T", err)
			}
			if lerr.Code != ErrUnterminatedQuote {
				t.Errorf("expected %s, got %s",
					ErrUnterminatedQuote, lerr.Code)
			}
		})
	}
}

func TestTokenize_QuoteContainingForbidden(t *testing.T) {
	// Newlines and control chars are forbidden even inside quotes.
	cases := []string{
		"rg \"line1\nline2\"",
		"rg \"line1\rline2\"",
		"rg \"before\x00after\"",
		"rg \"before\x07after\"",
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			_, err := Tokenize(input)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			var lerr *Error
			if !errors.As(err, &lerr) {
				t.Fatalf("expected *lex.Error, got %T", err)
			}
			if lerr.Code != ErrForbiddenChar {
				t.Errorf("expected %s, got %s",
					ErrForbiddenChar, lerr.Code)
			}
		})
	}
}

func TestTokenize_QuoteAllowsOtherwiseForbidden(t *testing.T) {
	// Most forbidden characters are permitted inside quotes because
	// the parser treats quoted content as literal. This test
	// documents that contract.
	got, err := Tokenize(`rg "$ \ * ? ~ ( ) { } [ ] ! # & ; < >"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 tokens, got %d: %s",
			len(got), formatTokens(got))
	}
	want := `$ \ * ? ~ ( ) { } [ ] ! # & ; < >`
	if got[1].Value != want {
		t.Errorf("quoted content:\n  got:  %q\n  want: %q",
			got[1].Value, want)
	}
}

func TestTokenize_EmptyInput(t *testing.T) {
	cases := []string{"", "   ", "\t\t", "  \t "}
	for _, input := range cases {
		t.Run("input_"+input, func(t *testing.T) {
			_, err := Tokenize(input)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			var lerr *Error
			if !errors.As(err, &lerr) {
				t.Fatalf("expected *lex.Error, got %T", err)
			}
			if lerr.Code != ErrEmptyInput {
				t.Errorf("expected %s, got %s",
					ErrEmptyInput, lerr.Code)
			}
		})
	}
}

func TestTokenize_InputTooLong(t *testing.T) {
	input := strings.Repeat("a", MaxInputLen+1)
	_, err := Tokenize(input)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var lerr *Error
	if !errors.As(err, &lerr) {
		t.Fatalf("expected *lex.Error, got %T", err)
	}
	if lerr.Code != ErrInputTooLong {
		t.Errorf("expected %s, got %s", ErrInputTooLong, lerr.Code)
	}
}

func TestTokenize_InvalidUTF8(t *testing.T) {
	// A stray continuation byte with no lead byte.
	input := "ls \xff\xfe"
	_, err := Tokenize(input)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var lerr *Error
	if !errors.As(err, &lerr) {
		t.Fatalf("expected *lex.Error, got %T", err)
	}
	if lerr.Code != ErrInvalidUTF8 {
		t.Errorf("expected %s, got %s", ErrInvalidUTF8, lerr.Code)
	}
}

func TestTokenize_ConsecutivePipes(t *testing.T) {
	// Two pipes with no word between them. The lexer accepts this —
	// emitting two TokPipe tokens — because validating pipeline
	// structure is the parser's job, not the lexer's.
	got, err := Tokenize("ls || wc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Token{
		{Kind: TokWord, Value: "ls", Pos: 0},
		{Kind: TokPipe, Value: "|", Pos: 3},
		{Kind: TokPipe, Value: "|", Pos: 4},
		{Kind: TokWord, Value: "wc", Pos: 6},
		{Kind: TokEOF, Value: "", Pos: 8},
	}
	if !tokensEqual(got, want) {
		t.Errorf("got:  %s\nwant: %s",
			formatTokens(got), formatTokens(want))
	}
}

func TestTokenize_NeverPanics(t *testing.T) {
	// Smoke test: every byte value from 0..255 as a single-char
	// input must either succeed or return a typed error. Never panic.
	for b := 0; b < 256; b++ {
		input := string(rune(b))
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on byte 0x%02x: %v", b, r)
				}
			}()
			_, _ = Tokenize(input)
		}()
	}
}

// --- helpers ---

func tokensEqual(a, b []Token) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func formatTokens(ts []Token) string {
	var sb strings.Builder
	sb.WriteString("[")
	for i, t := range ts {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(t.Kind.String())
		if t.Value != "" {
			sb.WriteString("(")
			sb.WriteString(t.Value)
			sb.WriteString(")")
		}
		sb.WriteString("@")
		sb.WriteString(itoa(t.Pos))
	}
	sb.WriteString("]")
	return sb.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestTokenize_InvalidInputs covers rejection paths that aren't
// simple "character X is forbidden" cases. Every entry fails, and
// each asserts the specific ErrorCode returned so that regressions
// in error classification are caught.
func TestTokenize_InvalidInputs(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantCode ErrorCode
	}{
		// ---- Empty / whitespace-only ----
		{"empty string", "", ErrEmptyInput},
		{"single space", " ", ErrEmptyInput},
		{"single tab", "\t", ErrEmptyInput},
		{"mixed whitespace only", " \t \t  ", ErrEmptyInput},

		// ---- Length limits ----
		{"one byte over limit", strings.Repeat("a", MaxInputLen+1), ErrInputTooLong},
		{"far beyond limit", strings.Repeat("a", MaxInputLen*4), ErrInputTooLong},
		{"length check beats forbidden char",
			strings.Repeat("a", MaxInputLen) + "$", ErrInputTooLong},

		// ---- UTF-8 validity ----
		{"stray continuation byte", "ls \xff", ErrInvalidUTF8},
		{"truncated two-byte seq", "ls \xc3", ErrInvalidUTF8},
		{"truncated three-byte seq", "ls \xe2\x98", ErrInvalidUTF8},
		{"overlong encoding", "ls \xc0\xaf", ErrInvalidUTF8},
		{"invalid at start", "\xff\xfe ls", ErrInvalidUTF8},
		{"lone surrogate", "ls \xed\xa0\x80", ErrInvalidUTF8},

		// ---- Unterminated quotes ----
		{"unterminated empty double", `"`, ErrUnterminatedQuote},
		{"unterminated empty single", `'`, ErrUnterminatedQuote},
		{"unterminated double with content", `rg "unclosed`, ErrUnterminatedQuote},
		{"unterminated single with content", `rg 'unclosed`, ErrUnterminatedQuote},
		{"wrong closing quote type", `rg "opened' still open`, ErrUnterminatedQuote},
		{"single opened, double seen", `rg 'opened" still open`, ErrUnterminatedQuote},
		{"quote at end of input", `ls -la "`, ErrUnterminatedQuote},
		{"inner quote is content", `rg "foo'bar`, ErrUnterminatedQuote},

		// ---- Forbidden inside quoted strings ----
		{"newline inside double quotes", "rg \"a\nb\"", ErrForbiddenChar},
		{"newline inside single quotes", "rg 'a\nb'", ErrForbiddenChar},
		{"CR inside quotes", "rg \"a\rb\"", ErrForbiddenChar},
		{"null byte inside quotes", "rg \"a\x00b\"", ErrForbiddenChar},
		{"bell inside quotes", "rg \"a\x07b\"", ErrForbiddenChar},
		{"escape char inside quotes", "rg \"a\x1bb\"", ErrForbiddenChar},
		{"DEL inside quotes", "rg \"a\x7fb\"", ErrForbiddenChar},

		// ---- Non-ASCII rejected in unquoted words ----
		{"accented letter", "cat café", ErrForbiddenChar},
		{"cyrillic homoglyph", "сat file", ErrForbiddenChar},
		{"fullwidth digits", "head -\uff11\uff10", ErrForbiddenChar},
		{"zero-width space", "ls\u200b-la", ErrForbiddenChar},
		{"emoji", "ls \U0001f600", ErrForbiddenChar},
		{"combining diacritic alone", "ls \u0301", ErrForbiddenChar},
		{"RTL override", "ls \u202e", ErrForbiddenChar},
		{"byte order mark", "\ufeffls", ErrForbiddenChar},

		// ---- Control chars outside quotes ----
		{"bare newline between tokens", "ls\nwc", ErrForbiddenChar},
		{"bare CR between tokens", "ls\rwc", ErrForbiddenChar},
		{"vertical tab", "ls\x0bwc", ErrForbiddenChar},
		{"form feed", "ls\x0cwc", ErrForbiddenChar},
		{"null byte between tokens", "ls\x00wc", ErrForbiddenChar},
		{"escape char between tokens", "ls\x1bwc", ErrForbiddenChar},

		// ---- Unicode whitespace that isn't space or tab ----
		{"non-breaking space", "ls\u00a0-la", ErrForbiddenChar},
		{"en quad", "ls\u2000-la", ErrForbiddenChar},
		{"ideographic space", "ls\u3000-la", ErrForbiddenChar},

		// ---- Metacharacter boundary cases ----
		{"dollar at end", "ls $", ErrForbiddenChar},
		{"dollar mid-word", "ls foo$bar", ErrForbiddenChar},
		{"backtick pair", "ls ``", ErrForbiddenChar},
		{"bare backtick", "`", ErrForbiddenChar},
		{"redirect no whitespace", "ls>file", ErrForbiddenChar},
		{"append redirect no whitespace", "ls>>file", ErrForbiddenChar},
		{"heredoc start", "cat<<EOF", ErrForbiddenChar},
		{"process substitution", "diff <(ls) <(ls)", ErrForbiddenChar},
		{"brace expansion", "ls {a,b}.go", ErrForbiddenChar},
		{"tilde home", "cat ~/.bashrc", ErrForbiddenChar},
		{"star glob", "ls src/*.go", ErrForbiddenChar},
		{"question glob", "ls a?.go", ErrForbiddenChar},
		{"char class glob", "ls [abc].go", ErrForbiddenChar},
		{"negated char class", "ls [!abc].go", ErrForbiddenChar},
		{"bang at start", "!ls", ErrForbiddenChar},
		{"history ref", "ls !!", ErrForbiddenChar},
		{"comment marker", "ls # list files", ErrForbiddenChar},
		{"comment no space", "ls#comment", ErrForbiddenChar},
		{"single ampersand", "ls &", ErrForbiddenChar},
		{"double ampersand chain", "ls && wc", ErrForbiddenChar},
		{"semicolon sequence", "ls ; wc", ErrForbiddenChar},
		{"semicolon no spaces", "ls;wc", ErrForbiddenChar},
		{"backslash escape", `ls foo\ bar`, ErrForbiddenChar},
		{"backslash before quote", `ls \"foo\"`, ErrForbiddenChar},
		{"stderr redirect", "ls 2>err", ErrForbiddenChar},
		{"fd duplication", "ls 2>&1", ErrForbiddenChar},
		{"caret", "ls ^foo", ErrForbiddenChar},

		// ---- Composed attack attempts ----
		{"command sub parens", "cat $(ls)", ErrForbiddenChar},
		{"nested command sub", "cat $(ls $(pwd))", ErrForbiddenChar},
		{"param expansion default", "cat ${FOO:-/etc/passwd}", ErrForbiddenChar},
		{"arithmetic expansion", "echo $((1+1))", ErrForbiddenChar},
		{"subshell group", "(ls)", ErrForbiddenChar},
		{"brace group", "{ ls; }", ErrForbiddenChar},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Tokenize(tc.input)
			if err == nil {
				t.Fatalf("expected error for input %q, got tokens %s",
					tc.input, formatTokens(got))
			}
			if got != nil {
				t.Errorf("expected nil tokens on error, got %s",
					formatTokens(got))
			}
			var lerr *Error
			if !errors.As(err, &lerr) {
				t.Fatalf("expected *lex.Error, got %T: %v", err, err)
			}
			if lerr.Code != tc.wantCode {
				t.Errorf("input %q: want code %s, got %s (%v)",
					tc.input, tc.wantCode, lerr.Code, lerr)
			}
		})
	}
}
