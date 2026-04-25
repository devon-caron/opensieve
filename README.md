# OpenSieve

A YAML-defined safety harness for AI agent shell calls.

OpenSieve takes a tokenized command string and decides whether it's allowed
to run, governed by a YAML policy that an operator writes once and reuses
across agent runtimes. It's aimed at two audiences: **policy authors** who
write the YAML, and **library integrators** who load that YAML from their
own program. This README serves as a switchboard for both.

> **Status:** experimental alpha. The Go API and YAML schema are stable enough to
> use in projects but may shift before a 1.0 tag. Pin the version you
> integrate against.

## The 'Why'

AI agents increasingly want to run real shell commands — `git log`, `rg
"TODO"`, `find . -name '*.go'` — to investigate codebases, inspect state,
and report back. Letting them do so without checks is unsafe; banning the
shell entirely throws away the most expressive tool they have.

The two common workarounds both fall short:

- **Per-framework allowlists** (Claude Code permissions, OpenAI function
  schemas, LangChain tool definitions) live inside one runtime and don't
  port to another. The same policy gets re-implemented in every agent you
  build.
- **OS-level isolation** (containers, sandbox-exec, AppArmor) enforces but
  doesn't *explain*. When a sandbox blocks a command, the agent sees an
  exit code and a guessing game; it doesn't know which specific flag was
  the problem or what to try instead.

OpenSieve is declarative pre-execution validation. You write a YAML policy
that describes which commands an agent may invoke and which arguments are
allowed or denied. The library tokenizes the agent's command, walks it
against the policy, and either passes it through or returns a multi-line
diagnostic that names the offending token, cites the YAML rule that fired,
and suggests a fix.

