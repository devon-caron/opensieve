package permissions

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPermMode_String(t *testing.T) {
	cases := []struct {
		mode PermMode
		want string
	}{
		{PermDenyAll, "PermDenyAll"},
		{PermReadOnly, "PermReadOnly"},
		{PermAllowEdits, "PermAllowEdits"},
		{PermCustomElevated, "PermCustomElevated"},
		{PermDangerAllowAll, "PermDangerAllowAll"},
	}
	for _, c := range cases {
		if got := c.mode.String(); got != c.want {
			t.Errorf("(%d).String() = %q, want %q", int(c.mode), got, c.want)
		}
	}
}

func TestParseMode_Canonical(t *testing.T) {
	cases := []struct {
		in   string
		want PermMode
	}{
		{"PermDenyAll", PermDenyAll},
		{"PermReadOnly", PermReadOnly},
		{"PermAllowEdits", PermAllowEdits},
		{"PermCustomElevated", PermCustomElevated},
		{"PermDangerAllowAll", PermDangerAllowAll},
	}
	for _, c := range cases {
		got, err := ParseMode(c.in)
		if err != nil {
			t.Errorf("ParseMode(%q) returned error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseMode(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseMode_AliasesAndCase(t *testing.T) {
	cases := []struct {
		in   string
		want PermMode
	}{
		{"deny", PermDenyAll},
		{"DENY", PermDenyAll},
		{"  read  ", PermReadOnly},
		{"readonly", PermReadOnly},
		{"edit", PermAllowEdits},
		{"edits", PermAllowEdits},
		{"custom", PermCustomElevated},
		{"elevated", PermCustomElevated},
		{"danger", PermDangerAllowAll},
		{"all", PermDangerAllowAll},
	}
	for _, c := range cases {
		got, err := ParseMode(c.in)
		if err != nil {
			t.Errorf("ParseMode(%q) returned error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseMode(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseMode_Unknown(t *testing.T) {
	if _, err := ParseMode("nonsense"); err == nil {
		t.Error("ParseMode(\"nonsense\") returned nil error; want error")
	}
}

func TestPermMode_YAMLRoundtrip(t *testing.T) {
	for _, mode := range AllModes() {
		out, err := yaml.Marshal(mode)
		if err != nil {
			t.Errorf("yaml.Marshal(%v): %v", mode, err)
			continue
		}
		var got PermMode
		if err := yaml.Unmarshal(out, &got); err != nil {
			t.Errorf("yaml.Unmarshal(%q): %v", out, err)
			continue
		}
		if got != mode {
			t.Errorf("roundtrip: got %v, want %v (yaml=%q)", got, mode, out)
		}
	}
}

func TestAllModes_OrderedAndComplete(t *testing.T) {
	all := AllModes()
	want := []PermMode{
		PermDenyAll,
		PermReadOnly,
		PermAllowEdits,
		PermCustomElevated,
		PermDangerAllowAll,
	}
	if len(all) != len(want) {
		t.Fatalf("AllModes() len = %d, want %d", len(all), len(want))
	}
	for i := range want {
		if all[i] != want[i] {
			t.Errorf("AllModes()[%d] = %v, want %v", i, all[i], want[i])
		}
	}
}
