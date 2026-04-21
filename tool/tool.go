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

type Command struct {
	Command string `yaml:"command"`

	// Subcommands are promoted versions of arguments, in cases where subcommands
	// are featured enough to deserve their own set of permissions.
	// Crucially, any permissions on the parent do NOT apply to the subcommands.
	// Subcommands must define their own allows/denies.
	// If a subcommand is defined, it will be allowed regardless of the parent's command mode.
	Subcommands []Command `yaml:"subcommands"`

	// Mode is the permission that determines if the AllowedArgs or DisallowedArgs list
	// is used. If Mode is "whitelist", only the arguments whitelisted in AllowedArgs are allowed.
	// If Mode is "blacklist", all arguments except those blacklisted in DisallowedArgs are allowed.
	Mode CommandMode `yaml:"mode"`

	// AllowedArgs is a list of arguments that are allowed to be used with the command.
	AllowedArgs []Argument `yaml:"allowed_args,omitempty"`

	// DisallowedArgs is a list of arguments that are not allowed to be used with the command.
	DisallowedArgs []Argument `yaml:"disallowed_args,omitempty"`

	// RequiredArgs is a list of arguments that must be provided with the command. Supplied in order presented in YAML.
	RequiredArgs []Argument `yaml:"required_args,omitempty"`
}

type Argument struct {
	// Specific string value to allow/disallow.
	Arg string `yaml:"arg,omitempty"`

	// Path specification to allow/disallow (e.g., "*.go" or "/tmp/*").
	PathSpec string `yaml:"path_spec,omitempty"`

	// Regular expression to match arguments to allow/disallow.
	Regex string `yaml:"regex"`
}
