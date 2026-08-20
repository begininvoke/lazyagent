package core

import (
	"encoding/json"
	"testing"
)

func TestNormalizeDetailWidth(t *testing.T) {
	for _, valid := range []int{300, 400, 720, 2000} {
		if got := NormalizeDetailWidth(valid); got != valid {
			t.Errorf("NormalizeDetailWidth(%d) = %d, want passthrough", valid, got)
		}
	}
	for _, invalid := range []int{0, -1, 299, 2001} {
		if got := NormalizeDetailWidth(invalid); got != 400 {
			t.Errorf("NormalizeDetailWidth(%d) = %d, want 400", invalid, got)
		}
	}
}

func TestDetailWidthJSONKey(t *testing.T) {
	b, err := json.Marshal(Config{DetailWidth: 500})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["detail_width"] != float64(500) {
		t.Errorf("detail_width key missing or wrong: %v", m)
	}
}
