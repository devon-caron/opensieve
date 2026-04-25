package match

import (
	"errors"
	"strings"
	"testing"

	"github.com/devon-caron/opensieve/lex"
	"github.com/devon-caron/opensieve/tool"
)

// ---------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------

func mustMatcher(t *testing.T, spec *tool.ToolSpec) *Matcher {
	t.Helper()
	m, err := FromSpec(spec)
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}
	return m
}

func mustBuildFails(t *testing.T, spec *tool.ToolSpec, wantSubstr string) {
	t.Helper()
	_, err := BuildIndex(spec)
	if err == nil {
		t.Fatalf("expected build error, got nil")
	}
	if wantSubstr != "" && !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("error %q does not contain %q", err.Error(), wantSubstr)
	}
}

// seg builds a Segment from word values, synthesizing byte positions
// as if the words were joined with a single space. Positions reflect
// the offset of each word's first byte in the assembled string, which
// matches what the lexer would produce for the same input.
func seg(words ...string) Segment {
	tokens := make([]lex.Token, len(words))
	var raw strings.Builder
	pos := 0
	for i, w := range words {
		if i > 0 {
			raw.WriteByte(' ')
			pos++
		}
		tokens[i] = lex.Token{Kind: lex.TokWord, Value: w, Pos: pos}
		raw.WriteString(w)
		pos += len(w)
	}
	return Segment{Tokens: tokens, Raw: raw.String()}
}

// asError extracts a single *Error from err, whether it was returned
// directly or wrapped in Errors with len == 1.
func asError(t *testing.T, err error) *Error {
	t.Helper()
	var single *Error
	if errors.As(err, &single) {
		return single
	}
	var many Errors
	if errors.As(err, &many) && len(many) == 1 {
		return many[0]
	}
	t.Fatalf("expected single *Error, got %T: %v", err, err)
	return nil
}

// asErrors extracts an Errors slice from err, whether it was returned
// directly or promoted from a single *Error.
func asErrors(t *testing.T, err error) Errors {
	t.Helper()
	var many Errors
	if errors.As(err, &many) {
		return many
	}
	var single *Error
	if errors.As(err, &single) {
		return Errors{single}
	}
	t.Fatalf("expected Errors, got %T: %v", err, err)
	return nil
}

