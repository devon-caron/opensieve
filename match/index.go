package match

import (
	"fmt"
	"regexp"

	"github.com/devon-caron/opensieve/tool"
)

// Index is the precomputed lookup structure built from a tool.ToolSpec
// at load time. All regex compilation, doublestar validation, and
// inheritance flattening happens here so the matcher's hot path does
// no allocation or compilation work.
type Index struct {
	// byName maps a top-level command name to its compiled entry.
	byName map[string]*entry

	// names is the registered command names in declaration order. Used
	// to render the "allowed:" line of an ErrCommandNotAllowed.
	names []string
}

// entry is the compiled form of a tool.Command, carrying precompiled
// patterns and the effective disallowed-args list (own ∪ every
// ancestor's subcommands.disallowed_sub_args).
type entry struct {
	name string
	raw  *tool.Command
	mode tool.CommandMode

	// Compiled arg lists. allowed/required hold only this entry's own
	// declarations; disallowed is the recursive union of own +
	// every ancestor's subcommands.disallowed_sub_args, so a denial
	// anywhere in the chain reaches every descendant. Each
	// compiledArg.source records its provenance so error messages can
	// cite the YAML location.
	allowed    []*compiledArg
	disallowed []*compiledArg
	required   []*compiledArg

	// subByName maps subcommand name → subcommand entry. Nil for
	// commands without subcommands.
	subByName map[string]*entry

	// subNames is the subcommand names in declaration order. Used to
	// render the "subs:" line of routing-stage ErrArgNotAllowed.
	subNames []string
}

// compiledArg is the precompiled form of a tool.Argument. Exactly one
// of exact/regex/path is populated, mirroring the source Argument's
// Arg/Regex/PathSpec field.
type compiledArg struct {
	raw tool.Argument

	exact string
	regex *regexp.Regexp
	path  string

	// source is the YAML provenance string, e.g.
	//   "git.allowed_args[0]"
	//   "git.subcommands.disallowed_sub_args[1] (inherited)"
	//   "git.log.disallowed_args[0]"
	// Populated by the build path, never by the matcher.
	source string
}

// humanPattern returns a one-line description of what this rule
// matches, suitable for embedding in an error message.
func (a *compiledArg) humanPattern() string {
	switch {
	case a.exact != "":
		return fmt.Sprintf("exact %q", a.exact)
	case a.regex != nil:
		return fmt.Sprintf("regex %q", a.regex.String())
	case a.path != "":
		return fmt.Sprintf("path %q", a.path)
	}
	return "<empty>"
}

// allowedPatterns returns the human-readable patterns for an entry's
// allowed list, used to populate Error.Allowed.
func (e *entry) allowedPatterns() []string {
	if len(e.allowed) == 0 {
		return nil
	}
	out := make([]string, len(e.allowed))
	for i, a := range e.allowed {
		out[i] = a.humanPattern()
	}
	return out
}

// BuildIndex compiles a ToolSpec into an Index suitable for fast
// matching. It fails if any regex doesn't compile, any doublestar
// pattern is malformed, or the policy is structurally invalid (empty
// names, duplicates, arguments with multiple fields set).
func BuildIndex(spec *tool.ToolSpec) (*Index, error) {
	idx := &Index{
		byName: make(map[string]*entry, len(spec.Commands)),
		names:  make([]string, 0, len(spec.Commands)),
	}
	for i := range spec.Commands {
		cmd := &spec.Commands[i]
		e, err := buildEntry(cmd, "", nil)
		if err != nil {
			return nil, fmt.Errorf("command %q: %w", cmd.Command, err)
		}
		if _, dup := idx.byName[e.name]; dup {
			return nil, fmt.Errorf("duplicate command %q", e.name)
		}
		idx.byName[e.name] = e
		idx.names = append(idx.names, e.name)
	}
	return idx, nil
}

