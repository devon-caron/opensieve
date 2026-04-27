package opensieve

import (
	"errors"
	"strings"
	"testing"

	"github.com/devon-caron/opensieve/lex"
	"github.com/devon-caron/opensieve/match"
)

const readToolYAML = "read_tool.yaml"

func mustParser(t *testing.T) *Parser {
	t.Helper()
	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func TestParse_GitLogHappyPath(t *testing.T) {
	p := mustParser(t)
	r := p.Parse(readToolYAML, "git --no-pager log --no-pager --oneline")
	if !r.Pass {
		t.Fatalf("expected Pass=true, got false: %v", r.Reason)
	}
	if r.Rule != "ReadCodebase" {
		t.Errorf("Rule = %q, want %q", r.Rule, "ReadCodebase")
	}
}

func TestParse_GitLogTextconvDenied(t *testing.T) {
	p := mustParser(t)
	r := p.Parse(readToolYAML, "git --no-pager log --no-pager --textconv")
	if r.Pass {
		t.Fatal("expected Pass=false")
	}
	msg := r.Reason.Error()
	for _, want := range []string{"--textconv", "disallowed_sub_args", "inherited"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q\nfull message:\n%s", want, msg)
		}
	}

	var me *match.Error
	if !errors.As(r.Reason, &me) {
		t.Fatalf("expected *match.Error, got %T", r.Reason)
	}
	if me.Code != match.ErrArgDenied {
		t.Errorf("Code = %q, want %q", me.Code, match.ErrArgDenied)
	}
}

func TestParse_GitPushUnknownSubcommand(t *testing.T) {
	p := mustParser(t)
	r := p.Parse(readToolYAML, "git push origin")
	if r.Pass {
		t.Fatal("expected Pass=false")
	}

	var me *match.Error
	if !errors.As(r.Reason, &me) {
		t.Fatalf("expected *match.Error, got %T: %v", r.Reason, r.Reason)
	}
	if me.Code != match.ErrArgNotAllowed {
		t.Errorf("Code = %q, want %q", me.Code, match.ErrArgNotAllowed)
	}
	if me.Token != "push" {
		t.Errorf("Token = %q, want %q", me.Token, "push")
	}
	if me.Pos != 4 {
		t.Errorf("Pos = %d, want 4 (byte offset of 'push' in 'git push origin')", me.Pos)
	}
	// Under the strict-routing model, git's only sub is `--no-pager`,
	// so that's what gets surfaced as the available next step.
	if len(me.Subs) != 1 || me.Subs[0] != "--no-pager" {
		t.Errorf("Subs = %v, want [--no-pager]", me.Subs)
	}

	msg := me.Error()
	for _, want := range []string{"push", "--no-pager", "fix:"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q\nfull message:\n%s", want, msg)
		}
	}
}

func TestParse_PipelineHappyPath(t *testing.T) {
	p := mustParser(t)
	r := p.Parse(readToolYAML, "ls -la | grep foo")
	if !r.Pass {
		t.Fatalf("expected Pass=true, got false: %v", r.Reason)
	}
}

func TestParse_PipelineSecondSegmentDenied(t *testing.T) {
	p := mustParser(t)
	r := p.Parse(readToolYAML, "ls -la | grep -f patterns.txt")
	if r.Pass {
		t.Fatal("expected Pass=false")
	}

	var me *match.Error
	if !errors.As(r.Reason, &me) {
		t.Fatalf("expected *match.Error, got %T: %v", r.Reason, r.Reason)
	}
	if me.Code != match.ErrArgDenied {
		t.Errorf("Code = %q, want %q", me.Code, match.ErrArgDenied)
	}
	if me.Token != "-f" {
		t.Errorf("Token = %q, want %q", me.Token, "-f")
	}
	if me.Command != "grep" {
		t.Errorf("Command = %q, want %q", me.Command, "grep")
	}

	msg := me.Error()
	if !strings.Contains(msg, "-f") {
		t.Errorf("error message missing -f:\n%s", msg)
	}
}

func TestParse_CacheReusesMatcher(t *testing.T) {
	p := mustParser(t)
	r1 := p.Parse(readToolYAML, "ls -la")
	r2 := p.Parse(readToolYAML, "ls -la")
	if !r1.Pass || !r2.Pass {
		t.Fatalf("expected both passes; r1=%v r2=%v", r1.Reason, r2.Reason)
	}
	if len(p.loaded) != 1 {
		t.Errorf("expected 1 cached spec, got %d", len(p.loaded))
	}
}

func TestParse_MissingFile(t *testing.T) {
	p := mustParser(t)
	r := p.Parse("/nonexistent/path/spec.yaml", "ls -la")
	if r.Pass {
		t.Fatal("expected Pass=false for missing file")
	}
	if r.Reason == nil {
		t.Fatal("expected non-nil Reason")
	}
}

func TestParse_LexerError(t *testing.T) {
	p := mustParser(t)
	r := p.Parse(readToolYAML, "ls > out.txt")
	if r.Pass {
		t.Fatal("expected Pass=false for forbidden char")
	}
	if r.Reason == nil {
		t.Fatal("expected non-nil Reason")
	}
}

func TestSplitSegments_PipelineCounts(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"single", "ls -la", 1},
		{"two", "ls -la | grep foo", 2},
		{"three", "ls -la | grep foo | wc -l", 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tokens, err := lex.Tokenize(tc.input)
			if err != nil {
				t.Fatalf("Tokenize(%q): %v", tc.input, err)
			}
			got := splitSegments(tc.input, tokens)
			if len(got) != tc.want {
				t.Errorf("len(segs) = %d, want %d", len(got), tc.want)
			}
		})
	}
}
