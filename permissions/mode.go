// Package permission defines clifford's tool permission harness: the
// five capability tiers, the registry that holds clifford-native and
// user-defined tools, the read-only command allowlist, and the YAML
// loader for user tools.
//
// Tool execution is intentionally out of scope here — this package
// builds the access machinery the future tool-calling layer will
// consult. The actual tools land in a follow-up change.
package permissions

import (
	"fmt"
	"strings"
)

// PermMode is one of the five capability tiers. Higher values are
// strictly more permissive: a session running at mode M reaches every
// tool whose Tool.Mode is <= M.
type PermMode int

const (
	// PermDenyAll allows only "data-shape" tools that declare zero
	// shell commands (e.g. JSON reshape helpers). User-defined tools
	// at this tier MUST have an empty Commands list — the loader
	// rejects YAML that violates this.
	PermDenyAll PermMode = iota

	// PermReadOnly adds clifford-native read tools and any user tool
	// at this tier. User tools at this tier and above can declare
	// arbitrary commands; the read-only command allowlist is advisory
	// for user tools (they can shoot themselves in the foot).
	PermReadOnly

	// PermAllowEdits adds clifford-native edit tools and user tools
	// at this tier — write-capable file edits.
	PermAllowEdits

	// PermCustomElevated adds user tools needing elevation beyond
	// pure edits. No clifford-native tools live at this tier.
	PermCustomElevated

	// PermDangerAllowAll adds every remaining tool and grants the LLM
	// unrestricted shell command execution. No clifford-native tools
	// live at this tier; user tools here exist to streamline workflows
	// rather than to gate logic the LLM could otherwise run directly.
	PermDangerAllowAll
)

// String returns the canonical name used in YAML and the UI.
func (m PermMode) String() string {
	switch m {
	case PermDenyAll:
		return "PermDenyAll"
	case PermReadOnly:
		return "PermReadOnly"
	case PermAllowEdits:
		return "PermAllowEdits"
	case PermCustomElevated:
		return "PermCustomElevated"
	case PermDangerAllowAll:
		return "PermDangerAllowAll"
	default:
		return fmt.Sprintf("PermMode(%d)", int(m))
	}
}

// Short returns a 6-char-or-less label suitable for the tab-bar tag.
func (m PermMode) Short() string {
	switch m {
	case PermDenyAll:
		return "DENY"
	case PermReadOnly:
		return "READ"
	case PermAllowEdits:
		return "EDIT"
	case PermCustomElevated:
		return "CUSTOM"
	case PermDangerAllowAll:
		return "DANGER"
	default:
		return "?"
	}
}

// AllModes returns the five modes in defined order. Used by the UI
// picker and the debug CLI listing.
func AllModes() []PermMode {
	return []PermMode{
		PermDenyAll,
		PermReadOnly,
		PermAllowEdits,
		PermCustomElevated,
		PermDangerAllowAll,
	}
}

// ParseMode resolves a string to a PermMode. Accepts the canonical
// name (case-insensitive), the short label, or a bare alias
// (deny / read / edit / custom / danger).
func ParseMode(s string) (PermMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "permdenyall", "deny", "denyall", "none":
		return PermDenyAll, nil
	case "permreadonly", "read", "readonly":
		return PermReadOnly, nil
	case "permallowedits", "edit", "edits", "allowedits":
		return PermAllowEdits, nil
	case "permcustomelevated", "custom", "elevated", "customelevated":
		return PermCustomElevated, nil
	case "permdangerallowall", "danger", "dangerallowall", "all":
		return PermDangerAllowAll, nil
	default:
		return 0, fmt.Errorf("unknown permission mode %q", s)
	}
}

// MarshalYAML emits the canonical name so config.yaml stays
// human-readable.
func (m PermMode) MarshalYAML() (any, error) {
	return m.String(), nil
}

// UnmarshalYAML accepts any form ParseMode accepts.
func (m *PermMode) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	parsed, err := ParseMode(s)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}