It is not a sandbox. Pair it with subprocess isolation for defense in
depth — see [Threat model](#threat-model).

## Quick start

```bash
go get github.com/devon-caron/opensieve
```

Save this as `policy.yaml`:

```yaml
name: ReadOnlyShell
description: A minimal read-only command set for an AI agent.
commands:
  # ls is permissive — agents pass any flags they like.
  - command: ls
    mode: blacklist
    disallowed_args: []

  # grep denies the few flags that read patterns from arbitrary files
  # (a path-policy bypass), but everything else is fair game.
  - command: grep
    mode: blacklist
    disallowed_args:
      - regex: "^-f.*$"
      - regex: "^--file(=.*)?$"
```

And run:

```go
package main

import (
	"fmt"

	parser "github.com/devon-caron/opensieve"
)

func main() {
	p, _ := parser.New()

	r := p.Parse("policy.yaml", "ls -la | grep TODO")
	fmt.Println(r.Pass) // true

	r = p.Parse("policy.yaml", "ls -la | grep -f patterns.txt")
	fmt.Println(r.Reason)
}
```

The rejected case prints something like:

```
match: arg_denied for command "grep"
  token:   "-f" at byte 14
  rule:    grep.disallowed_args[0] — regex "^-f.*$"
  reason:  "-f" is denied by the policy.
  fix:     remove "-f".
```

That's the shape of every rejection: a code, the offending token with its
byte position, the YAML rule that fired, a one-line reason, and a fix.
Designed to be readable by both humans and AI agents.

## Concepts

A handful of ideas account for nearly all of OpenSieve's behavior.

### Tool spec & commands

A `ToolSpec` is one named YAML policy — typically one file per tool an
agent has access to (e.g. `read_tool.yaml`, `build_tool.yaml`). It carries
a `name`, a `description` (the LLM-facing blurb explaining what the tool
does), an optional `env` block, and a list of `commands`.

A command is one top-level executable name — `ls`, `git`, `find` — plus
the rules that govern its arguments and (optionally) its subcommands.

### Modes: whitelist vs blacklist

Each command picks a mode:

- **`whitelist`** — only arguments listed in `allowed_args` pass. Anything
  else is rejected. Use this when you can fully enumerate the safe surface.
- **`blacklist`** — anything passes *except* arguments listed in
  `disallowed_args`. Use this when you only need to deny a known-bad
  surface and don't want to maintain an exhaustive allow list.

Mode is consulted only at the **leaf** entry — the deepest command the
matcher routed to. Intermediate levels are governed by routing, not
validation (see below).

### Subcommands & strict routing

A command may declare child commands via a `subcommands` block. When the
matcher reaches an entry that has children, the **next token must name a
known child** — there is no per-level pass-through for parent flags. This
is a deliberate design choice: it makes every accepted invocation one
whose full prefix path was explicitly listed in the policy.

The tradeoff is that allowing extra parent flags requires modeling them
as subcommands. The canonical example is in
[`read_tool.yaml`](./read_tool.yaml), where `--no-pager` is declared as
git's only direct subcommand:

```
git
└── --no-pager
    ├── log
    ├── show
    ├── status
    ├── blame
    └── ...
```

The agent must write `git --no-pager log …`; `git log …` is rejected at
the `log` token because `log` isn't a direct child of `git`.

This trick guarantees `--no-pager` is always present without any
required-arg machinery and without inventing a separate "parent flag"
concept. Stack more pseudo-subcommands to require more flags.

### Recursive `disallowed_sub_args` inheritance

A `subcommands` block may carry a `disallowed_sub_args` list. Those rules
are **recursively inherited** into every descendant leaf's effective
denylist. Declaring `--textconv` on git's `subcommands` automatically
denies it on `git --no-pager log`, `git --no-pager show`, `git --no-pager
grep`, and every other leaf reachable through git.

This is the only thing that flows downward. Allow lists, required lists,
and own disallow lists all stay attached to the entry they're declared on.

### Leaf-only validation

`allowed_args` / `disallowed_args` / `required_args` are evaluated only
when the entry is reached as the terminal command. At intermediate routing
levels they are dead config — write them and the matcher will not consult
them. This is a consequence of the strict routing model: intermediate
tokens can only ever be subcommand names, so per-level allow/deny lists
have nothing to test.

### Pipelines

Input may contain pipes. The parser splits at `|` into one segment per
pipeline stage, drops the trailing EOF, and matches each segment in order
against the policy. The first failing segment short-circuits the rest.

Empty segments (a leading or trailing pipe, or `||`) are rejected with
`empty_segment`.

More bash utilities (;, if statements, etc) will be supported as development goes on. 

## YAML reference

The Go types in [`tool/tool.go`](./tool/tool.go) are the source of truth;
the snippets below mirror those structs.

### `ToolSpec`

```yaml
name: ReadCodebase                  # required — uniquely identifies the spec
description: |                      # required — LLM-facing blurb
  Read-only codebase analysis...
env:                                # optional — env vars the tool needs
  - "GIT_PAGER=cat"
  - "PAGER=cat"
commands:                           # required — top-level commands
  - command: ls
    mode: blacklist
    disallowed_args: []
  - command: grep
    mode: blacklist
    disallowed_args:
      - regex: "^-f.*$"
```

The `env` block is part of the schema today but not enforced by the
matcher. See [Roadmap](#roadmap).

### `Command`

```yaml
- command: tail                     # required — the executable name
  mode: blacklist                   # required — "whitelist" or "blacklist"
  allowed_args:                     # whitelist mode only (leaf-only)
    - arg: "--lines"
  disallowed_args:                  # blacklist mode (leaf-only)
    - arg: "-f"
    - regex: "^--follow=.*$"
  required_args:                    # leaf-only
    - arg: "--quiet"
  subcommands:                      # optional — see SubcommandsConfig below
    commands: [...]
```

When `subcommands` is set, the matcher routes through this command into
the named children; only the leaf entry's `*_args` lists are consulted
during validation.

### `SubcommandsConfig`

```yaml
subcommands:
  disallowed_sub_args:              # recursively inherited by every descendant
    - arg: "--ext-diff"
    - arg: "--textconv"
    - regex: "^--output(=.*)?$"
  commands:                         # the actual child commands
    - command: log
      mode: blacklist
      disallowed_args: []
    - command: blame
      mode: blacklist
      disallowed_args:
        - regex: "^--contents(=.*)?$"
```

`disallowed_sub_args` declared at any level reaches every leaf below it.
There is no `allowed_sub_args` or `required_sub_args` — by design. Each
leaf owns its own allow/required lists; only denials propagate.

### `Argument`

Every entry in `allowed_args`, `disallowed_args`, `required_args`, and
`disallowed_sub_args` is a single argument rule. **Exactly one** of the
three fields must be set:

```yaml
- arg: "--textconv"                 # exact string match
- regex: "^--output(=.*)?$"         # Go regexp; anchor with ^ and $
- path_spec: "/etc/*"               # doublestar glob
```

Setting zero or multiple fields is a load-time error. Patterns are
compiled at `BuildIndex` time so the matching hot path does no allocation
or compilation.

## Go API tour

The public surface is intentionally small.

```go
import parser "github.com/devon-caron/opensieve"

p, err := parser.New()
if err != nil { /* ... */ }

r := p.Parse("policy.yaml", "ls -la | grep TODO")
// r.Pass    bool   — true on success
// r.Reason  error  — non-nil on failure (see "Errors for AI agents")
// r.Rule    string — the spec's `name` field
```

`Parser` caches compiled matchers per `toolPath`. Repeat `Parse` calls for
the same file reuse the built `Index` instead of re-reading the YAML or
re-compiling regexes.

To switch on the failure mode without string matching:

```go
import (
	"errors"
	"github.com/devon-caron/opensieve/match"
)

if !r.Pass {
	var single *match.Error
	var many match.Errors
	switch {
	case errors.As(r.Reason, &single):
		// One violation — common case. single.Code, single.Token, single.Pos, etc.
	case errors.As(r.Reason, &many):
		// Multiple violations from the same segment. Iterate `many`.
	}
}
```

A single-violation result is always returned as `*match.Error` (the
package never returns an `Errors` of length 1). A multi-violation result
is `match.Errors`.

For lower-level access, `match.New(idx)` and `match.FromSpec(spec)`
construct a `Matcher` directly, and `Matcher.Match(seg)` validates one
segment at a time.

## Errors for AI agents

Every `*match.Error` carries enough state to render a multi-line
diagnostic that an agent can act on without further context. Here's an
inherited-disallow case from the demo:

```
match: arg_denied for command "git --no-pager log"
  token:   "--textconv" at byte 19
  rule:    git.subcommands.disallowed_sub_args[1] (inherited) — exact "--textconv"
  reason:  "--textconv" is denied by the policy.
  fix:     remove "--textconv".
```

And a case where the agent used `flag=value` for a denial that only
applies to the `=` form:

```
match: arg_denied for command "rg"
  token:   "--file=patterns.txt" at byte 3
  rule:    rg.disallowed_args[0] — regex "^--file=.*$"
  reason:  "--file=patterns.txt" is denied by the policy.
  hint:    "--file" does not accept the key=value form here; pass the value separated by a space instead (e.g., "--file" "patterns.txt").
  fix:     remove "--file=patterns.txt".
```

The fields, defined in [`match/errors.go`](./match/errors.go):

| Field     | Meaning                                                                                          |
|-----------|--------------------------------------------------------------------------------------------------|
| `Code`    | One of `empty_segment`, `command_not_allowed`, `arg_not_allowed`, `arg_denied`, `missing_required` |
| `Command` | Dotted command path that governed when the failure was detected (e.g., `"git --no-pager log"`)   |
| `Token`   | The offending argv token's value                                                                 |
| `Pos`     | Byte offset of `Token` in the original input string                                              |
| `Pattern` | Human-readable description of the rule that fired (e.g., `regex "^--output(=.*)?$"`)             |
| `Source`  | YAML provenance string (e.g., `git.subcommands.disallowed_sub_args[1] (inherited)`)              |
| `Allowed` | For `command_not_allowed` and whitelist-leaf `arg_not_allowed`: what *would* have matched        |
| `Subs`    | For routing-stage `arg_not_allowed`: the entry's known subcommand names                          |
| `Reason`  | Either an auto-generated explanation or a policy-author override                                 |
| `Hint`    | Optional extra tip — currently fires for `flag=value` denials when spacing would help            |

The two `arg_not_allowed` shapes — routing-stage (with `Subs` populated)
versus validation-stage (with `Allowed` populated) — are distinguishable
by which list is non-empty. Inspect both fields to disambiguate.

`match.Errors` aggregates multiple violations from one segment. The
matcher collects every issue at the leaf in one pass so the agent can fix
them all in a single round-trip.

## A real-world example

[`read_tool.yaml`](./read_tool.yaml) is a fully-worked policy modeling a
read-only codebase analysis tool: `ls`, `cat`, `head`, `tail`, `wc`,
`file`, `stat`, `grep`, `rg`, `find`, `diff`, `sort`, `uniq`, `cut`, `tr`,
`ps`, `df`, `du`, `uname`, `whoami`, `date`, and `git` (read-only
subcommands only).

The git block illustrates both design tricks at once. Stripped down:

```yaml
- command: git
  mode: whitelist
  subcommands:
    # Recursively inherited into every leaf below — git, --no-pager, log,
    # show, blame, etc. all see this denylist as part of their own.
    disallowed_sub_args:
      - arg: "--ext-diff"
      - arg: "--textconv"
      - regex: "^--output(=.*)?$"
      - arg: "--"                   # prevents flag injection past --
    commands:
      # --no-pager modeled as the only sub of git: every git invocation
      # MUST route through it. The pseudo-sub trick.
      - command: "--no-pager"
        mode: whitelist
        subcommands:
          commands:
            - command: log
              mode: blacklist
              disallowed_args: []   # nothing of its own; inherits the lot
            - command: blame
              mode: blacklist
              disallowed_args:
                - regex: "^--contents(=.*)?$"
            # ...show, status, ls-files, rev-parse, grep
```

A few invocations and the policy's verdict:

| Command                                       | Result                                              |
|-----------------------------------------------|-----------------------------------------------------|
| `git --no-pager ls-files`                     | pass                                                |
| `git --no-pager log --oneline`                | pass                                                |
| `git log --oneline`                           | reject — `log` isn't a direct child of `git`        |
| `git --no-pager log --textconv`               | reject — `--textconv` denied via inheritance        |
| `git --no-pager push origin`                  | reject — `push` isn't a child of `--no-pager`       |
| `git --no-pager blame --contents=/etc/passwd` | reject — own denial on `blame.disallowed_args[0]`   |

Read the full file for the rest of the commands and their denials. Each
deny has a comment explaining *why* it's there.

## Try the demo

```bash
go run ./demo
```

Runs 66 cases against `read_tool.yaml` plus an inline auxiliary spec,
grouped by failure mode:

1. **Happy paths** — bare commands, multi-stage pipelines, quoted args.
2. **Lexer-stage rejections** — empty input, redirect (`>`), `&&`, `;`,
   `$`, backticks, glob (`*`), brace expansion, unterminated quotes.
3. **Routing-stage rejections** — unknown top-level commands, unknown
   subs at every depth, path-form invocations.
4. **Validation-stage rejections** — every per-command denial in the
   policy: `tail -f`, `find -exec`, `git grep -O`, `blame --contents`,
   `rg --pre=`, `sort -o`, `date -s`, `diff --to-file=`, `file -C`,
   `wc --files0-from=`, etc.
5. **Recursive disallow inheritance** — `--textconv` reaches log, show,
   and status; `--ext-diff` and `--output=` likewise.
6. **Multi-error collection** — three denied flags in one find call;
   own + inherited denial in the same segment.
7. **Pipeline behavior** — pass-through, mid-segment denial, leading /
   trailing / `||` empty segments.
8. **Boundary cases** — single-char unknown command, bare `git`,
   `git --no-pager` alone, repeated commands in a pipeline.
9. **Space-form hint** — uses an auxiliary spec (written to a temp file
   on demo startup) to show when the `flag=value` hint fires and when
   it's suppressed.

Each rejection prints the matcher's full descriptive output so you can
see exactly what an agent would receive.

## Testing

```bash
go test ./...                                 # full suite
go test -fuzz=FuzzMatch -fuzztime=1m ./match/ # fuzz the matcher
```

The fuzz target ([`match/fuzz_test.go`](./match/fuzz_test.go)) seeds with
accepted, rejected, and pathological inputs and asserts the matcher's
contract on every step:

- `(Result, error)` are mutually exclusive — exactly one is non-nil.
- Errors are `*match.Error` or `match.Errors` only, never bare strings.
- A returned `Errors` always has length ≥ 2; single violations get
  promoted to `*Error`.
- Every cited byte position is within input bounds.
- Every error code is one of the defined `ErrorCode` constants.
- On success, `Path` is non-empty, `Entry` is non-nil, `Path[0] ==
  Argv[0]`.
- Match is deterministic — same input twice yields the same shape.

Preserve these invariants when changing the matcher.

## Threat model

OpenSieve is a **policy validator**, not a sandbox. It decides whether a
command string passes a YAML policy *before* the command runs. It does
nothing to constrain the command once it's running. Be honest with
yourself about the seams.

### In scope

- **RCE-shaped flag denials.** `find -exec`, `git grep -O`, `rg --pre=`,
  `git log --textconv` — flags whose presence lets the agent execute
  arbitrary programs through an otherwise-safe command.
- **Path-policy bypass flags.** `grep -f file`, `git blame --contents=`,
  `diff --to-file=`, `wc --files0-from=` — flags that make a tool read
  from or write to an arbitrary path that isn't named on the command line.
- **Pager and interactive blockers.** `tail -f`, `tail --follow=`, git's
  default pager, `--retry`. These hang the agent loop.
- **Shell metacharacter rejection at the lexer.** `>`, `<`, `&`, `;`,
  `$`, backticks, glob (`*`, `?`, `[`, `]`), brace expansion, tilde,
  comment markers, control characters. The lexer rejects every input
  containing them; redirection, sequencing, substitution, and expansion
  are out of the grammar entirely.
- **Structured, agent-actionable errors.** Every rejection cites the
  failing token's byte position, the YAML rule that fired, and a fix.

### Out of scope

- **Kernel-level isolation.** No syscall filtering, no namespaces, no
  uid drop. If a command passes the policy, it runs as whoever invoked
  it.
- **Network policy.** OpenSieve says nothing about whether `curl` or
  `git fetch` should reach the network.
- **Filesystem access enforcement.** A passing command can read or
  write any path the OS user can. The `path_spec` argument type
  validates *paths the agent typed*, not the cwd or downstream FS
  effects.
- **Resource limits.** No CPU, memory, file-descriptor, or wall-clock
  caps.
- **Race-freeness against TOCTOU.** Validation happens before exec; if
  the filesystem changes between the two, OpenSieve neither sees nor
  cares.

### How to compose it

Validating before execution gives rich, agent-actionable errors;
isolation gives enforcement. Compose them: run OpenSieve in your agent's
hot path to surface remediation hints, and run the resulting command
under whatever subprocess isolation you trust (uid drop, namespace,
container, sandbox-exec, gVisor, Firecracker — whichever fits your
threat model). The two layers are designed to coexist.

## Comparison with alternatives

OpenSieve sits between in-process allowlists and OS-level isolation. The
honest comparison:

### Agent framework allowlists

Things like Claude Code permissions, OpenAI function tool schemas, and
LangChain tool definitions all let you constrain what an agent runtime
will dispatch.

- **They are:** baked into one runtime, expressive within that runtime,
  and conveniently tied to the framework's tool-calling mechanism.
- **They aren't:** portable — switch frameworks and the same policy has
  to be re-encoded. They also don't typically expose a structured
  rejection error suitable for the agent itself to consume and fix.

OpenSieve is a YAML file plus a small library. The same policy compiles
in any host language you build for, and rejections come back in a format
designed to be read by the agent that produced the bad command.

### Container / chroot isolation

Docker, Firecracker, gVisor, sandbox-exec, bubblewrap.

- **They are:** real OS-level enforcement. If the policy says "no
  network," the kernel enforces it. They protect against bugs in
  OpenSieve, in your validator, and in the command itself.
- **They aren't:** lightweight, fast, or talkative. A blocked command
  surfaces as an exit code or a write failure; the agent doesn't learn
  *which flag* was the problem or what to try instead.

These compose cleanly with OpenSieve. Validate first to give the agent
actionable feedback; isolate second so a passing command can't escape
its budget. Don't pick one; use both.

### Generic regex / string-match command filters

`sudoers Cmnd_Alias`, custom shell wrappers, `grep -E` over the agent's
argv, ad-hoc denylists in a script.

- **They are:** trivial to start.
- **They aren't:** structured, validated, or self-documenting. They
  drift; they grow special cases; they don't tell you which rule fired
  or where it lives.

OpenSieve replaces these with a YAML file whose schema is enforced at
load time, whose rules each carry a provenance string back to their
source line, and whose errors are designed for an AI agent to remediate
in one shot.

## Roadmap

The current implementation is the matcher (Go). Planned:

- **Other-language reference implementations.** The "universal spec"
  ambition is genuine; Go is just first. The YAML schema + the matcher
  semantics are the portable contract.
- **Env scrubbing.** The `env:` block exists in the schema and in
  `read_tool.yaml`, but the library doesn't apply it yet. The intended
  shape is a helper that returns a sanitized `[]string` for `os/exec`.
- **Path-safe checker.** A separate package for validating that a
  passed path stays within an allowed prefix (no `..` escape, no
  symlink traversal). Mentioned in `layout.md` as `/pathsafe`.
- **Argument-grouping rules.** Currently the matcher checks each
  argument in isolation. Some flags only matter in combination
  (e.g., `--allow-X` is only meaningful when `--enable-Y` is also set).
- **Optional `Reason` text from YAML comments.** The `Error.Reason`
  field already supports a custom override; surfacing it from `# why`
  comments adjacent to YAML rules would let policy authors explain
  *why* a denial exists without writing Go.

If you have an interest in any of these, file an issue describing your
use case before implementing — the design is still flexible and your
constraints help shape it.

## License

Apache License 2.0. See [`LICENSE`](./LICENSE).