func countCodes(es Errors, code ErrorCode) int {
	n := 0
	for _, e := range es {
		if e.Code == code {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------
// Shared spec fixtures
// ---------------------------------------------------------------------

func simpleBlacklist(cmd string, deny ...tool.Argument) *tool.ToolSpec {
	return &tool.ToolSpec{
		Commands: []tool.Command{{
			Command:        cmd,
			Mode:           tool.CommandModeBlacklist,
			DisallowedArgs: deny,
		}},
	}
}

func simpleWhitelist(cmd string, allow ...tool.Argument) *tool.ToolSpec {
	return &tool.ToolSpec{
		Commands: []tool.Command{{
			Command:     cmd,
			Mode:        tool.CommandModeWhitelist,
			AllowedArgs: allow,
		}},
	}
}

// gitSpec mirrors the production-style git policy from read_tool.yaml.
//
// `--no-pager` is modeled as a subcommand of git (rather than a parent
// flag) so that strict subcommand routing forces every git invocation
// through the no-pager gate. `disallowed_sub_args` declared at git's
// level recursively reaches every leaf via the matcher's inheritance.
func gitSpec() *tool.ToolSpec {
	return &tool.ToolSpec{
		Commands: []tool.Command{
			{
				Command: "git",
				Mode:    tool.CommandModeWhitelist,
				Subcommands: &tool.SubcommandsConfig{
					DisallowedSubArgs: []tool.Argument{
						{Arg: "--ext-diff"},
						{Arg: "--textconv"},
						{Regex: "^--output(=.*)?$"},
					},
					Commands: []tool.Command{
						{
							Command: "--no-pager",
							Mode:    tool.CommandModeWhitelist,
							Subcommands: &tool.SubcommandsConfig{
								Commands: []tool.Command{
									{Command: "log", Mode: tool.CommandModeBlacklist},
									{Command: "status", Mode: tool.CommandModeBlacklist},
									{
										Command: "blame",
										Mode:    tool.CommandModeBlacklist,
										DisallowedArgs: []tool.Argument{
											{Regex: "^--contents(=.*)?$"},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------
// Step 1: top-level command routing
// ---------------------------------------------------------------------

func TestMatch_EmptySegment(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("ls"))
	_, err := m.Match(seg())
	e := asError(t, err)
	if e.Code != ErrEmptySegment {
		t.Errorf("code = %s, want %s", e.Code, ErrEmptySegment)
	}
}

func TestMatch_UnknownCommand(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("ls"))

	cases := []struct {
		name string
		argv []string
	}{
		{"completely unknown", []string{"rm", "-rf", "/"}},
		{"similar to allowed", []string{"ls2"}},
		{"case mismatch", []string{"LS"}},
		{"flag as first token", []string{"-la"}},
		{"empty string", []string{""}},
		{"leading slash", []string{"/bin/ls"}},
		{"relative path", []string{"./ls"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.Match(seg(tc.argv...))
			e := asError(t, err)
			if e.Code != ErrCommandNotAllowed {
				t.Errorf("code = %s, want %s", e.Code, ErrCommandNotAllowed)
			}
			if e.Token != tc.argv[0] {
				t.Errorf("token = %q, want %q", e.Token, tc.argv[0])
			}
		})
	}
}

func TestMatch_CommandOnlyNoArgs(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("ls"))
	r, err := m.Match(seg("ls"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Path) != 1 || r.Path[0] != "ls" {
		t.Errorf("path = %v, want [ls]", r.Path)
	}
	if len(r.Argv) != 1 {
		t.Errorf("argv = %v, want [ls]", r.Argv)
	}
}

// ---------------------------------------------------------------------
// Step 2: blacklist mode semantics
// ---------------------------------------------------------------------

func TestMatch_BlacklistAllowsByDefault(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("ls"))

	cases := [][]string{
		{"ls"},
		{"ls", "-la"},
		{"ls", "-la", "src/"},
		{"ls", "--color=never", "-R", "path"},
		{"ls", "anything", "goes", "here"},
	}
	for _, argv := range cases {
		t.Run(strings.Join(argv, "_"), func(t *testing.T) {
			_, err := m.Match(seg(argv...))
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestMatch_BlacklistRejectsListed(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("find",
		tool.Argument{Arg: "-exec"},
		tool.Argument{Arg: "-delete"},
		tool.Argument{Arg: "-execdir"},
	))

	cases := []struct {
		name  string
		argv  []string
		token string
	}{
		{"deny -exec", []string{"find", ".", "-exec"}, "-exec"},
		{"deny -delete", []string{"find", ".", "-delete"}, "-delete"},
		{"deny -execdir", []string{"find", ".", "-execdir"}, "-execdir"},
		{"deny at end", []string{"find", "-name", "*.go", "-exec"}, "-exec"},
		{"deny at start", []string{"find", "-exec", "rm"}, "-exec"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.Match(seg(tc.argv...))
			e := asError(t, err)
			if e.Code != ErrArgDenied {
				t.Errorf("code = %s, want %s", e.Code, ErrArgDenied)
			}
			if e.Token != tc.token {
				t.Errorf("token = %q, want %q", e.Token, tc.token)
			}
		})
	}
}

func TestMatch_BlacklistEmptyDenyList(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("cat"))
	_, err := m.Match(seg("cat", "--anything", "--at", "all"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------
// Step 3: whitelist mode semantics
// ---------------------------------------------------------------------

func TestMatch_WhitelistAcceptsListed(t *testing.T) {
	m := mustMatcher(t, simpleWhitelist("git",
		tool.Argument{Arg: "--no-pager"},
		tool.Argument{Arg: "--version"},
		tool.Argument{Arg: "--help"},
	))

	cases := [][]string{
		{"git"},
		{"git", "--no-pager"},
		{"git", "--version"},
		{"git", "--help"},
		{"git", "--no-pager", "--version"},
	}
	for _, argv := range cases {
		t.Run(strings.Join(argv, "_"), func(t *testing.T) {
			_, err := m.Match(seg(argv...))
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestMatch_WhitelistRejectsUnlisted(t *testing.T) {
	m := mustMatcher(t, simpleWhitelist("git",
		tool.Argument{Arg: "--no-pager"},
		tool.Argument{Arg: "--version"},
	))

	cases := []struct {
		name  string
		argv  []string
		token string
	}{
		{"unknown flag", []string{"git", "--unknown"}, "--unknown"},
		{"positional", []string{"git", "log"}, "log"},
		{"mixed known+unknown", []string{"git", "--no-pager", "--unknown"}, "--unknown"},
		{"empty token", []string{"git", ""}, ""},
		{"similar but not equal", []string{"git", "--no-pagers"}, "--no-pagers"},
		{"case mismatch", []string{"git", "--NO-PAGER"}, "--NO-PAGER"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.Match(seg(tc.argv...))
			e := asError(t, err)
			if e.Code != ErrArgNotAllowed {
				t.Errorf("code = %s, want %s", e.Code, ErrArgNotAllowed)
			}
			if e.Token != tc.token {
				t.Errorf("token = %q, want %q", e.Token, tc.token)
			}
		})
	}
}

func TestMatch_WhitelistEmptyAllowList(t *testing.T) {
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "git",
			Mode:    tool.CommandModeWhitelist,
		}},
	}
	m := mustMatcher(t, spec)

	_, err := m.Match(seg("git"))
	if err != nil {
		t.Errorf("bare git should match: %v", err)
	}

	_, err = m.Match(seg("git", "--anything"))
	e := asError(t, err)
	if e.Code != ErrArgNotAllowed {
		t.Errorf("code = %s, want %s", e.Code, ErrArgNotAllowed)
	}
}

// ---------------------------------------------------------------------
// Step 4: argument type — exact match
// ---------------------------------------------------------------------

func TestMatch_ExactArgMatchesOnlyExactString(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("x",
		tool.Argument{Arg: "--foo"},
	))

	_, err := m.Match(seg("x", "--foo"))
	e := asError(t, err)
	if e.Code != ErrArgDenied {
		t.Fatalf("expected denial of --foo, got %v", err)
	}

	passCases := []string{
		"--foobar",
		"--foo=bar",
		"foo",
		"-foo",
		"--fo",
		"",
	}
	for _, c := range passCases {
		t.Run("pass_"+c, func(t *testing.T) {
			_, err := m.Match(seg("x", c))
			if err != nil {
				t.Errorf("unexpected denial of %q: %v", c, err)
			}
		})
	}
}

func TestMatch_ExactArgEmptyString(t *testing.T) {
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "x",
			Mode:    tool.CommandModeBlacklist,
			DisallowedArgs: []tool.Argument{
				{Arg: ""},
			},
		}},
	}
	mustBuildFails(t, spec, "no field set")
}

// ---------------------------------------------------------------------
// Step 5: argument type — regex match
// ---------------------------------------------------------------------

func TestMatch_RegexMatchesAnchoredPatterns(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("x",
		tool.Argument{Regex: "^--output(=.*)?$"},
	))

	denyCases := []string{
		"--output",
		"--output=",
		"--output=/tmp/x",
		"--output=anything goes here too",
	}
	for _, c := range denyCases {
		t.Run("deny_"+c, func(t *testing.T) {
			_, err := m.Match(seg("x", c))
			e := asError(t, err)
			if e.Code != ErrArgDenied {
				t.Errorf("%q: code = %s, want %s", c, e.Code, ErrArgDenied)
			}
		})
	}

	passCases := []string{
		"--outputs",
		"--output-dir",
		"-output",
		"output",
		"--OUTPUT",
		"--output-file=x",
	}
	for _, c := range passCases {
		t.Run("pass_"+c, func(t *testing.T) {
			_, err := m.Match(seg("x", c))
			if err != nil {
				t.Errorf("%q: unexpected denial: %v", c, err)
			}
		})
	}
}

func TestMatch_RegexUnanchoredWarning(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("x",
		tool.Argument{Regex: "output"},
	))

	for _, c := range []string{"output", "--output", "foutputbar", "--output=x"} {
		t.Run("deny_"+c, func(t *testing.T) {
			_, err := m.Match(seg("x", c))
			e := asError(t, err)
			if e.Code != ErrArgDenied {
				t.Errorf("%q: code = %s, want %s", c, e.Code, ErrArgDenied)
			}
		})
	}
}

func TestMatch_RegexShortOptionGlob(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("rg",
		tool.Argument{Regex: "^-O.*$"},
	))

	denyCases := []string{"-O", "-Ovim", "-O/bin/sh", "-Ofoo=bar"}
	for _, c := range denyCases {
		t.Run("deny_"+c, func(t *testing.T) {
			_, err := m.Match(seg("rg", c))
			e := asError(t, err)
			if e.Code != ErrArgDenied {
				t.Errorf("%q: code = %s, want %s", c, e.Code, ErrArgDenied)
			}
		})
	}

	passCases := []string{"-o", "--O", "O"}
	for _, c := range passCases {
		t.Run("pass_"+c, func(t *testing.T) {
			_, err := m.Match(seg("rg", c))
			if err != nil {
				t.Errorf("%q: unexpected denial: %v", c, err)
			}
		})
	}
}

// ---------------------------------------------------------------------
// Step 6: argument type — path_spec match
// ---------------------------------------------------------------------

func TestMatch_PathSpecDenySingleStar(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("rm",
		tool.Argument{PathSpec: "/etc/*"},
	))

	denyCases := []string{"/etc/passwd", "/etc/shadow", "/etc/hostname"}
	for _, c := range denyCases {
		t.Run("deny_"+c, func(t *testing.T) {
			_, err := m.Match(seg("rm", c))
			e := asError(t, err)
			if e.Code != ErrArgDenied {
				t.Errorf("%q: code = %s, want %s", c, e.Code, ErrArgDenied)
			}
		})
	}

	passCases := []string{
		"/etc/ssl/cert.pem",
		"/etc",
		"/var/log/syslog",
		"etc/passwd",
	}
	for _, c := range passCases {
		t.Run("pass_"+c, func(t *testing.T) {
			_, err := m.Match(seg("rm", c))
			if err != nil {
				t.Errorf("%q: unexpected denial: %v", c, err)
			}
		})
	}
}

func TestMatch_PathSpecDenyDoublestar(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("rm",
		tool.Argument{PathSpec: "/etc/**"},
	))

	denyCases := []string{
		"/etc/passwd",
		"/etc/ssl/cert.pem",
		"/etc/deep/nested/file",
	}
	for _, c := range denyCases {
		t.Run("deny_"+c, func(t *testing.T) {
			_, err := m.Match(seg("rm", c))
			e := asError(t, err)
			if e.Code != ErrArgDenied {
				t.Errorf("%q: code = %s, want %s", c, e.Code, ErrArgDenied)
			}
		})
	}
}

func TestMatch_PathSpecExtensionGlob(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("x",
		tool.Argument{PathSpec: "*.pem"},
	))

	_, err := m.Match(seg("x", "cert.pem"))
	if !errors.Is(err, &Error{Code: ErrArgDenied}) {
		t.Errorf("*.pem should match cert.pem, got %v", err)
	}
	_, err = m.Match(seg("x", "cert.crt"))
	if err != nil {
		t.Errorf("*.pem should not match cert.crt, got %v", err)
	}
}

// ---------------------------------------------------------------------
// Step 7: subcommand routing (the previously-broken cases)
// ---------------------------------------------------------------------

func TestMatch_SubcommandRoutesToOwnEntry(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	cases := []struct {
		name string
		argv []string
		path []string
	}{
		{"git log", []string{"git", "--no-pager", "log", "--oneline"}, []string{"git", "--no-pager", "log"}},
		{"git status", []string{"git", "--no-pager", "status"}, []string{"git", "--no-pager", "status"}},
		{"git blame", []string{"git", "--no-pager", "blame", "file.go"}, []string{"git", "--no-pager", "blame"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := m.Match(seg(tc.argv...))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(r.Path) != len(tc.path) {
				t.Fatalf("path length = %d, want %d", len(r.Path), len(tc.path))
			}
			for i := range tc.path {
				if r.Path[i] != tc.path[i] {
					t.Errorf("path[%d] = %q, want %q", i, r.Path[i], tc.path[i])
				}
			}
		})
	}
}

func TestMatch_UnknownSubcommandFallsThrough(t *testing.T) {
	// Per design: an unknown token at any routing level is reported as
	// ErrArgNotAllowed at that token's position, with the level's known
	// subs attached. Routing fails fast — anything past the unknown
	// token is unreached.
	m := mustMatcher(t, gitSpec())

	cases := []struct {
		name  string
		argv  []string
		token string
	}{
		{"git stash (unknown leaf-2 sub)", []string{"git", "--no-pager", "stash"}, "stash"},
		{"git push (unknown leaf-2 sub)", []string{"git", "--no-pager", "push", "origin"}, "push"},
		{"git <typo>", []string{"git", "--no-pager", "lgo"}, "lgo"},
		{"git foo (unknown level-1 sub)", []string{"git", "foo"}, "foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.Match(seg(tc.argv...))
			e := asError(t, err)
			if e.Code != ErrArgNotAllowed {
				t.Errorf("code = %s, want %s", e.Code, ErrArgNotAllowed)
			}
			if e.Token != tc.token {
				t.Errorf("token = %q, want %q", e.Token, tc.token)
			}
			if len(e.Subs) == 0 {
				t.Errorf("expected Subs to be populated for routing-stage failure")
			}
		})
	}
}

func TestMatch_SubcommandWithNoArgsAfter(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	r, err := m.Match(seg("git", "--no-pager", "log"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Path) != 3 || r.Path[2] != "log" {
		t.Errorf("path = %v, want [git --no-pager log]", r.Path)
	}
}

func TestMatch_CommandWithSubsButNoArg(t *testing.T) {
	// `git` alone: no args, no routing. Validation runs against the
	// top entry with an empty args list — git has no own required
	// args in the new model, so it passes. (The spec relies on
	// routing, not required_args, to force --no-pager.)
	m := mustMatcher(t, gitSpec())

	_, err := m.Match(seg("git"))
	if err != nil {
		t.Errorf("bare git should pass under strict routing model: %v", err)
	}
}

// TestMatch_StrictRoutingRequiresKnownSub verifies that a non-sub
// token immediately after a parent that has subs is rejected at
// routing time. In the current model, allow-listing extra parent
// flags requires modeling them as subs — there is no per-level
// allowed_args pass-through during routing.
func TestMatch_StrictRoutingRequiresKnownSub(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	// git has one sub: --no-pager. Anything else fails at position 4.
	_, err := m.Match(seg("git", "log", "--oneline"))
	e := asError(t, err)
	if e.Code != ErrArgNotAllowed {
		t.Fatalf("code = %s, want %s", e.Code, ErrArgNotAllowed)
	}
	if e.Token != "log" {
		t.Errorf("token = %q, want log", e.Token)
	}
	if e.Pos != 4 {
		t.Errorf("pos = %d, want 4", e.Pos)
	}
}

// ---------------------------------------------------------------------
// Step 8: SubcommandsConfig inheritance (recursive disallow)
// ---------------------------------------------------------------------

func TestMatch_SubcommandInheritsDisallowed(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	cases := []struct {
		name  string
		argv  []string
		token string
	}{
		{
			"log with --textconv",
			[]string{"git", "--no-pager", "log", "--textconv"},
			"--textconv",
		},
		{
			"status with --ext-diff",
			[]string{"git", "--no-pager", "status", "--ext-diff"},
			"--ext-diff",
		},
		{
			"log with --output=file",
			[]string{"git", "--no-pager", "log", "--output=/tmp/x"},
			"--output=/tmp/x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.Match(seg(tc.argv...))
			errs := asErrors(t, err)

			found := false
			for _, e := range errs {
				if e.Code == ErrArgDenied && e.Token == tc.token {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected denial of %q in %v", tc.token, errs)
			}
		})
	}
}

func TestMatch_SubcommandOwnAndInheritedCombine(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	cases := []struct {
		name  string
		token string
	}{
		{"own_contents", "--contents=/tmp/x"},
		{"inherited_textconv", "--textconv"},
		{"inherited_output", "--output=/tmp/y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.Match(seg("git", "--no-pager", "blame",
				"file.go", tc.token))
			errs := asErrors(t, err)
			if countCodes(errs, ErrArgDenied) == 0 {
				t.Errorf("expected ErrArgDenied for %q in %v",
					tc.token, errs)
			}
		})
	}
}

// TestMatch_DisallowedInheritsRecursively verifies that a denial
// declared on a grandparent reaches every descendant, not just direct
// children. With gitSpec, `--textconv` is declared at git's level but
// must surface on `log` (two levels down) via `--no-pager`.
func TestMatch_DisallowedInheritsRecursively(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	_, err := m.Match(seg("git", "--no-pager", "log", "--textconv"))
	e := asError(t, err)
	if e.Code != ErrArgDenied {
		t.Fatalf("code = %s, want %s", e.Code, ErrArgDenied)
	}
	if e.Token != "--textconv" {
		t.Errorf("token = %q, want --textconv", e.Token)
	}
	// Source must cite git's subcommands block (the grandparent), not
	// --no-pager's (which declares no disallows of its own).
	if !strings.Contains(e.Source, "git.subcommands.disallowed_sub_args") {
		t.Errorf("source = %q, want it to cite git.subcommands.disallowed_sub_args",
			e.Source)
	}
	if !strings.Contains(e.Source, "(inherited)") {
		t.Errorf("source = %q, want (inherited) marker", e.Source)
	}
}

// ---------------------------------------------------------------------
// Step 9: required args
// ---------------------------------------------------------------------

func TestMatch_RequiredArgPresent(t *testing.T) {
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "cmd",
			Mode:    tool.CommandModeBlacklist,
			RequiredArgs: []tool.Argument{
				{Arg: "--required"},
			},
		}},
	}
	m := mustMatcher(t, spec)

	cases := [][]string{
		{"cmd", "--required"},
		{"cmd", "--required", "other"},
		{"cmd", "other", "--required"},
		{"cmd", "before", "--required", "after"},
	}
	for _, argv := range cases {
		t.Run(strings.Join(argv, "_"), func(t *testing.T) {
			_, err := m.Match(seg(argv...))
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestMatch_RequiredArgMissing(t *testing.T) {
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "cmd",
			Mode:    tool.CommandModeBlacklist,
			RequiredArgs: []tool.Argument{
				{Arg: "--required"},
			},
		}},
	}
	m := mustMatcher(t, spec)

	cases := [][]string{
		{"cmd"},
		{"cmd", "--other"},
		{"cmd", "positional"},
		{"cmd", "--require", "--required-ish"},
	}
	for _, argv := range cases {
		t.Run(strings.Join(argv, "_"), func(t *testing.T) {
			_, err := m.Match(seg(argv...))
			errs := asErrors(t, err)
			if countCodes(errs, ErrMissingRequired) != 1 {
				t.Errorf("expected 1 ErrMissingRequired, got %v", errs)
			}
		})
	}
}

