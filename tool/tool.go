package tool

type ToolSpec struct {
	// Name uniquely identifies the tool inside a Registry. Workspace
	// tools intentionally collide with global tools by name to shadow
	// them.
	Name string `yaml:"name"`

	// Description is the LLM-facing blurb explaining what the tool
	// does. Required.
	Description string `yaml:"description"`

	// Environment variables the tool needs to run.
	Env []string `yaml:"env"`

	// Commands lists the shell commands a user tool will execute.
	// Empty for native tools and for "data-shape" user tools that
	// only reshape data without invoking the shell.
	//
	// Required to be empty when Mode == PermDenyAll for user tools —
	// the loader and Registry.Register both enforce this.
	Commands []Command `yaml:"commands"`
}

type CommandMode string

const (
	CommandModeWhitelist CommandMode = "whitelist"
	CommandModeBlacklist CommandMode = "blacklist"
)

// Command is one node in the policy tree.
//
// The matcher uses strict subcommand routing: at every level that has
// children, the next token must be a known subcommand or matching
// fails fast. There is no per-level pass-through for parent flags —
// to allow a flag at a parent level, model the flag as a subcommand
// (see read_tool.yaml's `--no-pager` for an example).
//
// As a consequence, only the leaf entry (the deepest matched
// command) validates arguments. AllowedArgs / DisallowedArgs /
// RequiredArgs are evaluated only when the entry is reached as the
// terminal command — at intermediate routing levels they are dead
// config. The one rule that propagates downward is
// SubcommandsConfig.DisallowedSubArgs, which the matcher inherits
// recursively into every descendant entry's effective denylist.
type Command struct {
	Command string `yaml:"command"`

	// Subcommands declares this command's children. Strict routing
	// means the next token after this command's name must be one of
	// Subcommands.Commands' names — anything else is rejected at
	// routing time. Permissions on the parent do NOT apply to the
	// subcommands' own arg validation; only DisallowedSubArgs flows
	// downward (recursively to every descendant).
	Subcommands *SubcommandsConfig `yaml:"subcommands,omitempty"`

	// Mode determines which leaf-level arg list is consulted:
	// "whitelist" means only AllowedArgs pass; "blacklist" means
	// anything not in DisallowedArgs (own + inherited) passes.
	// Only consulted when this entry is the deepest matched command.
	Mode CommandMode `yaml:"mode"`

	// AllowedArgs is the whitelist used when Mode == "whitelist" and
	// this entry is the leaf. Dead config at intermediate routing
	// levels.
	AllowedArgs []Argument `yaml:"allowed_args,omitempty"`

	// DisallowedArgs is the blacklist used when Mode == "blacklist"
	// and this entry is the leaf. Combined with every ancestor's
	// SubcommandsConfig.DisallowedSubArgs to form the effective
	// denylist. Dead config at intermediate routing levels.
	DisallowedArgs []Argument `yaml:"disallowed_args,omitempty"`

	// RequiredArgs lists patterns that must each be matched by some
	// token in the leaf's argv. Order in YAML is preserved for
	// readability but doesn't impose positional constraints. Dead
	// config at intermediate routing levels.
	RequiredArgs []Argument `yaml:"required_args,omitempty"`
}

// SubcommandsConfig holds a command's children plus the cross-cutting
// denylist that propagates to every descendant.
type SubcommandsConfig struct {
	// Commands are the entries reachable as direct subcommands of the
	// parent. Strict routing requires the next token to match one of
	// these by name.
	Commands []Command `yaml:"commands"`

	// DisallowedSubArgs is recursively inherited into every
	// descendant's effective DisallowedArgs list. Use it to declare
	// denials once at a tree node and have them apply to every leaf
	// underneath — e.g., declaring `--textconv` denied at git's
	// SubcommandsConfig blocks it on `git --no-pager log`,
	// `git --no-pager show`, and every other reachable leaf.
	DisallowedSubArgs []Argument `yaml:"disallowed_sub_args,omitempty"`
}

type Argument struct {
	// Specific string value to allow/disallow.
	Arg string `yaml:"arg,omitempty"`

	// Path specification to allow/disallow (e.g., "*.go" or "/tmp/*").
	PathSpec string `yaml:"path_spec,omitempty"`

	// Regular expression to match arguments to allow/disallow.
	Regex string `yaml:"regex"`
}
