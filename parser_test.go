package opensieve

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/devon-caron/opensieve/lex"
	"github.com/devon-caron/opensieve/match"
)

// mustReadYAML reads the YAML file and returns its content.
// This is needed because Parser.Parse() expects YAML content as a string,
// not a file path.
func mustReadYAML(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("read_tool.yaml")
	if err != nil {
		t.Fatalf("read read_tool.yaml: %v", err)
	}
	return string(data)
}

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
	yaml := mustReadYAML(t)
	r := p.Parse(yaml, "git", []string{"--no-pager", "log", "--no-pager", "--oneline"})
	if !r.Pass {
		t.Fatalf("expected Pass=true, got false: %v", r.Reason)
	}
	if r.Rule != "ReadCodebase" {
		t.Errorf("Rule = %q, want %q", r.Rule, "ReadCodebase")
	}
}

func TestParse_GitLogTextconvDenied(t *testing.T) {
	p := mustParser(t)
	yaml := mustReadYAML(t)
	r := p.Parse(yaml, "git", []string{"--no-pager", "log", "--no-pager", "--textconv"})
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
	yaml := mustReadYAML(t)
	r := p.Parse(yaml, "git", []string{"push", "origin"})
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
	yaml := mustReadYAML(t)
	r := p.Parse(yaml, "ls", []string{"-la", "|", "grep", "foo"})
	if !r.Pass {
		t.Fatalf("expected Pass=true, got false: %v", r.Reason)
	}
}

func TestParse_PipelineSecondSegmentDenied(t *testing.T) {
	p := mustParser(t)
	yaml := mustReadYAML(t)
	r := p.Parse(yaml, "ls", []string{"-la", "|", "grep", "-f", "patterns.txt"})
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
	yaml := mustReadYAML(t)
	r1 := p.Parse(yaml, "ls", []string{"-la"})
	r2 := p.Parse(yaml, "ls", []string{"-la"})
	if !r1.Pass || !r2.Pass {
		t.Fatalf("expected both passes; r1=%v r2=%v", r1.Reason, r2.Reason)
	}
	if len(p.loaded) != 1 {
		t.Errorf("expected 1 cached spec, got %d", len(p.loaded))
	}
}

func TestParse_MissingFile(t *testing.T) {
	p := mustParser(t)
	r := p.Parse("/nonexistent/path/spec.yaml", "ls", []string{"-la"})
	if r.Pass {
		t.Fatal("expected Pass=false for missing file")
	}
	if r.Reason == nil {
		t.Fatal("expected non-nil Reason")
	}
}

func TestParse_LexerError(t *testing.T) {
	p := mustParser(t)
	yaml := mustReadYAML(t)
	// Use a control character that the lexer rejects even inside quotes.
	r := p.Parse(yaml, "ls", []string{"\x01"})
	if r.Pass {
		t.Fatal("expected Pass=false for forbidden char")
	}
	if r.Reason == nil {
		t.Fatal("expected non-nil Reason")
	}
}

