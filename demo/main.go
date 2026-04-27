// Demo: load read_tool.yaml and run a curated set of commands through
// the matcher to show what the policy accepts, what it rejects, and
// what the rejection messages look like.
//
// Cases are grouped by failure mode so the output reads as a tour of
// the matcher's behavior. Run from the repo root:
//
//	go run ./demo
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	parser "github.com/devon-caron/opensieve"
)

type tcase struct {
	label string
	base  string
	argv  []string
}

type section struct {
	title string
	// specPath overrides the default spec for this section. When
	// empty, cases match against read_tool.yaml.
	specPath string
	cases    []tcase
}

// hintSpec is a deliberately tiny auxiliary policy used only by the
// "Space-form hint" section. It showcases policies where the hint
// fires and where it's suppressed — neither of which is exercised by
// read_tool.yaml (every `=` regex there pairs with a bare-form rule).
const hintSpec = `name: HintDemo
description: Demonstrates the matcher's space-form hint for flag=value denials.
commands:
  # Blacklist: only the =-form is denied. Bare --file would pass →
  # the hint fires, telling the agent to try space-separated form.
  - command: rg
    mode: blacklist
    disallowed_args:
      - regex: "^--file=.*$"

  # Blacklist: the (=.*)? wrapper denies both bare and =-form. The
  # hint is suppressed because space-separating wouldn't help.
  - command: diff
    mode: blacklist
    disallowed_args:
      - regex: "^--to-file(=.*)?$"

  # Whitelist: bare --name / --count ARE in the allow list, but the
  # agent wrote the =-form so the token doesn't match any rule as-is.
  # The hint fires symmetrically in whitelist mode.
  - command: probe
    mode: whitelist
    allowed_args:
      - arg: "--name"
      - arg: "--count"
`