// buildEntry compiles a single command (and its subcommands, if any).
// pathPrefix is the dotted YAML path leading to this command (empty
// for top-level entries). inheritedDeny is the accumulated list of
// disallowed_sub_args compiled from every ancestor's
// subcommands.disallowed_sub_args block; nil for top-level entries.
//
// Each level appends inheritedDeny to its own disallowed list, then
// folds its own subcommands.disallowed_sub_args into the accumulator
// before recursing. That makes a denial declared anywhere in the
// chain visible at every descendant, which is what the policy reads
// like in the YAML.
func buildEntry(cmd *tool.Command, pathPrefix string, inheritedDeny []*compiledArg) (*entry, error) {
	if cmd.Command == "" {
		return nil, fmt.Errorf("command name is empty")
	}

	e := &entry{
		name: cmd.Command,
		raw:  cmd,
		mode: cmd.Mode,
	}

	// Own arg lists. Source prefix is "<full-path>.<list-name>".
	ownPath := joinPath(pathPrefix, cmd.Command)
	allowedOwn, err := compileArgs(cmd.AllowedArgs, ownPath+".allowed_args", false)
	if err != nil {
		return nil, fmt.Errorf("allowed_args: %w", err)
	}
	disallowedOwn, err := compileArgs(cmd.DisallowedArgs, ownPath+".disallowed_args", false)
	if err != nil {
		return nil, fmt.Errorf("disallowed_args: %w", err)
	}
	requiredOwn, err := compileArgs(cmd.RequiredArgs, ownPath+".required_args", false)
	if err != nil {
		return nil, fmt.Errorf("required_args: %w", err)
	}

	e.allowed = allowedOwn
	e.disallowed = append(disallowedOwn, inheritedDeny...)
	e.required = requiredOwn

	if cmd.Subcommands != nil && len(cmd.Subcommands.Commands) > 0 {
		// Compile this entry's own subcommands.disallowed_sub_args;
		// tag them as inherited because that's how every descendant
		// will encounter them.
		ownSubDeny, err := compileArgs(cmd.Subcommands.DisallowedSubArgs,
			ownPath+".subcommands.disallowed_sub_args", true)
		if err != nil {
			return nil, fmt.Errorf("subcommands.disallowed_sub_args: %w", err)
		}
		// Children inherit everything we inherited PLUS our own.
		childDeny := make([]*compiledArg, 0, len(inheritedDeny)+len(ownSubDeny))
		childDeny = append(childDeny, inheritedDeny...)
		childDeny = append(childDeny, ownSubDeny...)

		e.subByName = make(map[string]*entry, len(cmd.Subcommands.Commands))
		e.subNames = make([]string, 0, len(cmd.Subcommands.Commands))
		for i := range cmd.Subcommands.Commands {
			sub := &cmd.Subcommands.Commands[i]
			se, err := buildEntry(sub, ownPath, childDeny)
			if err != nil {
				return nil, fmt.Errorf("subcommand %q: %w", sub.Command, err)
			}
			if _, dup := e.subByName[se.name]; dup {
				return nil, fmt.Errorf("duplicate subcommand %q under %q",
					se.name, e.name)
			}
			e.subByName[se.name] = se
			e.subNames = append(e.subNames, se.name)
		}
	}

	return e, nil
}

// joinPath assembles a dotted YAML path. Treats an empty prefix as
// "no prefix" rather than producing a leading dot.
func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// compileArgs compiles a slice of tool.Argument into compiledArgs and
// stamps each with a source string of the form "<prefix>[<index>]".
// When inherited is true, " (inherited)" is appended so error messages
// can distinguish parent-defined rules from the entry's own.
func compileArgs(args []tool.Argument, sourcePrefix string, inherited bool) ([]*compiledArg, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make([]*compiledArg, 0, len(args))
	for i, a := range args {
		c, err := compileArg(a)
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i, err)
		}
		c.source = fmt.Sprintf("%s[%d]", sourcePrefix, i)
		if inherited {
			c.source += " (inherited)"
		}
		out = append(out, c)
	}
	return out, nil
}

// compileArg validates and compiles a single tool.Argument. Exactly
// one of Arg, Regex, or PathSpec must be populated; an empty Argument
// or one with multiple fields set is rejected.
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
		// validate the pattern here by attempting a no-op match so a
		// bad pattern fails at load rather than at first use.
		if err := validatePathSpec(a.PathSpec); err != nil {
			return nil, fmt.Errorf("invalid path_spec %q: %w",
				a.PathSpec, err)
		}
		c.path = a.PathSpec
	}
	return c, nil
}
