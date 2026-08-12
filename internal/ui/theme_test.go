package ui

import "testing"

// stubDetector returns a hasDarkBackground func with a fixed answer, plus a
// pointer to a flag recording whether it was called. An explicitly named theme
// must resolve without querying the terminal at all, and the flag is what
// proves it.
func stubDetector(dark bool) (func() bool, *bool) {
	called := false
	return func() bool {
		called = true
		return dark
	}, &called
}

// The resolution table below discriminates on Text. If the two themes ever
// converge on that field, those assertions would silently stop proving
// anything, so guard it explicitly.
func TestThemesDifferOnText(t *testing.T) {
	if DarkTheme().Text == LightTheme().Text {
		t.Fatal("DarkTheme().Text == LightTheme().Text — the resolution tests need a different discriminator")
	}
}

func TestLoadThemeResolution(t *testing.T) {
	const (
		darkText  = "#F9FAFB"
		lightText = "#111827"
	)
	cases := []struct {
		name         string
		theme        string
		terminalDark bool
		wantText     string
		wantCalled   bool
	}{
		{"explicit light wins over a dark terminal", "light", true, lightText, false},
		{"explicit dark wins over a light terminal", "dark", false, darkText, false},
		{"auto on a dark terminal", "auto", true, darkText, true},
		{"auto on a light terminal", "auto", false, lightText, true},
		{"unknown name falls back to dark", "nonsense", false, darkText, false},
		{"empty name falls back to dark", "", false, darkText, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			detect, called := stubDetector(c.terminalDark)
			got := loadTheme(c.theme, detect)
			if string(got.Text) != c.wantText {
				t.Errorf("loadTheme(%q).Text = %q, want %q", c.theme, got.Text, c.wantText)
			}
			if *called != c.wantCalled {
				t.Errorf("loadTheme(%q): detector called = %v, want %v", c.theme, *called, c.wantCalled)
			}
		})
	}
}