func TestMatch_RequiredArgRegexMatch(t *testing.T) {
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "cmd",
			Mode:    tool.CommandModeBlacklist,
			RequiredArgs: []tool.Argument{
				{Regex: "^--config=.+$"},
			},
		}},
	}
	m := mustMatcher(t, spec)

	passCases := [][]string{
		{"cmd", "--config=file.yaml"},
		{"cmd", "other", "--config=x"},
	}
	for _, argv := range passCases {
		t.Run("pass_"+strings.Join(argv, "_"), func(t *testing.T) {
			_, err := m.Match(seg(argv...))
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}

	failCases := [][]string{
		{"cmd"},
		{"cmd", "--config="},
		{"cmd", "--config"},
	}
	for _, argv := range failCases {
		t.Run("fail_"+strings.Join(argv, "_"), func(t *testing.T) {
			_, err := m.Match(seg(argv...))
			errs := asErrors(t, err)
			if countCodes(errs, ErrMissingRequired) == 0 {
				t.Errorf("expected ErrMissingRequired for %v", argv)
			}
		})
	}
}

func TestMatch_MultipleRequiredArgs(t *testing.T) {
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "cmd",
			Mode:    tool.CommandModeBlacklist,
			RequiredArgs: []tool.Argument{
				{Arg: "--one"},
				{Arg: "--two"},
				{Arg: "--three"},
			},
		}},
	}
	m := mustMatcher(t, spec)

	_, err := m.Match(seg("cmd", "--one", "--two", "--three"))
	if err != nil {
		t.Errorf("all required present, got error: %v", err)
	}

	_, err = m.Match(seg("cmd", "--one"))
	errs := asErrors(t, err)
	if countCodes(errs, ErrMissingRequired) != 2 {
		t.Errorf("expected 2 missing, got %v", errs)
	}

	_, err = m.Match(seg("cmd"))
	errs = asErrors(t, err)
	if countCodes(errs, ErrMissingRequired) != 3 {
		t.Errorf("expected 3 missing, got %v", errs)
	}
}

