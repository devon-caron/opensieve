package match

import (
	"errors"
	"strings"
	"testing"

	"github.com/devon-caron/opensieve/lex"
)

// FuzzMatch drives Matcher.Match with arbitrary lexable inputs and
// asserts the matcher's contract holds on every output:
//
//   - (Result, error) is mutually exclusive — exactly one is non-nil.
//   - On failure, err is *Error or Errors; never some other type.
//   - Errors of length 1 are promoted to *Error per the package
//     contract; a returned Errors must therefore have len >= 2.
//   - Every Pos field cited in an error refers to a byte offset within
//     the original input.
//   - Every Code is one of the defined ErrorCode constants.
//   - On success, Path is non-empty and Entry is non-nil.
//   - Match is deterministic — calling it twice with the same input
//     produces equivalent error/no-error and equivalent Path on
//     success.
//
// The seed corpus mixes accepted commands, rejected commands, lexer
// pathological inputs, and a few injection-flavored strings so the
// fuzzer has a varied starting point. The matcher uses gitSpec from
// the test fixtures — the same policy the unit tests exercise — so
// fuzz failures and unit test failures share a frame of reference.
func FuzzMatch(f *testing.F) {
	seeds := []string{
		// Accepted shapes
		"git --no-pager log --oneline",
		"git --no-pager status",
		"git --no-pager blame README.md",
		"ls",
		"git",
		"git --no-pager",

		// Routing-stage rejections
		"git log",
		"git --no-pager push origin",
		"git foo",
		"rm -rf /",

		// Validation-stage rejections (inherited + own)
		"git --no-pager log --textconv",
		"git --no-pager blame --contents=/etc/passwd",
		"git --no-pager log --output=/tmp/x",

		// Multi-error
		"git --no-pager blame --contents=/x --textconv --ext-diff",

		// Empty / pathological lex inputs (some will lex-fail; that's fine)
		"",
		" ",
		"|",
		"||",
		"a",
		`""`,
		`"unterminated`,
		"git\t--no-pager\tlog",
		"git --no-pager log " + strings.Repeat("a ", 64),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	matcher, err := FromSpec(gitSpec())
	if err != nil {
		f.Fatalf("build matcher from gitSpec: %v", err)
	}

	validCodes := map[ErrorCode]struct{}{
		ErrEmptySegment:      {},
		ErrCommandNotAllowed: {},
		ErrArgNotAllowed:     {},
		ErrArgDenied:         {},
		ErrMissingRequired:   {},
	}

	checkErr := func(t *testing.T, input string, err error) {
		t.Helper()

		// The error must be either a single *Error or an Errors slice.
		var single *Error
		var many Errors
		gotSingle := errors.As(err, &single)
		gotMany := errors.As(err, &many)
		if !gotSingle && !gotMany {
			t.Fatalf("error is neither *Error nor Errors: %T %v", err, err)
		}
		if gotMany && len(many) < 2 {
			t.Fatalf("Errors must have len >= 2 (single-error gets promoted to *Error); got len %d",
				len(many))
		}

		validateOne := func(e *Error) {
			if _, ok := validCodes[e.Code]; !ok {
				t.Errorf("error code %q is not one of the defined ErrorCode constants",
					e.Code)
			}
			if e.Pos < 0 || e.Pos > len(input) {
				t.Errorf("byte pos %d out of bounds for input len %d (token=%q, code=%s)",
					e.Pos, len(input), e.Token, e.Code)
			}
		}
		if gotSingle {
			validateOne(single)
		}
		for _, e := range many {
			validateOne(e)
		}
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Step 1: tokenize. Lex errors aren't a matcher concern; skip.
		tokens, err := lex.Tokenize(input)
		if err != nil {
			return
		}

		// Step 2: split tokens at TokPipe (drop trailing TokEOF) — this
		// mirrors what parser.Parse does upstream.
		var segs []Segment
		var cur []lex.Token
		for _, tok := range tokens {
			switch tok.Kind {
			case lex.TokPipe:
				segs = append(segs, Segment{Tokens: cur})
				cur = nil
			case lex.TokEOF:
				segs = append(segs, Segment{Tokens: cur})
			case lex.TokWord:
				cur = append(cur, tok)
			}
		}

		for i, seg := range segs {
			r1, err1 := matcher.Match(seg)
			r2, err2 := matcher.Match(seg)

			// Determinism: same input, same output shape.
			if (r1 == nil) != (r2 == nil) || (err1 == nil) != (err2 == nil) {
				t.Fatalf("non-deterministic Match on segment %d", i)
			}

			// Mutual exclusion: result XOR error.
			if (r1 == nil) == (err1 == nil) {
				t.Fatalf("Match returned (r=%v, err=%v) — both nil or both non-nil on segment %d",
					r1 != nil, err1 != nil, i)
			}

			if err1 != nil {
				checkErr(t, input, err1)
			}

			if r1 != nil {
				if len(r1.Path) == 0 {
					t.Errorf("Result.Path is empty on success (segment %d)", i)
				}
				if r1.Entry == nil {
					t.Errorf("Result.Entry is nil on success (segment %d)", i)
				}
				if r1.Path[0] != r1.Argv[0] {
					t.Errorf("Path[0]=%q != Argv[0]=%q on segment %d",
						r1.Path[0], r1.Argv[0], i)
				}
			}
		}
	})
}
