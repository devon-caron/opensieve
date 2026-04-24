// Demo: load read_tool.yaml and run a handful of commands through the
// matcher to show what the policy accepts, what it rejects, and what
// the rejection messages look like.
//
// Run from the repo root:
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

	cases := []struct {
		label string
		cmd   string
	}{
		{"happy: --no-pager fronts every git invocation (orient)",
			"git --no-pager ls-files"},

		{"happy: --no-pager + log + flags",
			"git --no-pager log --oneline"},

		{"happy: pipeline through grep",
			"ls -la | grep README"},

		{"reject: git without --no-pager (would stall the agent on a pager)",
			"git log --oneline"},

		{"reject: inherited disallow (--textconv RCE vector)",
			"git --no-pager log --textconv"},

		{"reject: write subcommand not in the whitelisted sub tree",
			"git --no-pager push origin main"},

		{"reject: grep -f reads arbitrary file (path-policy bypass)",
			"ls -la | grep -f patterns.txt"},

		{"reject: forbidden shell metachar (redirection)",
			"cat README.md > out.txt"},
	}

	for i, tc := range cases {
		fmt.Printf("──────── case %d: %s\n", i+1, tc.label)
		fmt.Printf("$ %s\n", tc.cmd)
		r := p.Parse(specPath, tc.cmd)
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

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "demo: "+format+"\n", args...)
	os.Exit(1)
}