// ---------------------------------------------------------------------
// Step 10: multiple-error collection
// ---------------------------------------------------------------------

func TestMatch_CollectsMultipleDenials(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("find",
		tool.Argument{Arg: "-exec"},
		tool.Argument{Arg: "-delete"},
		tool.Argument{Arg: "-fprint"},
	))

	_, err := m.Match(seg("find", ".", "-exec", "-delete", "-fprint"))
	errs := asErrors(t, err)
	if countCodes(errs, ErrArgDenied) != 3 {
		t.Errorf("expected 3 denials, got %v", errs)
	}
}

func TestMatch_CollectsDenialsAndMissing(t *testing.T) {
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "find",
			Mode:    tool.CommandModeBlacklist,
			DisallowedArgs: []tool.Argument{
				{Arg: "-exec"},
				{Arg: "-delete"},
			},
			RequiredArgs: []tool.Argument{
				{Arg: "--never-present"},
			},
		}},
	}
	m := mustMatcher(t, spec)

	_, err := m.Match(seg("find", ".", "-exec", "cmd", "-delete"))
	errs := asErrors(t, err)
	if len(errs) != 3 {
		t.Fatalf("expected 3 errors (2 denied + 1 missing), got %d: %v",
			len(errs), errs)
	}
	if countCodes(errs, ErrArgDenied) != 2 {
		t.Errorf("deny count = %d, want 2", countCodes(errs, ErrArgDenied))
	}
	if countCodes(errs, ErrMissingRequired) != 1 {
		t.Errorf("missing count = %d, want 1", countCodes(errs, ErrMissingRequired))
	}
}

