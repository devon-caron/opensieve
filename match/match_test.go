package match

import (
	"errors"
	"strings"
	"testing"

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

func seg(words ...string) Segment {
	return Segment{Words: words}
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

// simpleBlacklist returns a single-command blacklist spec for targeted tests.
func simpleBlacklist(cmd string, deny ...tool.Argument) *tool.ToolSpec {
	return &tool.ToolSpec{
		Commands: []tool.Command{{
			Command:        cmd,
			Mode:           tool.CommandModeBlacklist,
			DisallowedArgs: deny,
		}},
	}
}

// simpleWhitelist returns a single-command whitelist spec.
func simpleWhitelist(cmd string, allow ...tool.Argument) *tool.ToolSpec {
	return &tool.ToolSpec{
		Commands: []tool.Command{{
			Command:     cmd,
			Mode:        tool.CommandModeWhitelist,
			AllowedArgs: allow,
		}},
	}
}

// gitSpec returns a representative git spec for routing and
// inheritance tests. Mirrors the shape of the production policy.
func gitSpec() *tool.ToolSpec {
	return &tool.ToolSpec{
		Commands: []tool.Command{
			{
				Command: "git",
				Mode:    tool.CommandModeWhitelist,
				AllowedArgs: []tool.Argument{
					{Arg: "--no-pager"},
					{Arg: "--no-optional-locks"},
					{Arg: "--version"},
				},
				RequiredArgs: []tool.Argument{
					{Arg: "--no-pager"},
				},
				Subcommands: &tool.SubcommandsConfig{
					RequiredSubArgs: []tool.Argument{
						{Arg: "--no-pager"},
					},
					DisallowedSubArgs: []tool.Argument{
						{Arg: "--ext-diff"},
						{Arg: "--textconv"},
						{Regex: "^--output(=.*)?$"},
					},
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
	// A command name with no args should match cleanly if the
	// command has no RequiredArgs.
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
	// Blacklist with no entries allows everything.
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
	// Whitelist with no entries rejects every non-command token.
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "git",
			Mode:    tool.CommandModeWhitelist,
		}},
	}
	m := mustMatcher(t, spec)

	// Command alone is fine.
	_, err := m.Match(seg("git"))
	if err != nil {
		t.Errorf("bare git should match: %v", err)
	}

	// Any arg fails.
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

	// Exactly --foo is denied.
	_, err := m.Match(seg("x", "--foo"))
	e := asError(t, err)
	if e.Code != ErrArgDenied {
		t.Fatalf("expected denial of --foo, got %v", err)
	}

	// Substring and superstring are not denied.
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
	// Can't configure: loader rejects empty arg. Verify.
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
		"--outputs",       // not anchored at end
		"--output-dir",    // has non-= suffix
		"-output",         // wrong dash count
		"output",          // no dashes
		"--OUTPUT",        // case mismatch
		"--output-file=x", // superset
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
	// An unanchored regex matches substrings. This is the user's
	// problem (policy bug, not matcher bug), but we should verify
	// behavior is predictable.
	m := mustMatcher(t, simpleBlacklist("x",
		tool.Argument{Regex: "output"},
	))

	// All of these contain "output" and are denied.
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
	// The rg "-O<cmd>" case from the production policy: deny -O
	// and anything starting with -O.
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
		"/etc/ssl/cert.pem", // deeper path, single * doesn't cross /
		"/etc",              // not under /etc/
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
// Step 7: subcommand routing
// ---------------------------------------------------------------------

func TestMatch_SubcommandRoutesToOwnEntry(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	cases := []struct {
		name string
		argv []string
		path []string
	}{
		{"git log", []string{"git", "--no-pager", "log", "--no-pager", "--oneline"}, []string{"git", "log"}},
		{"git status", []string{"git", "--no-pager", "status", "--no-pager"}, []string{"git", "status"}},
		{"git blame", []string{"git", "--no-pager", "blame", "--no-pager", "file.go"}, []string{"git", "blame"}},
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
	// Per decision: unknown subcommand of a whitelist parent is
	// validated as an arg against the parent's AllowedArgs,
	// producing ErrArgNotAllowed.
	m := mustMatcher(t, gitSpec())

	cases := []struct {
		name  string
		argv  []string
		token string
	}{
		{"git stash (unknown sub)", []string{"git", "--no-pager", "stash"}, "stash"},
		{"git push (unknown sub)", []string{"git", "--no-pager", "push", "origin"}, "push"},
		{"git <typo>", []string{"git", "--no-pager", "lgo"}, "lgo"},
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

func TestMatch_SubcommandWithNoArgsAfter(t *testing.T) {
	// `git --no-pager log` with nothing after should route to log
	// and apply log's validation.
	m := mustMatcher(t, gitSpec())

	r, err := m.Match(seg("git", "--no-pager", "log", "--no-pager"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Path) != 2 || r.Path[1] != "log" {
		t.Errorf("path = %v, want [git log]", r.Path)
	}
}

func TestMatch_SubcommandBeforeTopLevelFlag(t *testing.T) {
	// If the first token after `git` is a subcommand name, routing
	// consumes it — even if a valid top-level flag would follow.
	// This is correct behavior; subcommand routing has priority.
	m := mustMatcher(t, gitSpec())

	r, err := m.Match(seg("git", "log", "--no-pager"))
	// The required --no-pager on git parent is missing because
	// "log" was consumed by routing, so the parent never saw
	// --no-pager. Actually, let's think: the parent's required_args
	// is checked on the parent path's args (none here since routing
	// consumed "log" first... wait, the parent validates args that
	// remain AFTER routing? No — the parent's validation runs when
	// the governing entry is the parent. When governing is the
	// subcommand, the subcommand's required_args are checked.
	//
	// With the current design, "git log --no-pager" routes to the
	// log subcommand, subcommand's required (inherited) is
	// --no-pager, which is present. Should pass.
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if r != nil && len(r.Path) != 2 {
		t.Errorf("path = %v, want [git log]", r.Path)
	}
}

func TestMatch_CommandWithSubsButNoArg(t *testing.T) {
	// `git` with no args at all. No subcommand to route to. The
	// parent governs. RequiredArgs includes --no-pager, so this
	// should fail with ErrMissingRequired.
	m := mustMatcher(t, gitSpec())

	_, err := m.Match(seg("git"))
	e := asError(t, err)
	if e.Code != ErrMissingRequired {
		t.Errorf("code = %s, want %s", e.Code, ErrMissingRequired)
	}
}

// ---------------------------------------------------------------------
// Step 8: SubcommandsConfig inheritance
// ---------------------------------------------------------------------

func TestMatch_SubcommandInheritsRequired(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	// Missing --no-pager should fail because it's in RequiredSubArgs.
	cases := [][]string{
		{"git", "--no-pager", "log"},    // parent has --no-pager but subcommand doesn't
		{"git", "--no-pager", "status"}, // same
		{"git", "--no-pager", "blame", "file.go"},
	}
	for _, argv := range cases {
		t.Run(strings.Join(argv, "_"), func(t *testing.T) {
			_, err := m.Match(seg(argv...))
			errs := asErrors(t, err)
			if countCodes(errs, ErrMissingRequired) == 0 {
				t.Errorf("expected ErrMissingRequired in %v", errs)
			}
		})
	}
}

func TestMatch_SubcommandInheritsDisallowed(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	cases := []struct {
		name  string
		argv  []string
		token string
	}{
		{
			"log with --textconv",
			[]string{"git", "--no-pager", "log", "--no-pager", "--textconv"},
			"--textconv",
		},
		{
			"status with --ext-diff",
			[]string{"git", "--no-pager", "status", "--no-pager", "--ext-diff"},
			"--ext-diff",
		},
		{
			"log with --output=file",
			[]string{"git", "--no-pager", "log", "--no-pager", "--output=/tmp/x"},
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
	// blame has its own DisallowedArgs (--contents) and also
	// inherits --textconv, --ext-diff, --output from the parent.
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
				"--no-pager", "file.go", tc.token))
			errs := asErrors(t, err)
			if countCodes(errs, ErrArgDenied) == 0 {
				t.Errorf("expected ErrArgDenied for %q in %v",
					tc.token, errs)
			}
		})
	}
}

func TestMatch_SubcommandInheritsWhitelistAllowed(t *testing.T) {
	// Test that AllowedSubArgs inheritance also works in whitelist
	// mode. The gitSpec uses blacklist subcommands, so use a
	// custom spec for this case.
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "cmd",
			Mode:    tool.CommandModeBlacklist,
			Subcommands: &tool.SubcommandsConfig{
				AllowedSubArgs: []tool.Argument{
					{Arg: "--global-ok"},
				},
				Commands: []tool.Command{{
					Command: "sub",
					Mode:    tool.CommandModeWhitelist,
					AllowedArgs: []tool.Argument{
						{Arg: "--sub-only"},
					},
				}},
			},
		}},
	}
	m := mustMatcher(t, spec)

	// Inherited arg should pass whitelist.
	_, err := m.Match(seg("cmd", "sub", "--global-ok"))
	if err != nil {
		t.Errorf("inherited allowed arg rejected: %v", err)
	}
	// Subcommand's own arg passes.
	_, err = m.Match(seg("cmd", "sub", "--sub-only"))
	if err != nil {
		t.Errorf("own allowed arg rejected: %v", err)
	}
	// Neither list mentions --other.
	_, err = m.Match(seg("cmd", "sub", "--other"))
	e := asError(t, err)
	if e.Code != ErrArgNotAllowed {
		t.Errorf("code = %s, want %s", e.Code, ErrArgNotAllowed)
	}
}

func TestMatch_NoInheritanceOnParentItself(t *testing.T) {
	// SubcommandsConfig lists should NOT affect the parent's own
	// validation — only its subcommands. Verify the parent's
	// validation ignores them.
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "cmd",
			Mode:    tool.CommandModeBlacklist,
			Subcommands: &tool.SubcommandsConfig{
				DisallowedSubArgs: []tool.Argument{
					{Arg: "--only-sub-denies-this"},
				},
				Commands: []tool.Command{{
					Command: "sub",
					Mode:    tool.CommandModeBlacklist,
				}},
			},
		}},
	}
	m := mustMatcher(t, spec)

	// Arg is disallowed for subcommand.
	_, err := m.Match(seg("cmd", "sub", "--only-sub-denies-this"))
	e := asError(t, err)
	if e.Code != ErrArgDenied {
		t.Errorf("subcommand should deny: code = %s", e.Code)
	}

	// But parent `cmd` alone accepts it — no inheritance to parent.
	_, err = m.Match(seg("cmd", "--only-sub-denies-this"))
	if err != nil {
		t.Errorf("parent should not inherit sub-denies: %v", err)
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
		{"cmd", "--config="}, // regex requires content after =
		{"cmd", "--config"},  // regex requires =
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

	// All present.
	_, err := m.Match(seg("cmd", "--one", "--two", "--three"))
	if err != nil {
		t.Errorf("all required present, got error: %v", err)
	}

	// Two missing — should get two errors.
	_, err = m.Match(seg("cmd", "--one"))
	errs := asErrors(t, err)
	if countCodes(errs, ErrMissingRequired) != 2 {
		t.Errorf("expected 2 missing, got %v", errs)
	}

	// None present — three errors.
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
	// Each collected error must identify its specific token.
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

	// Top-level entry.
	r, err := m.Match(seg("git", "--no-pager"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Entry.Command != "git" {
		t.Errorf("entry = %q, want git", r.Entry.Command)
	}

	// Subcommand entry.
	r, err = m.Match(seg("git", "--no-pager", "log", "--no-pager"))
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
	// Sanity check: gitSpec must build successfully.
	_, err := BuildIndex(gitSpec())
	if err != nil {
		t.Errorf("gitSpec should build, got %v", err)
	}
}

// ---------------------------------------------------------------------
// Step 13: miscellaneous edge cases
// ---------------------------------------------------------------------

func TestMatch_UnknownModeIsFailClosed(t *testing.T) {
	// A command with an unrecognized mode rejects all arguments.
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "x",
			Mode:    tool.CommandMode("bogus"),
		}},
	}
	m := mustMatcher(t, spec)

	// Command alone: no args to validate, so passes.
	_, err := m.Match(seg("x"))
	if err != nil {
		t.Errorf("bare command with unknown mode should pass: %v", err)
	}

	// Any arg rejected.
	_, err = m.Match(seg("x", "anything"))
	e := asError(t, err)
	if e.Code != ErrArgNotAllowed {
		t.Errorf("unknown mode should fail closed, got %s", e.Code)
	}
}

