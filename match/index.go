package match

import (
	"fmt"
	"regexp"

	"github.com/devon-caron/opensieve/tool"
)

// Index is a precomputed lookup structure built from a tool.ToolSpec
// at load time. All regex compilation and list merging happens here,
// so the matcher's hot path does no allocation or compilation work.
type Index struct {
	// byName maps a top-level command name to its compiled entry.
	// Example: "git" -> *entry for git.
	byName map[string]*entry
}

// entry is the compiled form of a tool.Command, carrying precompiled
// patterns and effective (merged) arg lists for subcommands.
type entry struct {
	name string // the command name, e.g., "git" or "log"
	raw  *tool.Command

	mode tool.CommandMode

	// Compiled, merged arg lists. For subcommands, these include
	// inherited entries from the parent's SubcommandsConfig.
	allowed    []*compiledArg
	disallowed []*compiledArg
	required   []*compiledArg

	// subByName maps subcommand name -> subcommand entry. Nil for
	// commands without subcommands.
	subByName map[string]*entry
}

// compiledArg is the precompiled form of a tool.Argument.
//
// Exactly one of the fields is populated based on which field of the
// source Argument was set. The original is retained for error
// reporting.
type compiledArg struct {
	raw tool.Argument

	exact string         // set if raw.Arg is populated
	regex *regexp.Regexp // set if raw.Regex is populated
	path  string         // set if raw.PathSpec is populated
}

// humanPattern returns a user-facing string describing what this
// compiled arg matches, for use in error messages.
func (a *compiledArg) humanPattern() string {
	switch {
	case a.exact != "":
		return a.exact
	case a.regex != nil:
		return "regex:" + a.regex.String()
	case a.path != "":
		return "path:" + a.path
	}
	return "<empty>"
}

// BuildIndex compiles a ToolSpec into an Index suitable for fast
// matching. It fails if any regex in the policy does not compile or
// if the policy is structurally invalid.
func BuildIndex(spec *tool.ToolSpec) (*Index, error) {
	idx := &Index{byName: make(map[string]*entry, len(spec.Commands))}
	for i := range spec.Commands {
		cmd := &spec.Commands[i]
		e, err := buildEntry(cmd, nil)
		if err != nil {
			return nil, fmt.Errorf("command %q: %w", cmd.Command, err)
		}
		if _, dup := idx.byName[e.name]; dup {
			return nil, fmt.Errorf("duplicate command %q", e.name)
		}
		idx.byName[e.name] = e
	}
	return idx, nil
}

// buildEntry compiles a single command (and its subcommands, if any).
// parentSubCfg is the parent's SubcommandsConfig if this entry is a
// subcommand; nil for top-level commands.
func buildEntry(cmd *tool.Command, parentSubCfg *tool.SubcommandsConfig) (*entry, error) {
	if cmd.Command == "" {
		return nil, fmt.Errorf("command name is empty")
	}

	e := &entry{
		name: cmd.Command,
		raw:  cmd,
		mode: cmd.Mode,
	}

	own, err := compileArgLists(cmd)
	if err != nil {
		return nil, err
	}

	var inheritedAllowed, inheritedDisallowed, inheritedRequired []*compiledArg
	if parentSubCfg != nil {
		inheritedAllowed, err = compileArgs(parentSubCfg.AllowedSubArgs)
		if err != nil {
			return nil, fmt.Errorf("inherited allowed_sub_args: %w", err)
		}
		inheritedDisallowed, err = compileArgs(parentSubCfg.DisallowedSubArgs)
		if err != nil {
			return nil, fmt.Errorf("inherited disallowed_sub_args: %w", err)
		}
		inheritedRequired, err = compileArgs(parentSubCfg.RequiredSubArgs)
		if err != nil {
			return nil, fmt.Errorf("inherited required_sub_args: %w", err)
		}
	}

	e.allowed = append(own.allowed, inheritedAllowed...)
	e.disallowed = append(own.disallowed, inheritedDisallowed...)
	e.required = append(own.required, inheritedRequired...)

	if cmd.Subcommands != nil && len(cmd.Subcommands.Commands) > 0 {
		e.subByName = make(map[string]*entry, len(cmd.Subcommands.Commands))
		for i := range cmd.Subcommands.Commands {
			sub := &cmd.Subcommands.Commands[i]
			se, err := buildEntry(sub, cmd.Subcommands)
			if err != nil {
				return nil, fmt.Errorf("subcommand %q: %w", sub.Command, err)
			}
			if _, dup := e.subByName[se.name]; dup {
				return nil, fmt.Errorf("duplicate subcommand %q under %q",
					se.name, e.name)
			}
			e.subByName[se.name] = se
		}
	}

	return e, nil
}

// compiledLists holds the three compiled arg lists for a single
// command's own policy.
type compiledLists struct {
	allowed    []*compiledArg
	disallowed []*compiledArg
	required   []*compiledArg
}

func compileArgLists(cmd *tool.Command) (*compiledLists, error) {
	allowed, err := compileArgs(cmd.AllowedArgs)
	if err != nil {
		return nil, fmt.Errorf("allowed_args: %w", err)
	}
	disallowed, err := compileArgs(cmd.DisallowedArgs)
	if err != nil {
		return nil, fmt.Errorf("disallowed_args: %w", err)
	}
	required, err := compileArgs(cmd.RequiredArgs)
	if err != nil {
		return nil, fmt.Errorf("required_args: %w", err)
	}
	return &compiledLists{
		allowed: allowed, disallowed: disallowed, required: required,
	}, nil
}

func compileArgs(args []tool.Argument) ([]*compiledArg, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make([]*compiledArg, 0, len(args))
	for i, a := range args {
		c, err := compileArg(a)
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i, err)
		}
		out = append(out, c)
	}
	return out, nil
}

// compileArg validates and compiles a single tool.Argument.
//
// Exactly one of Arg, Regex, or PathSpec must be populated. An empty
// Argument, or one with multiple fields set, is rejected at load
// time.
func compileArg(a tool.Argument) (*compiledArg, error) {
	set := 0
	if a.Arg != "" {
		set++
	}
	if a.Regex != "" {
		set++
	}
	if a.PathSpec != "" {
		set++
	}
	switch set {
	case 0:
		return nil, fmt.Errorf("argument has no field set")
	case 1:
		// ok
	default:
		return nil, fmt.Errorf("argument has multiple fields set; " +
			"exactly one of arg/regex/path_spec is permitted")
	}

	c := &compiledArg{raw: a}
	switch {
	case a.Arg != "":
		c.exact = a.Arg
	case a.Regex != "":
		re, err := regexp.Compile(a.Regex)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %w", a.Regex, err)
		}
		c.regex = re
	case a.PathSpec != "":
		// PathSpec is matched via doublestar at lookup time; we
		// validate the pattern by attempting a no-op match. A bad
		// pattern fails here rather than at the first use.
		if err := validatePathSpec(a.PathSpec); err != nil {
			return nil, fmt.Errorf("invalid path_spec %q: %w",
				a.PathSpec, err)
		}
		c.path = a.PathSpec
	}
	return c, nil
}