func TestJoinArgv(t *testing.T) {
	tests := []struct {
		name string
		base string
		argv []string
		want string
	}{
		{
			name: "empty input yields empty string",
			base: "",
			argv: nil,
			want: "",
		},
		{
			name: "base only, no args",
			base: "ls",
			argv: nil,
			want: "ls",
		},
		{
			name: "plain args need no quoting",
			base: "git",
			argv: []string{"--no-pager", "log", "--oneline"},
			want: "git --no-pager log --oneline",
		},
		{
			name: "arg with whitespace is double-quoted",
			base: "git",
			argv: []string{"commit", "-m", "hello world"},
			want: `git commit -m "hello world"`,
		},
		{
			name: "arg with embedded double quote uses single quotes",
			base: "echo",
			argv: []string{`say "hi"`},
			want: `echo 'say "hi"'`,
		},
		{
			name: "arg with embedded single quote uses double quotes",
			base: "echo",
			argv: []string{"it's"},
			want: `echo "it's"`,
		},
		{
			name: "arg with literal pipe is quoted (preserves single segment)",
			base: "rg",
			argv: []string{"foo|bar"},
			want: `rg "foo|bar"`,
		},
		{
			name: "bare pipe element passes through as pipeline boundary",
			base: "ls",
			argv: []string{"-la", "|", "wc", "-l"},
			want: "ls -la | wc -l",
		},
		{
			name: "arg with metachar gets quoted to round-trip as one token",
			base: "find",
			argv: []string{".", "-name", "*.go", "-type", "f"},
			want: `find . -name "*.go" -type f`,
		},
		{
			name: "arg containing dollar sign is quoted",
			base: "echo",
			argv: []string{"$HOME"},
			want: `echo "$HOME"`,
		},
		{
			name: "empty arg becomes empty quoted token",
			base: "cmd",
			argv: []string{"a", "", "b"},
			want: `cmd a "" b`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := JoinArgv(tc.base, tc.argv)
			if got != tc.want {
				t.Errorf("JoinArgv(%q, %#v)\n  got:  %q\n  want: %q",
					tc.base, tc.argv, got, tc.want)
			}
		})
	}
}

// TestSeparateCommand tests the deprecated SeparateCommand function
// which only handles single-segment commands. For pipelines, use
// SeparateCommands instead.
func TestSeparateCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantBase string
		wantArgv []string
	}{
		{
			name:     "single bare command",
			input:    "ls",
			wantBase: "ls",
			wantArgv: []string{},
		},
		{
			name:     "command with plain args",
			input:    "git --no-pager log --oneline",
			wantBase: "git",
			wantArgv: []string{"--no-pager", "log", "--oneline"},
		},
		{
			name:     "double-quoted arg with whitespace becomes single element",
			input:    `git commit -m "hello world"`,
			wantBase: "git",
			wantArgv: []string{"commit", "-m", "hello world"},
		},
		{
			name:     "single-quoted arg with embedded double quote",
			input:    `echo 'say "hi"'`,
			wantBase: "echo",
			wantArgv: []string{`say "hi"`},
		},
		{
			name:     "metachar inside quotes survives as literal element",
			input:    `find . -name "*.go"`,
			wantBase: "find",
			wantArgv: []string{".", "-name", "*.go"},
		},
		{
			name:     "empty quoted element is preserved",
			input:    `cmd "" arg`,
			wantBase: "cmd",
			wantArgv: []string{"", "arg"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotBase, gotArgv, err := SeparateCommand(tc.input)
			if err != nil {
				t.Fatalf("SeparateCommand(%q): unexpected error: %v",
					tc.input, err)
			}
			if gotBase != tc.wantBase {
				t.Errorf("base: got %q, want %q", gotBase, tc.wantBase)
			}
			if !stringSlicesEqual(gotArgv, tc.wantArgv) {
				t.Errorf("argv: got %#v, want %#v", gotArgv, tc.wantArgv)
			}
		})
	}
}

func TestSeparateCommand_Errors(t *testing.T) {
	t.Run("empty input returns lex error", func(t *testing.T) {
		_, _, err := SeparateCommand("")
		if err == nil {
			t.Fatal("expected error on empty input")
		}
		if errors.Is(err, ErrPipeInSingleCommand) {
			t.Errorf("empty input should not return ErrPipeInSingleCommand")
		}
	})

	t.Run("pipe operator returns ErrPipeInSingleCommand", func(t *testing.T) {
		_, _, err := SeparateCommand("ls -la | wc -l")
		if !errors.Is(err, ErrPipeInSingleCommand) {
			t.Errorf("got %v, want ErrPipeInSingleCommand", err)
		}
	})

	t.Run("forbidden char returns lex error", func(t *testing.T) {
		_, _, err := SeparateCommand("ls > out.txt")
		if err == nil {
			t.Fatal("expected error on forbidden char")
		}
		var lerr *lex.Error
		if !errors.As(err, &lerr) {
			t.Fatalf("expected *lex.Error, got %T", err)
		}
	})
}

