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
	cmd   string
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
					"ls"},
				{"orient: list tracked files",
					"git --no-pager ls-files"},
				{"oneline log with extra flags",
					"git --no-pager log --oneline -20"},
				{"blame a file",
					"git --no-pager blame README.md"},
				{"quoted arg passes through as a single TokWord",
					`git --no-pager grep "TODO"`},
				{"rg with multiple flags",
					`rg "TODO" -A 5 --type go`},
				{"two-stage pipeline (cat | head)",
					"cat README.md | head -20"},
				{"three-stage pipeline (ls | grep | wc)",
					"git --no-pager ls-files | grep README | wc -l"},
				{"find with allowed predicates",
					`find . -name "*.go" -type f`},
				{"safe disk usage check",
					"du -sh ."},
			},
		},
		{
			title: "Lexer-stage rejections (never reach the matcher)",
			cases: []tcase{
				{"empty input",
					""},
				{"whitespace only",
					"   "},
				{"forbidden redirect operator",
					"cat README.md > out.txt"},
				{"forbidden && operator",
					"ls && rm -rf /"},
				{"forbidden ; sequencing",
					"ls; rm -rf /"},
				{"forbidden $ (variable expansion)",
					"echo $HOME"},
				{"forbidden backtick (command substitution)",
					"echo `whoami`"},
				{"forbidden glob expansion",
					"ls *.go"},
				{"forbidden brace expansion",
					"ls {a,b}.go"},
				{"unterminated quote",
					`grep "unterminated`},
			},
		},
		{
			title: "Routing-stage rejections (strict subcommand match required)",
			cases: []tcase{
				{"unknown top-level command",
					"rm -rf /"},
				{"unknown command (typo)",
					"cargo build"},
				{"path-form command attempt",
					"/bin/ls -la"},
				{"unknown sub at depth 1 — git's only sub is --no-pager",
					"git log --oneline"},
				{"unknown sub at depth 2 — push isn't read-only",
					"git --no-pager push origin main"},
				{"typo'd sub at depth 2",
					"git --no-pager lgo"},
				{"would-be flag in subcommand position (no fall-through)",
					"git --no-pager --version"},
			},
		},
		{
			title: "Validation-stage rejections at the leaf",
			cases: []tcase{
				{"tail -f would block the agent loop",
					"tail -f /var/log/syslog"},
				{"tail --follow= regex denial",
					"tail --follow=name /var/log/syslog"},
				{"find -exec is RCE",
					"find . -exec rm {}"},
				{"find -delete is destructive",
					"find . -name old -delete"},
				{"git grep -O runs a command per match (RCE)",
					"git --no-pager grep -O echo TODO"},
				{"blame --contents reads an arbitrary file",
					"git --no-pager blame --contents=/etc/passwd README.md"},
				{"rg --pre=<cmd> is RCE",
					"rg --pre=cat TODO"},
				{"sort -o writes an arbitrary file",
					"sort -o /tmp/leak data.txt"},
				{"date -s sets the system clock",
					"date -s 1990-01-01"},
				{"diff --to-file= can bypass path policy",
					"diff a.txt --to-file=/etc/shadow"},
				{"file -C compiles magic files",
					"file -C magic.mgc"},
				{"wc --files0-from reads an arbitrary file list",
					"wc --files0-from=/tmp/list"},
				{"ls-files --exclude-from reads an arbitrary file",
					"git --no-pager ls-files --exclude-from=/tmp/x"},
			},
		},
		{
			title: "Recursive disallow inheritance (declared once at git, reaches every leaf)",
			cases: []tcase{
				{"--textconv at log",
					"git --no-pager log --textconv"},
				{"--textconv at show (same denial, different leaf)",
					"git --no-pager show --textconv HEAD"},
				{"--ext-diff at status",
					"git --no-pager status --ext-diff"},
				{"--output=… regex at log",
					"git --no-pager log --output=/tmp/leak"},
				{`-- end-of-options marker is denied (prevents flag injection past it)`,
					"git --no-pager log -- --textconv"},
			},
		},
		{
			title: "Multi-error collection at the leaf (single round-trip diagnosis)",
			cases: []tcase{
				{"three denied flags in one find call",
					"find . -exec rm -delete -fprint /tmp/x"},
				{"own + inherited denial in the same segment",
					"git --no-pager blame --contents=/etc/passwd --textconv README.md"},
				{"same denied flag repeated three times",
					"tail -f -f -f /var/log/syslog"},
			},
		},
		{
			title: "Pipeline-stage behavior",
			cases: []tcase{
				{"pass-through: every stage validates",
					"cat README.md | grep TODO | wc -l"},
				{"first segment passes, second denied",
					"ls -la | grep -f patterns.txt"},
				{"long pipeline, middle segment denied",
					`git --no-pager ls-files | grep --include-from=ignore.txt | wc -l`},
				{"final segment denied",
					"cat data.txt | sort -o /tmp/leak"},
				{"empty segment from a leading pipe",
					"| ls -la"},
				{"empty segment from a trailing pipe",
					"ls -la |"},
				{"empty segment from || (consecutive pipes)",
					"ls -la || grep foo"},
			},
		},
		{
			title: "Boundary cases",
			cases: []tcase{
				{"single-character unknown command",
					"a"},
				{"top-level with subs but no args (bare git)",
					"git"},
				{"top-level + only the routing token (no leaf descent)",
					"git --no-pager"},
				{"identical command repeated through a pipeline",
					"cat a.txt | cat | cat"},
				{"long arg list, all OK",
					"find . -type f -name a -name b -name c -name d -not -empty"},
			},
		},
		{
			title:    "Space-form hint (uses an auxiliary hint-demo spec)",
			specPath: hintPath,
			cases: []tcase{
				{"blacklist — hint fires: =-only regex, bare --file isn't denied",
					"rg --file=patterns.txt"},
				{"blacklist — hint suppressed: (=.*)? regex covers both forms",
					"diff --to-file=/etc/shadow"},
				{"blacklist — following the hint: space-separated form passes",
					"rg --file patterns.txt"},
				{"whitelist — hint fires: bare --name is allowed, --name=alice isn't",
					"probe --name=alice"},
				{"whitelist — same shape, different flag",
					"probe --count=10"},
				{"whitelist — following the hint still fails here because values must also be in the allow list",
					"probe --name alice"},
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
			fmt.Printf("$ %s\n", tc.cmd)
			r := p.Parse(path, tc.cmd)
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
