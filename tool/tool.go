package tool

import "github.com/devon-caron/opensieve/permissions"

type ToolSpec struct {
	// Name uniquely identifies the tool inside a Registry. Workspace
	// tools intentionally collide with global tools by name to shadow
	// them.
	Name string

	// Description is the LLM-facing blurb explaining what the tool
	// does. Required.
	Description string

	// Mode is the tier the tool sits at. The tool is reachable when
	// the active session mode is >= Mode.
	Mode permissions.PermMode

	// Commands lists the shell commands a user tool will execute.
	// Empty for native tools and for "data-shape" user tools that
	// only reshape data without invoking the shell.
	//
	// Required to be empty when Mode == PermDenyAll for user tools —
	// the loader and Registry.Register both enforce this.
	Commands []Command
}

type CommandMode string

const (
	CommandModeAllow CommandMode = "allow"
	CommandModeDeny  CommandMode = "deny"
)

type Command struct {
	Command string `yaml:"command"`

	// Mode is the tier the command sits at. The command is reachable when
	// the active session mode is >= Mode.
	Mode CommandMode `yaml:"mode"`

	// AllowedArgs is a list of arguments that are allowed to be used with the command.
	AllowedArgs []Argument `yaml:"allowed_args,omitempty"`

	// DisallowedArgs is a list of arguments that are not allowed to be used with the command.
	DisallowedArgs []Argument `yaml:"disallowed_args,omitempty"`
}

type Argument struct {
	Arg      string `yaml:"arg,omitempty"`
	PathSpec string `yaml:"path_spec,omitempty"`
	Regex    string `yaml:"regex"`
}