// TestSeparateCommands tests the SeparateCommands function which supports
// pipelines. This is the preferred function for parsing command strings.
func TestSeparateCommands(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCmds  []CommandSegment
		wantPipes int
	}{
		{
			name:      "single bare command",
			input:     "ls",
			wantCmds:  []CommandSegment{{Base: "ls", Argv: []string{}}},
			wantPipes: 0,
		},
		{
			name:      "command with plain args",
			input:     "git --no-pager log --oneline",
			wantCmds:  []CommandSegment{{Base: "git", Argv: []string{"--no-pager", "log", "--oneline"}}},
			wantPipes: 0,
		},
		{
			name:  "two-segment pipeline",
			input: "ls -la | grep foo",
			wantCmds: []CommandSegment{
				{Base: "ls", Argv: []string{"-la"}},
				{Base: "grep", Argv: []string{"foo"}},
			},
			wantPipes: 1,
		},
		{
			name:  "three-segment pipeline",
			input: "git --no-pager ls-files | grep README | wc -l",
			wantCmds: []CommandSegment{
				{Base: "git", Argv: []string{"--no-pager", "ls-files"}},
				{Base: "grep", Argv: []string{"README"}},
				{Base: "wc", Argv: []string{"-l"}},
			},
			wantPipes: 2,
		},
		{
			name:  "quoted args in pipeline",
			input: `git commit -m "hello world" | cat`,
			wantCmds: []CommandSegment{
				{Base: "git", Argv: []string{"commit", "-m", "hello world"}},
				{Base: "cat", Argv: []string{}},
			},
			wantPipes: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cs, err := SeparateCommands(tc.input)
			if err != nil {
				t.Fatalf("SeparateCommands(%q): unexpected error: %v", tc.input, err)
			}
			if cs.Pipes != tc.wantPipes {
				t.Errorf("Pipes: got %d, want %d", cs.Pipes, tc.wantPipes)
			}
			if len(cs.Segments) != len(tc.wantCmds) {
				t.Fatalf("Segments: got %d, want %d", len(cs.Segments), len(tc.wantCmds))
			}
			for i, want := range tc.wantCmds {
				got := cs.Segments[i]
				if got.Base != want.Base {
					t.Errorf("segment %d base: got %q, want %q", i, got.Base, want.Base)
				}
				if !stringSlicesEqual(got.Argv, want.Argv) {
					t.Errorf("segment %d argv: got %#v, want %#v", i, got.Argv, want.Argv)
				}
			}
		})
	}
}

func TestSeparateCommands_Errors(t *testing.T) {
	t.Run("empty input returns error", func(t *testing.T) {
		_, err := SeparateCommands("")
		if err == nil {
			t.Fatal("expected error on empty input")
		}
	})

	t.Run("forbidden char returns lex error", func(t *testing.T) {
		_, err := SeparateCommands("ls > out.txt")
		if err == nil {
			t.Fatal("expected error on forbidden char")
		}
		var lerr *lex.Error
		if !errors.As(err, &lerr) {
			t.Fatalf("expected *lex.Error, got %T", err)
		}
	})
}

