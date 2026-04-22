package lex

import "testing"

// FuzzTokenize verifies that Tokenize never panics and never returns
// a non-nil result with a non-nil error. Run with:
//
//	go test -fuzz=FuzzTokenize -fuzztime=60s ./lex
func FuzzTokenize(f *testing.F) {
	seeds := []string{
		"",
		"ls",
		"ls -la",
		"ls | wc",
		`rg "a|b" src`,
		`cat $HOME`,
		"ls\nwc",
		"git --no-pager log --oneline",
		"\xff",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		tokens, err := Tokenize(input)
		if err != nil && tokens != nil {
			t.Errorf("non-nil tokens with non-nil error "+
				"for input %q", input)
		}
		if err == nil {
			// Post-condition: last token must be TokEOF.
			if len(tokens) == 0 {
				t.Errorf("no tokens returned but no error "+
					"for input %q", input)
			} else if tokens[len(tokens)-1].Kind != TokEOF {
				t.Errorf("last token is not TokEOF for "+
					"input %q", input)
			}
		}
	})
}