func main() {
	specPath, err := filepath.Abs("read_tool.yaml")
	if err != nil {
		die("resolve spec path: %v", err)
	}
	if _, err := os.Stat(specPath); err != nil {
		die("read_tool.yaml not found at %s — run from the repo root", specPath)
	}

	p, err := parser.New()
	if err != nil {
		die("parser.New: %v", err)
	}

	hintPath := writeTempSpec(hintSpec)
	defer os.Remove(hintPath)

	sections := []section{
		{
			title: "Happy paths",
			cases: []tcase{
				{"bare top-level command (no subs to route through)",
					"ls", nil},
				{"orient: list tracked files",
					"git", []string{"--no-pager", "ls-files"}},
				{"oneline log with extra flags",
					"git", []string{"--no-pager", "log", "--oneline", "-20"}},
				{"blame a file",
					"git", []string{"--no-pager", "blame", "README.md"}},
				{"argv element passes through as a single TokWord",
					"git", []string{"--no-pager", "grep", "TODO"}},
				{"rg with multiple flags",
					"rg", []string{"TODO", "-A", "5", "--type", "go"}},
				{"two-stage pipeline (cat | head)",
					"cat", []string{"README.md", "|", "head", "-20"}},
				{"three-stage pipeline (ls | grep | wc)",
					"git", []string{"--no-pager", "ls-files", "|", "grep", "README", "|", "wc", "-l"}},
				{"find with allowed predicates (glob arg auto-quoted)",
					"find", []string{".", "-name", "*.go", "-type", "f"}},
				{"safe disk usage check",
					"du", []string{"-sh", "."}},
			},
		},
		{
			// Note: under the argv API, elements that contain
			// shell metacharacters are auto-quoted by joinArgv so
			// they reach the matcher as literal tokens. Some of
			// these cases therefore now reject at the matcher
			// stage rather than the lexer stage; the rejection
			// itself is preserved.
			title: "Metacharacter inputs (lex- or matcher-stage rejection)",
			cases: []tcase{
				{"empty input",
					"", nil},
				{"whitespace-only base",
					"   ", nil},
				{"redirect operator as argv element",
					"cat", []string{"README.md", ">", "out.txt"}},
				{"&& as argv element",
					"ls", []string{"&&", "rm", "-rf", "/"}},
				{"; as argv element",
					"ls", []string{";", "rm", "-rf", "/"}},
				{"$HOME as argv element",
					"echo", []string{"$HOME"}},
				{"backtick command sub as argv element",
					"echo", []string{"`whoami`"}},
				{"glob as argv element",
					"ls", []string{"*.go"}},
				{"brace expansion as argv element",
					"ls", []string{"{a,b}.go"}},
				{"argv element with stray double quote (lexer ErrUnterminatedQuote)",
					"grep", []string{`"unterminated`}},
			},
		},
		{
			title: "Routing-stage rejections (strict subcommand match required)",
			cases: []tcase{
				{"unknown top-level command",
					"rm", []string{"-rf", "/"}},
				{"unknown command (typo)",
					"cargo", []string{"build"}},
				{"path-form command attempt",
					"/bin/ls", []string{"-la"}},
				{"unknown sub at depth 1 — git's only sub is --no-pager",
					"git", []string{"log", "--oneline"}},
				{"unknown sub at depth 2 — push isn't read-only",
					"git", []string{"--no-pager", "push", "origin", "main"}},
				{"typo'd sub at depth 2",
					"git", []string{"--no-pager", "lgo"}},
				{"would-be flag in subcommand position (no fall-through)",
					"git", []string{"--no-pager", "--version"}},
			},
		},
		{
			title: "Validation-stage rejections at the leaf",
			cases: []tcase{
				{"tail -f would block the agent loop",
					"tail", []string{"-f", "/var/log/syslog"}},
				{"tail --follow= regex denial",
					"tail", []string{"--follow=name", "/var/log/syslog"}},
				{"find -exec is RCE",
					"find", []string{".", "-exec", "rm", "{}"}},
				{"find -delete is destructive",
					"find", []string{".", "-name", "old", "-delete"}},
				{"git grep -O runs a command per match (RCE)",
					"git", []string{"--no-pager", "grep", "-O", "echo", "TODO"}},
				{"blame --contents reads an arbitrary file",
					"git", []string{"--no-pager", "blame", "--contents=/etc/passwd", "README.md"}},
				{"rg --pre=<cmd> is RCE",
					"rg", []string{"--pre=cat", "TODO"}},
				{"sort -o writes an arbitrary file",
					"sort", []string{"-o", "/tmp/leak", "data.txt"}},
				{"date -s sets the system clock",
					"date", []string{"-s", "1990-01-01"}},
				{"diff --to-file= can bypass path policy",
					"diff", []string{"a.txt", "--to-file=/etc/shadow"}},
				{"file -C compiles magic files",
					"file", []string{"-C", "magic.mgc"}},
				{"wc --files0-from reads an arbitrary file list",
					"wc", []string{"--files0-from=/tmp/list"}},
				{"ls-files --exclude-from reads an arbitrary file",
					"git", []string{"--no-pager", "ls-files", "--exclude-from=/tmp/x"}},
			},
		},
		{
			title: "Recursive disallow inheritance (declared once at git, reaches every leaf)",
			cases: []tcase{
				{"--textconv at log",
					"git", []string{"--no-pager", "log", "--textconv"}},
				{"--textconv at show (same denial, different leaf)",
					"git", []string{"--no-pager", "show", "--textconv", "HEAD"}},
				{"--ext-diff at status",
					"git", []string{"--no-pager", "status", "--ext-diff"}},
				{"--output=… regex at log",
					"git", []string{"--no-pager", "log", "--output=/tmp/leak"}},
				{`-- end-of-options marker is denied (prevents flag injection past it)`,
					"git", []string{"--no-pager", "log", "--", "--textconv"}},
			},
		},
		{
			title: "Multi-error collection at the leaf (single round-trip diagnosis)",
			cases: []tcase{
				{"three denied flags in one find call",
					"find", []string{".", "-exec", "rm", "-delete", "-fprint", "/tmp/x"}},
				{"own + inherited denial in the same segment",
					"git", []string{"--no-pager", "blame", "--contents=/etc/passwd", "--textconv", "README.md"}},
				{"same denied flag repeated three times",
					"tail", []string{"-f", "-f", "-f", "/var/log/syslog"}},
			},
		},
		{
			title: "Pipeline-stage behavior",
			cases: []tcase{
				{"pass-through: every stage validates",
					"cat", []string{"README.md", "|", "grep", "TODO", "|", "wc", "-l"}},
				{"first segment passes, second denied",
					"ls", []string{"-la", "|", "grep", "-f", "patterns.txt"}},
				{"long pipeline, middle segment denied",
					"git", []string{"--no-pager", "ls-files", "|", "grep", "--include-from=ignore.txt", "|", "wc", "-l"}},
				{"final segment denied",
					"cat", []string{"data.txt", "|", "sort", "-o", "/tmp/leak"}},
				{"empty segment from a leading pipe",
					"|", []string{"ls", "-la"}},
				{"empty segment from a trailing pipe",
					"ls", []string{"-la", "|"}},
				{"empty segment from consecutive pipes",
					"ls", []string{"-la", "|", "|", "grep", "foo"}},
			},
		},
		{
			title: "Boundary cases",
			cases: []tcase{
				{"single-character unknown command",
					"a", nil},
				{"top-level with subs but no args (bare git)",
					"git", nil},
				{"top-level + only the routing token (no leaf descent)",
					"git", []string{"--no-pager"}},
				{"identical command repeated through a pipeline",
					"cat", []string{"a.txt", "|", "cat", "|", "cat"}},
				{"long arg list, all OK",
					"find", []string{".", "-type", "f", "-name", "a", "-name", "b", "-name", "c", "-name", "d", "-not", "-empty"}},
			},
		},
		{
			title:    "Space-form hint (uses an auxiliary hint-demo spec)",
			specPath: hintPath,
			cases: []tcase{
				{"blacklist — hint fires: =-only regex, bare --file isn't denied",
					"rg", []string{"--file=patterns.txt"}},
				{"blacklist — hint suppressed: (=.*)? regex covers both forms",
					"diff", []string{"--to-file=/etc/shadow"}},
				{"blacklist — following the hint: space-separated form passes",
					"rg", []string{"--file", "patterns.txt"}},
				{"whitelist — hint fires: bare --name is allowed, --name=alice isn't",
					"probe", []string{"--name=alice"}},
				{"whitelist — same shape, different flag",
					"probe", []string{"--count=10"}},
				{"whitelist — following the hint still fails here because values must also be in the allow list",
					"probe", []string{"--name", "alice"}},
			},
		},
	}

	caseN := 0
	for _, sec := range sections {
		path := specPath
		if sec.specPath != "" {
			path = sec.specPath
		}
		fmt.Printf("\n══════════════════════════════════════════════════════\n")
		fmt.Printf("  %s\n", sec.title)
		fmt.Printf("══════════════════════════════════════════════════════\n\n")
		for _, tc := range sec.cases {
			caseN++
			fmt.Printf("──────── case %d: %s\n", caseN, tc.label)
			fmt.Printf("$ %s\n", display(tc.base, tc.argv))
			r := p.Parse(path, tc.base, tc.argv)
			if r.Pass {
				fmt.Printf("  → PASS  (rule: %s)\n\n", r.Rule)
				continue
			}
			fmt.Printf("  → REJECT (rule: %s)\n", r.Rule)
			for line := range strings.SplitSeq(r.Reason.Error(), "\n") {
				fmt.Println("  " + line)
			}
			fmt.Println()
		}
	}
}

// display renders a base + argv invocation as a shell-like string for
// the "$ ..." line in the demo output. It is purely for human reading;
// the parser receives base and argv directly, not this string.
func display(base string, argv []string) string {
	if len(argv) == 0 {
		return base
	}
	return base + " " + strings.Join(argv, " ")
}

// writeTempSpec writes content to a temp YAML file and returns the
// path. The caller is responsible for os.Remove when finished.
func writeTempSpec(content string) string {
	f, err := os.CreateTemp("", "opensieve-demo-*.yaml")
	if err != nil {
		die("create temp spec: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		die("write temp spec: %v", err)
	}
	if err := f.Close(); err != nil {
		die("close temp spec: %v", err)
	}
	return f.Name()
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "demo: "+format+"\n", args...)
	os.Exit(1)
}