// TestJoinCommands tests the JoinCommands function which converts a
// CommandSet back into a command string.
func TestJoinCommands(t *testing.T) {
	tests := []struct {
		name string
		cs   CommandSet
		want string
	}{
		{
			name: "single command",
			cs: CommandSet{
				Segments: []CommandSegment{{Base: "ls", Argv: []string{"-la"}}},
				Pipes:    0,
			},
			want: "ls -la",
		},
		{
			name: "two-segment pipeline",
			cs: CommandSet{
				Segments: []CommandSegment{
					{Base: "ls", Argv: []string{"-la"}},
					{Base: "grep", Argv: []string{"foo"}},
				},
				Pipes: 1,
			},
			want: "ls -la | grep foo",
		},
		{
			name: "three-segment pipeline",
			cs: CommandSet{
				Segments: []CommandSegment{
					{Base: "git", Argv: []string{"--no-pager", "ls-files"}},
					{Base: "grep", Argv: []string{"README"}},
					{Base: "wc", Argv: []string{"-l"}},
				},
				Pipes: 2,
			},
			want: "git --no-pager ls-files | grep README | wc -l",
		},
		{
			name: "quoted args",
			cs: CommandSet{
				Segments: []CommandSegment{
					{Base: "git", Argv: []string{"commit", "-m", "hello world"}},
					{Base: "cat", Argv: []string{}},
				},
				Pipes: 1,
			},
			want: `git commit -m "hello world" | cat`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := JoinCommands(tc.cs)
			if got != tc.want {
				t.Errorf("JoinCommands: got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestArgvRoundTrip locks in the inverse property: for any argv shape
// JoinArgv produces, SeparateCommand returns the original (base, argv).
// This is the contract callers rely on when moving between the two
// representations.
func TestArgvRoundTrip(t *testing.T) {
	cases := []struct {
		base string
		argv []string
	}{
		{"ls", nil},
		{"git", []string{"--no-pager", "log", "--oneline"}},
		{"git", []string{"commit", "-m", "hello world"}},
		{"echo", []string{`say "hi"`}},
		{"echo", []string{"it's"}},
		{"find", []string{".", "-name", "*.go", "-type", "f"}},
		{"echo", []string{"$HOME"}},
		{"cmd", []string{"a", "", "b"}},
		{"rg", []string{"a|b"}},
	}
	for _, tc := range cases {
		t.Run(tc.base+"_"+strings.Join(tc.argv, "_"), func(t *testing.T) {
			joined := JoinArgv(tc.base, tc.argv)
			gotBase, gotArgv, err := SeparateCommand(joined)
			if err != nil {
				t.Fatalf("round-trip failed: JoinArgv → %q, "+
					"SeparateCommand error: %v", joined, err)
			}
			if gotBase != tc.base {
				t.Errorf("base mismatch: joined=%q got=%q want=%q",
					joined, gotBase, tc.base)
			}
			wantArgv := tc.argv
			if wantArgv == nil {
				wantArgv = []string{}
			}
			if !stringSlicesEqual(gotArgv, wantArgv) {
				t.Errorf("argv mismatch: joined=%q\n  got:  %#v\n  want: %#v",
					joined, gotArgv, wantArgv)
			}
		})
	}
}

// TestCommandSetRoundTrip locks in the inverse property: for any
// CommandSet, JoinCommands produces a string that SeparateCommands
// can parse back into the original CommandSet.
func TestCommandSetRoundTrip(t *testing.T) {
	cases := []CommandSet{
		{
			Segments: []CommandSegment{{Base: "ls", Argv: []string{"-la"}}},
			Pipes:    0,
		},
		{
			Segments: []CommandSegment{
				{Base: "ls", Argv: []string{"-la"}},
				{Base: "grep", Argv: []string{"foo"}},
			},
			Pipes: 1,
		},
		{
			Segments: []CommandSegment{
				{Base: "git", Argv: []string{"--no-pager", "ls-files"}},
				{Base: "grep", Argv: []string{"README"}},
				{Base: "wc", Argv: []string{"-l"}},
			},
			Pipes: 2,
		},
		{
			Segments: []CommandSegment{
				{Base: "git", Argv: []string{"commit", "-m", "hello world"}},
				{Base: "cat", Argv: []string{}},
			},
			Pipes: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.Segments[0].Base, func(t *testing.T) {
			joined := JoinCommands(tc)
			got, err := SeparateCommands(joined)
			if err != nil {
				t.Fatalf("round-trip failed: JoinCommands → %q, SeparateCommands error: %v", joined, err)
			}
			if got.Pipes != tc.Pipes {
				t.Errorf("Pipes mismatch: got %d, want %d", got.Pipes, tc.Pipes)
			}
			if len(got.Segments) != len(tc.Segments) {
				t.Fatalf("Segments count mismatch: got %d, want %d", len(got.Segments), len(tc.Segments))
			}
			for i, want := range tc.Segments {
				got := got.Segments[i]
				if got.Base != want.Base {
					t.Errorf("segment %d base mismatch: got %q, want %q", i, got.Base, want.Base)
				}
				if !stringSlicesEqual(got.Argv, want.Argv) {
					t.Errorf("segment %d argv mismatch: got %#v, want %#v", i, got.Argv, want.Argv)
				}
			}
		})
	}
}

// TestParseCommandSet_SinglePass tests that a single-segment CommandSet
// that passes the policy returns Pass=true.
func TestParseCommandSet_SinglePass(t *testing.T) {
	p := mustParser(t)
	yaml := mustReadYAML(t)
	cs, err := SeparateCommands("ls -la")
	if err != nil {
		t.Fatalf("SeparateCommands: unexpected error: %v", err)
	}
	r := p.ParseCommandSet(yaml, cs)
	if !r.Pass {
		t.Fatalf("expected Pass=true, got false: %v", r.Reason)
	}
	if r.Rule != "ReadCodebase" {
		t.Errorf("Rule = %q, want %q", r.Rule, "ReadCodebase")
	}
}

// TestParseCommandSet_SingleFail tests that a single-segment CommandSet
// that fails the policy returns Pass=false.
func TestParseCommandSet_SingleFail(t *testing.T) {
	p := mustParser(t)
	yaml := mustReadYAML(t)
	cs, err := SeparateCommands("tail -f /var/log/syslog")
	if err != nil {
		t.Fatalf("SeparateCommands: unexpected error: %v", err)
	}
	r := p.ParseCommandSet(yaml, cs)
	if r.Pass {
		t.Fatal("expected Pass=false")
	}
	var me *match.Error
	if !errors.As(r.Reason, &me) {
		t.Fatalf("expected *match.Error, got %T", r.Reason)
	}
	if me.Code != match.ErrArgDenied {
		t.Errorf("Code = %q, want %q", me.Code, match.ErrArgDenied)
	}
	if me.Token != "-f" {
		t.Errorf("Token = %q, want %q", me.Token, "-f")
	}
}

// TestParseCommandSet_PipelineAllPass tests that a multi-segment pipeline
// where every segment passes the policy returns Pass=true.
func TestParseCommandSet_PipelineAllPass(t *testing.T) {
	p := mustParser(t)
	yaml := mustReadYAML(t)
	cs, err := SeparateCommands("ls -la | grep foo")
	if err != nil {
		t.Fatalf("SeparateCommands: unexpected error: %v", err)
	}
	r := p.ParseCommandSet(yaml, cs)
	if !r.Pass {
		t.Fatalf("expected Pass=true, got false: %v", r.Reason)
	}
}

// TestParseCommandSet_PipelineSecondFail tests that a multi-segment pipeline
// where the first segment passes but the second fails returns Pass=false.
func TestParseCommandSet_PipelineSecondFail(t *testing.T) {
	p := mustParser(t)
	yaml := mustReadYAML(t)
	cs, err := SeparateCommands("ls -la | grep -f patterns.txt")
	if err != nil {
		t.Fatalf("SeparateCommands: unexpected error: %v", err)
	}
	r := p.ParseCommandSet(yaml, cs)
	if r.Pass {
		t.Fatal("expected Pass=false")
	}
	var me *match.Error
	if !errors.As(r.Reason, &me) {
		t.Fatalf("expected *match.Error, got %T", r.Reason)
	}
	if me.Code != match.ErrArgDenied {
		t.Errorf("Code = %q, want %q", me.Code, match.ErrArgDenied)
	}
	if me.Token != "-f" {
		t.Errorf("Token = %q, want %q", me.Token, "-f")
	}
}

// TestParseCommandSet_EmptyCommandSet tests that an empty CommandSet
// returns a clear error.
func TestParseCommandSet_EmptyCommandSet(t *testing.T) {
	p := mustParser(t)
	yaml := mustReadYAML(t)
	cs := CommandSet{Segments: []CommandSegment{}}
	r := p.ParseCommandSet(yaml, cs)
	if r.Pass {
		t.Fatal("expected Pass=false for empty CommandSet")
	}
	if r.Reason == nil {
		t.Fatal("expected non-nil Reason for empty CommandSet")
	}
	if !strings.Contains(r.Reason.Error(), "empty") {
		t.Errorf("expected error message to mention 'empty', got: %v", r.Reason)
	}
}

// TestParseCommandSet_PipelineThreeSegments tests a three-segment pipeline.
func TestParseCommandSet_PipelineThreeSegments(t *testing.T) {
	p := mustParser(t)
	yaml := mustReadYAML(t)
	cs, err := SeparateCommands("git --no-pager ls-files | grep README | wc -l")
	if err != nil {
		t.Fatalf("SeparateCommands: unexpected error: %v", err)
	}
	r := p.ParseCommandSet(yaml, cs)
	if !r.Pass {
		t.Fatalf("expected Pass=true, got false: %v", r.Reason)
	}
}

// TestParseCommandSet_PipelineMiddleFail tests a three-segment pipeline
// where the middle segment fails.
func TestParseCommandSet_PipelineMiddleFail(t *testing.T) {
	p := mustParser(t)
	yaml := mustReadYAML(t)
	cs, err := SeparateCommands("git --no-pager ls-files | grep --include-from=ignore.txt | wc -l")
	if err != nil {
		t.Fatalf("SeparateCommands: unexpected error: %v", err)
	}
	r := p.ParseCommandSet(yaml, cs)
	if r.Pass {
		t.Fatal("expected Pass=false")
	}
	var me *match.Error
	if !errors.As(r.Reason, &me) {
		t.Fatalf("expected *match.Error, got %T", r.Reason)
	}
	if me.Code != match.ErrArgDenied {
		t.Errorf("Code = %q, want %q", me.Code, match.ErrArgDenied)
	}
	if me.Token != "--include-from=ignore.txt" {
		t.Errorf("Token = %q, want %q", me.Token, "--include-from=ignore.txt")
	}
}

// TestParseCommandSet_IntegrationWithSeparateCommands tests the full
// round-trip: SeparateCommands → ParseCommandSet.
func TestParseCommandSet_IntegrationWithSeparateCommands(t *testing.T) {
	p := mustParser(t)
	yaml := mustReadYAML(t)
	cases := []struct {
		input     string
		wantPass  bool
		wantToken string // token that should be denied, if any
	}{
		{"ls -la", true, ""},
		{"git --no-pager log --oneline", true, ""},
		{"ls -la | grep foo", true, ""},
		{"ls -la | grep -f patterns.txt", false, "-f"},
		{"tail -f /var/log/syslog", false, "-f"},
		{"find . -delete", false, "-delete"},         // matcher denial
		{"find . -delete | wc -l", false, "-delete"}, // fails on first segment
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			cs, err := SeparateCommands(tc.input)
			if err != nil {
				t.Fatalf("SeparateCommands(%q): unexpected error: %v", tc.input, err)
			}
			r := p.ParseCommandSet(yaml, cs)
			if r.Pass != tc.wantPass {
				t.Errorf("Pass = %v, want %v (reason: %v)", r.Pass, tc.wantPass, r.Reason)
			}
			if !tc.wantPass && tc.wantToken != "" {
				var me *match.Error
				if errors.As(r.Reason, &me) && me.Token != tc.wantToken {
					t.Errorf("Token = %q, want %q", me.Token, tc.wantToken)
				}
			}
		})
	}
}

func stringSlicesEqual(a, b []string) bool {
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
