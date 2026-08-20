package core

import (
	"encoding/json"
	"testing"
)

func TestNormalizeCardDensity(t *testing.T) {
	for _, valid := range []string{"compact", "rich", "live"} {
		if got := NormalizeCardDensity(valid); got != valid {
			t.Errorf("NormalizeCardDensity(%q) = %q, want passthrough", valid, got)
		}
	}
	for _, invalid := range []string{"", "dense", "LIVE"} {
		if got := NormalizeCardDensity(invalid); got != "live" {
			t.Errorf("NormalizeCardDensity(%q) = %q, want \"live\"", invalid, got)
		}
	}
}

func TestCardDensityJSONKey(t *testing.T) {
	b, err := json.Marshal(Config{CardDensity: "rich"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["card_density"] != "rich" {
		t.Errorf("card_density key missing or wrong: %v", m)
	}
}

func TestNormalizeTerminal(t *testing.T) {
	for _, valid := range []string{"terminal", "kitty"} {
		if got := NormalizeTerminal(valid); got != valid {
			t.Errorf("NormalizeTerminal(%q) = %q, want passthrough", valid, got)
		}
	}
	// iterm2/ghostty/wezterm/alacritty are parked until their launch
	// incantations are verified on real setups.
	for _, invalid := range []string{"", "xterm", "Kitty", "iterm2", "ghostty", "wezterm", "alacritty"} {
		if got := NormalizeTerminal(invalid); got != "terminal" {
			t.Errorf("NormalizeTerminal(%q) = %q, want \"terminal\"", invalid, got)
		}
	}
}
