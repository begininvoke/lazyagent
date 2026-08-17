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