func TestMatch_CollectsWhitelistViolations(t *testing.T) {
	m := mustMatcher(t, simpleWhitelist("git",
		tool.Argument{Arg: "--no-pager"},
	))

	_, err := m.Match(seg("git", "--unknown1", "--unknown2", "--unknown3"))
	errs := asErrors(t, err)
	if countCodes(errs, ErrArgNotAllowed) != 3 {
		t.Errorf("expected 3 not-allowed, got %v", errs)
	}
}

func TestMatch_ErrorsContainsToken(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("find",
		tool.Argument{Arg: "-exec"},
		tool.Argument{Arg: "-delete"},
	))

	_, err := m.Match(seg("find", "-exec", "-delete"))
	errs := asErrors(t, err)

	tokens := map[string]bool{}
	for _, e := range errs {
		tokens[e.Token] = true
	}
	if !tokens["-exec"] || !tokens["-delete"] {
		t.Errorf("expected both tokens in errors, got %v", errs)
	}
}

// ---------------------------------------------------------------------
// Step 11: Result correctness
// ---------------------------------------------------------------------

func TestMatch_ResultArgvUnchanged(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("ls"))
	argv := []string{"ls", "-la", "src/"}
	r, err := m.Match(seg(argv...))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Argv) != len(argv) {
		t.Fatalf("argv length mismatch")
	}
	for i := range argv {
		if r.Argv[i] != argv[i] {
			t.Errorf("argv[%d] = %q, want %q", i, r.Argv[i], argv[i])
		}
	}
}