func TestMatch_SameTokenRepeatedInArgv(t *testing.T) {
	// If the same disallowed token appears multiple times, we get
	// multiple errors (one per occurrence).
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
	// If two disallow patterns both match a token, the matcher
	// reports the first one (load order). Verify.
	spec := &tool.ToolSpec{
		Commands: []tool.Command{{
			Command: "x",
			Mode:    tool.CommandModeBlacklist,
			DisallowedArgs: []tool.Argument{
				{Regex: "^--.*$"}, // first: anything starting with --
				{Arg: "--output"}, // second: exact
			},
		}},
	}
	m := mustMatcher(t, spec)

	_, err := m.Match(seg("x", "--output"))
	e := asError(t, err)
	if !strings.Contains(e.Pattern, "regex:") {
		t.Errorf("expected first (regex) pattern to match, got pattern %q",
			e.Pattern)
	}
}

func TestMatch_ErrorContainsCommandPath(t *testing.T) {
	m := mustMatcher(t, gitSpec())

	_, err := m.Match(seg("git", "--no-pager", "log", "--no-pager", "--textconv"))
	e := asError(t, err)
	if e.Command != "git log" {
		t.Errorf("command path = %q, want git log", e.Command)
	}
}

func TestMatch_ErrorsInterfaceUnwrap(t *testing.T) {
	// errors.Is on Errors should find matching error codes.
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
	// Empty-string tokens in argv. Lexer produces these for "".
	// In blacklist mode, empty string matches nothing in deny list
	// (assuming none of them are ""), so it passes.
	m := mustMatcher(t, simpleBlacklist("x"))
	_, err := m.Match(seg("x", "", "something"))
	if err != nil {
		t.Errorf("empty token in blacklist should pass: %v", err)
	}

	// In whitelist mode, empty string doesn't match typical allow
	// entries, so it's rejected.
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
