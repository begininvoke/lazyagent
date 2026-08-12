package ui

import "github.com/charmbracelet/lipgloss"

// Theme holds all TUI colors. Each field is a lipgloss.Color so themes
// can be swapped at startup without touching rendering code.
type Theme struct {
	Primary     lipgloss.Color
	Accent      lipgloss.Color
	Warning     lipgloss.Color
	Danger      lipgloss.Color
	Muted       lipgloss.Color
	Text        lipgloss.Color
	Subtext     lipgloss.Color
	Border      lipgloss.Color
	BorderFocus lipgloss.Color
	SelectionBg lipgloss.Color

	// Title bar foreground colors (rendered on Primary background)
	TitleText    lipgloss.Color
	TitleSubtext lipgloss.Color
	TitleMuted   lipgloss.Color
	TitleWarning lipgloss.Color

	HelpBg      lipgloss.Color
	HelpKeyBg   lipgloss.Color
	ModalBg     lipgloss.Color
	OverlayBg   lipgloss.Color

	// Activity colors
	ActivityWaiting    lipgloss.Color
	ActivityThinking   lipgloss.Color
	ActivityCompacting lipgloss.Color
	ActivityReading    lipgloss.Color
	ActivityWriting    lipgloss.Color
	ActivityRunning    lipgloss.Color
	ActivitySearching  lipgloss.Color
	ActivityBrowsing   lipgloss.Color
	ActivitySpawning   lipgloss.Color
}

// LoadTheme returns the theme for the given name. "auto" resolves against the
// terminal's background color; unrecognized names fall back to dark.
//
// For "auto" this blocks on a terminal query (OSC 11, via lipgloss/termenv)
// that can take up to ~5s if nothing answers. It must be called before the
// terminal enters raw mode / the alt screen — currently guaranteed because
// NewModel (which calls LoadTheme) is evaluated as an argument to
// tea.NewProgram(...) in main.go, ahead of p.Run(). Do not hoist that
// construction below tea.NewProgram, and do not add tea.WithInput, without
// preserving this ordering.
func LoadTheme(name string) Theme {
	return loadTheme(name, lipgloss.HasDarkBackground)
}

// loadTheme is LoadTheme with the background detector injected, so theme
// resolution can be tested without a terminal. hasDarkBackground is consulted
// only for "auto" — an explicitly named theme never queries the terminal.
func loadTheme(name string, hasDarkBackground func() bool) Theme {
	switch name {
	case "light":
		return LightTheme()
	case "dark":
		return DarkTheme()
	case "auto":
		if hasDarkBackground() {
			return DarkTheme()
		}
		return LightTheme()
	default:
		return DarkTheme()
	}
}