func TestMatch_ResultEntryPointsToMatchedCommand(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	// Bare `git` doesn't descend into any sub: entry stays at git.
	r, err := m.Match(seg("git"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Entry.Command != "git" {
		t.Errorf("entry = %q, want git", r.Entry.Command)
	}

	// Routing descends through every matched sub: deepest entry wins.
	r, err = m.Match(seg("git", "--no-pager", "log", "--oneline"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Entry.Command != "log" {
		t.Errorf("entry = %q, want log", r.Entry.Command)
	}
}

// ---------------------------------------------------------------------
// Step 12: load-time (BuildIndex) validation
// ---------------------------------------------------------------------

func TestBuildIndex_EmptyCommandName(t *testing.T) {
	mustBuildFails(t, &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "",
			Mode:    tool.CommandModeBlacklist,
		}},
	}, "empty")
}

func TestBuildIndex_DuplicateCommand(t *testing.T) {
	mustBuildFails(t, &tool.ToolSpec{
		Commands: []tool.Command{
			{Command: "x", Mode: tool.CommandModeBlacklist},
			{Command: "x", Mode: tool.CommandModeBlacklist},
		},
	}, "duplicate")
}

func TestBuildIndex_DuplicateSubcommand(t *testing.T) {
	mustBuildFails(t, &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "x",
			Mode:    tool.CommandModeBlacklist,
			Subcommands: &tool.SubcommandsConfig{
				Commands: []tool.Command{
					{Command: "sub", Mode: tool.CommandModeBlacklist},
					{Command: "sub", Mode: tool.CommandModeBlacklist},
				},
			},
		}},
	}, "duplicate")
}

func TestBuildIndex_InvalidRegex(t *testing.T) {
	mustBuildFails(t, &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "x",
			Mode:    tool.CommandModeBlacklist,
			DisallowedArgs: []tool.Argument{
				{Regex: "[unclosed"},
			},
		}},
	}, "regex")
}

func TestBuildIndex_InvalidRegexInRequired(t *testing.T) {
	mustBuildFails(t, &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "x",
			Mode:    tool.CommandModeBlacklist,
			RequiredArgs: []tool.Argument{
				{Regex: "[unclosed"},
			},
		}},
	}, "regex")
}

func TestBuildIndex_InvalidRegexInInherited(t *testing.T) {
	mustBuildFails(t, &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "x",
			Mode:    tool.CommandModeBlacklist,
			Subcommands: &tool.SubcommandsConfig{
				DisallowedSubArgs: []tool.Argument{
					{Regex: "[unclosed"},
				},
				Commands: []tool.Command{
					{Command: "sub", Mode: tool.CommandModeBlacklist},
				},
			},
		}},
	}, "regex")
}

func TestBuildIndex_ArgumentMultipleFieldsSet(t *testing.T) {
	cases := []struct {
		name string
		arg  tool.Argument
	}{
		{"arg+regex", tool.Argument{Arg: "x", Regex: "y"}},
		{"arg+path", tool.Argument{Arg: "x", PathSpec: "y"}},
		{"regex+path", tool.Argument{Regex: "x", PathSpec: "y"}},
		{"all three", tool.Argument{Arg: "x", Regex: "y", PathSpec: "z"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mustBuildFails(t, &tool.ToolSpec{
				Commands: []tool.Command{{
					Command:        "x",
					Mode:           tool.CommandModeBlacklist,
					DisallowedArgs: []tool.Argument{tc.arg},
				}},
			}, "multiple fields")
		})
	}
}

func TestBuildIndex_ArgumentAllFieldsEmpty(t *testing.T) {
	mustBuildFails(t, &tool.ToolSpec{
		Commands: []tool.Command{{
			Command:        "x",
			Mode:           tool.CommandModeBlacklist,
			DisallowedArgs: []tool.Argument{{}},
		}},
	}, "no field")
}

func TestBuildIndex_ValidPolicyLoads(t *testing.T) {
	_, err := BuildIndex(gitSpec())
	if err != nil {
		t.Errorf("gitSpec should build, got %v", err)
	}
}

// ---------------------------------------------------------------------
// Step 13: miscellaneous edge cases
// ---------------------------------------------------------------------

func TestMatch_UnknownModeIsFailClosed(t *testing.T) {
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "x",
			Mode:    tool.CommandMode("bogus"),
		}},
	}
	m := mustMatcher(t, spec)

	_, err := m.Match(seg("x"))
	if err != nil {
		t.Errorf("bare command with unknown mode should pass: %v", err)
	}

	_, err = m.Match(seg("x", "anything"))
	e := asError(t, err)
	if e.Code != ErrArgNotAllowed {
		t.Errorf("unknown mode should fail closed, got %s", e.Code)
	}
}

func TestMatch_SameTokenRepeatedInArgv(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("x",
		tool.Argument{Arg: "--bad"},
	))

	_, err := m.Match(seg("x", "--bad", "--bad", "--bad"))
	errs := asErrors(t, err)
	if countCodes(errs, ErrArgDenied) != 3 {
		t.Errorf("expected 3 denials, got %v", errs)
	}
}

func TestMatch_OverlappingPatternsFirstMatchWins(t *testing.T) {
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "x",
			Mode:    tool.CommandModeBlacklist,
			DisallowedArgs: []tool.Argument{
				{Regex: "^--.*$"},
				{Arg: "--output"},
			},
		}},
	}
	m := mustMatcher(t, spec)

	_, err := m.Match(seg("x", "--output"))
	e := asError(t, err)
	if !strings.HasPrefix(e.Pattern, "regex ") {
		t.Errorf("expected first (regex) pattern to match, got pattern %q",
			e.Pattern)
	}
}

func TestMatch_ErrorContainsCommandPath(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	_, err := m.Match(seg("git", "--no-pager", "log", "--textconv"))
	e := asError(t, err)
	if e.Command != "git --no-pager log" {
		t.Errorf("command path = %q, want git --no-pager log", e.Command)
	}
}

func TestMatch_ErrorsInterfaceUnwrap(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("x",
		tool.Argument{Arg: "-bad"},
	))

	_, err := m.Match(seg("x", "-bad", "-bad"))
	if !errors.Is(err, &Error{Code: ErrArgDenied}) {
		t.Errorf("errors.Is should find ErrArgDenied in %v", err)
	}
	if errors.Is(err, &Error{Code: ErrMissingRequired}) {
		t.Errorf("errors.Is should NOT find ErrMissingRequired in %v", err)
	}
}

func TestMatch_EmptyTokenInArgv(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("x"))
	_, err := m.Match(seg("x", "", "something"))
	if err != nil {
		t.Errorf("empty token in blacklist should pass: %v", err)
	}

	m = mustMatcher(t, simpleWhitelist("x",
		tool.Argument{Arg: "--foo"},
	))
	_, err = m.Match(seg("x", ""))
	e := asError(t, err)
	if e.Code != ErrArgNotAllowed {
		t.Errorf("empty token in whitelist: code = %s, want %s",
			e.Code, ErrArgNotAllowed)
	}
	if e.Token != "" {
		t.Errorf("token should be empty string, got %q", e.Token)
	}
}

// ---------------------------------------------------------------------
// Step 14: descriptive-error coverage (new)
// ---------------------------------------------------------------------

// TestMatch_PositionTracking verifies that errors carry the byte
// offset of the offending token in the original input. The seg
// helper synthesizes positions as if words were single-space
// separated, matching what the lexer would produce.
func TestMatch_PositionTracking(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	// "git --no-pager push origin"
	//  0    4          15   20
	_, err := m.Match(seg("git", "--no-pager", "push", "origin"))
	e := asError(t, err)
	if e.Token != "push" {
		t.Fatalf("token = %q, want push", e.Token)
	}
	if e.Pos != 15 {
		t.Errorf("pos = %d, want 15", e.Pos)
	}
}

func TestMatch_PositionTrackingDenial(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	// "git --no-pager log --textconv"
	//                     19
	_, err := m.Match(seg("git", "--no-pager", "log", "--textconv"))
	e := asError(t, err)
	if e.Token != "--textconv" {
		t.Fatalf("token = %q, want --textconv", e.Token)
	}
	if e.Pos != 19 {
		t.Errorf("pos = %d, want 19", e.Pos)
	}
}

// TestMatch_ErrorRendersDescriptive verifies Error.Error() output
// includes the headline, token + position, source provenance, the
// allowed/subs lists where relevant, and a fix line.
func TestMatch_ErrorRendersDescriptiveDenial(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	_, err := m.Match(seg("git", "--no-pager", "log", "--textconv"))
	e := asError(t, err)
	out := e.Error()

	wants := []string{
		`arg_denied`,
		`for command "git --no-pager log"`,
		`token:`,
		`"--textconv"`,
		`at byte 19`,
		`rule:`,
		`subcommands.disallowed_sub_args`,
		`(inherited)`,
		`fix:`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("Error() missing %q\nfull output:\n%s", w, out)
		}
	}
}

func TestMatch_ErrorRendersDescriptiveUnknownSub(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	// Unknown sub at the leaf-2 position (past `--no-pager`). The
	// reported Subs should list `--no-pager`'s children (log, status,
	// blame), not git's direct subs (just `--no-pager`).
	_, err := m.Match(seg("git", "--no-pager", "push", "origin"))
	e := asError(t, err)
	out := e.Error()

	wants := []string{
		`arg_not_allowed`,
		`for command "git --no-pager"`,
		`"push"`,
		`at byte 15`,
		`subs:`,
		`log`,
		`status`,
		`blame`,
		`fix:`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("Error() missing %q\nfull output:\n%s", w, out)
		}
	}
}

func TestMatch_ErrorRendersDescriptiveUnknownCommand(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	_, err := m.Match(seg("rm", "-rf", "/"))
	e := asError(t, err)
	out := e.Error()

	wants := []string{
		`command_not_allowed`,
		`"rm"`,
		`at byte 0`,
		`allowed:`,
		`git`,
		`fix:`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("Error() missing %q\nfull output:\n%s", w, out)
		}
	}
}

// TestMatch_ErrorsAggregation verifies the multi-error renderer
// numbers and indents each child.
func TestMatch_ErrorsAggregation(t *testing.T) {
	m := mustMatcher(t, simpleBlacklist("find",
		tool.Argument{Arg: "-exec"},
		tool.Argument{Arg: "-delete"},
	))

	_, err := m.Match(seg("find", "-exec", "-delete"))
	out := err.Error()

	wants := []string{
		`match: 2 errors:`,
		`1.`,
		`2.`,
		`-exec`,
		`-delete`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("Errors.Error() missing %q\nfull output:\n%s", w, out)
		}
	}
}

// TestMatch_SourceProvenanceFormat sanity-checks the YAML-path-style
// source strings produced by BuildIndex.
func TestMatch_SourceProvenanceFormat(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	cases := []struct {
		name       string
		argv       []string
		wantSource string
	}{
		{
			"inherited disallowed (from grandparent git)",
			[]string{"git", "--no-pager", "log", "--textconv"},
			"git.subcommands.disallowed_sub_args[1] (inherited)",
		},
		{
			"own disallowed on subcommand",
			[]string{"git", "--no-pager", "blame", "file.go", "--contents=/tmp/x"},
			"git.--no-pager.blame.disallowed_args[0]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.Match(seg(tc.argv...))
			errs := asErrors(t, err)
			found := false
			for _, e := range errs {
				if e.Source == tc.wantSource {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected an error with Source = %q in %v",
					tc.wantSource, errs)
			}
		})
	}
}

// ---------------------------------------------------------------------
// Step 15: space-form hint for flag=value denials
// ---------------------------------------------------------------------

// TestMatch_EqualsHintFiresForAssignmentOnlyRegex verifies the hint is
// attached when the denial is specific to the `=` form and the bare
// flag would pass.
func TestMatch_EqualsHintFiresForAssignmentOnlyRegex(t *testing.T) {
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "rg",
			Mode:    tool.CommandModeBlacklist,
			DisallowedArgs: []tool.Argument{
				// = form denied; bare --file has no matching rule.
				{Regex: "^--file=.*$"},
			},
		}},
	}
	m := mustMatcher(t, spec)

	_, err := m.Match(seg("rg", "--file=patterns.txt"))
	e := asError(t, err)
	if e.Code != ErrArgDenied {
		t.Fatalf("code = %s, want %s", e.Code, ErrArgDenied)
	}
	if e.Hint == "" {
		t.Fatal("expected Hint to be populated for =-form-only denial")
	}
	for _, want := range []string{"--file", "patterns.txt", "space"} {
		if !strings.Contains(e.Hint, want) {
			t.Errorf("hint missing %q\nfull hint: %s", want, e.Hint)
		}
	}
	// Error() must render the hint on a "hint:" line.
	if !strings.Contains(e.Error(), "hint:") {
		t.Errorf("Error() missing hint line:\n%s", e.Error())
	}
}

// TestMatch_EqualsHintSuppressedWhenBareAlsoDenied verifies that the
// hint is NOT attached when the bare flag would also be denied —
// suggesting the space form would be misleading.
func TestMatch_EqualsHintSuppressedWhenBareAlsoDenied(t *testing.T) {
	// gitSpec denies `--textconv` via `arg: "--textconv"` which
	// matches the bare form. A `--textconv=anything` token (not
	// meaningfully valid in git, but permitted by the lexer) still
	// matches nothing in the denylist because the rule is exact.
	// Use diff-style `(=.*)?` pattern instead.
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "diff",
			Mode:    tool.CommandModeBlacklist,
			DisallowedArgs: []tool.Argument{
				// Covers both bare and =value.
				{Regex: "^--to-file(=.*)?$"},
			},
		}},
	}
	m := mustMatcher(t, spec)

	_, err := m.Match(seg("diff", "--to-file=/etc/shadow"))
	e := asError(t, err)
	if e.Code != ErrArgDenied {
		t.Fatalf("code = %s, want %s", e.Code, ErrArgDenied)
	}
	if e.Hint != "" {
		t.Errorf("hint should be suppressed when bare form is also denied, got %q",
			e.Hint)
	}
}

// TestMatch_EqualsHintIgnoredForNonEqualsToken verifies that a token
// without '=' never gets a hint, even on denial.
func TestMatch_EqualsHintIgnoredForNonEqualsToken(t *testing.T) {
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "tail",
			Mode:    tool.CommandModeBlacklist,
			DisallowedArgs: []tool.Argument{
				{Arg: "-f"},
			},
		}},
	}
	m := mustMatcher(t, spec)

	_, err := m.Match(seg("tail", "-f", "/var/log/x"))
	e := asError(t, err)
	if e.Hint != "" {
		t.Errorf("no-= token should never carry a hint, got %q", e.Hint)
	}
}

// TestMatch_EqualsHintInWhitelist verifies the symmetric case: when
// a leaf is whitelist-mode and only the bare flag is in AllowedArgs,
// a `flag=value` token fails with a hint to space-separate.
func TestMatch_EqualsHintInWhitelist(t *testing.T) {
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "cmd",
			Mode:    tool.CommandModeWhitelist,
			AllowedArgs: []tool.Argument{
				{Arg: "--name"},
			},
		}},
	}
	m := mustMatcher(t, spec)

	_, err := m.Match(seg("cmd", "--name=alice"))
	e := asError(t, err)
	if e.Code != ErrArgNotAllowed {
		t.Fatalf("code = %s, want %s", e.Code, ErrArgNotAllowed)
	}
	if e.Hint == "" {
		t.Fatal("expected Hint for =-form token when bare is in allow list")
	}
	for _, want := range []string{"--name", "alice", "space"} {
		if !strings.Contains(e.Hint, want) {
			t.Errorf("hint missing %q\nfull hint: %s", want, e.Hint)
		}
	}
}
