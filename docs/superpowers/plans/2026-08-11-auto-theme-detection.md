# Automatic TUI theme detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `auto` as a third value of the `tui.theme` config key, resolving the TUI palette from the terminal's actual background color, and make it the default for new installations.

**Architecture:** `LoadTheme` gains an `auto` case backed by `lipgloss.HasDarkBackground()`. Because that call performs terminal I/O, the detector is injected into a pure inner `loadTheme` so resolution stays unit-testable without a terminal. `DefaultConfig()` switches to `auto`; existing config files are deliberately left untouched.

**Tech Stack:** Go 1.25.5, stdlib plus `github.com/charmbracelet/lipgloss` (already a direct dependency) and `github.com/muesli/termenv` (already an indirect one). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-08-11-auto-theme-detection-design.md`

## Global Constraints

- The three supported values are exactly `"auto"`, `"dark"`, and `"light"`.
- `DefaultConfig().TUI.Theme` is `"auto"`.
- **No migration.** `LoadConfig`'s backfill block must NOT be extended to touch `Theme`. A config file that already carries `"theme": "dark"` must still read `dark` after `LoadConfig` runs, including in the file it rewrites.
- Unrecognized and empty theme names fall back to `"dark"`, matching today's behavior.
- No new third-party dependencies; do not add anything to `go.mod`.
- Terminal I/O happens only through the injected detector. `loadTheme` must be callable in tests with no terminal present.
- `LoadTheme`'s exported signature stays `func LoadTheme(name string) Theme` — `internal/ui/app.go:156` must not need changing.
- Commit messages follow Conventional Commits and carry NO `Co-Authored-By` trailer.
- `docs/superpowers` is ignored by the user's global gitignore but tracked in this repo — use `git add -f` for files under that path. No task here commits under it.

---

### Task 1: `auto` theme resolution and the new default

**Files:**
- Modify: `internal/ui/theme.go:42-50`
- Modify: `internal/core/config.go:18` (field comment) and `internal/core/config.go:143` (default value)
- Create: `internal/ui/theme_test.go`
- Test: `internal/core/config_test.go` (append)

**Interfaces:**
- Consumes: `Theme`, `DarkTheme()`, `LightTheme()` (`internal/ui/theme.go`, `theme_dark.go`, `theme_light.go`); `lipgloss.HasDarkBackground() bool`; `DefaultConfig() Config` and `LoadConfig() Config` (`internal/core/config.go`).
- Produces:
  - `func loadTheme(name string, hasDarkBackground func() bool) Theme` — unexported, pure, the unit under test
  - `func LoadTheme(name string) Theme` — unchanged signature, now delegating to `loadTheme`

- [ ] **Step 1: Write the failing theme tests**

Create `internal/ui/theme_test.go`:

```go
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
```

- [ ] **Step 2: Run the theme tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestLoadThemeResolution|TestThemesDifferOnText' -v`
Expected: compile failure — `undefined: loadTheme`.

- [ ] **Step 3: Implement the resolution**

In `internal/ui/theme.go`, replace `LoadTheme` (lines 42-50) with:

```go
// LoadTheme returns the theme for the given name. "auto" resolves against the
// terminal's background color; unrecognized names fall back to dark.
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
```

`"dark"` gets its own case even though `default` would still catch it. Behavior is identical; naming it makes the fallback deliberate now that a third value exists.

The `lipgloss` import at `internal/ui/theme.go:3` is already present for the `Theme` field types — do not add a second one.

- [ ] **Step 4: Run the theme tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestLoadThemeResolution|TestThemesDifferOnText' -v`
Expected: PASS — 6 subtests plus the discriminator guard.

- [ ] **Step 5: Write the failing config tests**

Append to `internal/core/config_test.go`. The file already imports `encoding/json`, `os`, `path/filepath`, and `testing`.

```go
func TestDefaultConfigThemeIsAuto(t *testing.T) {
	if got := DefaultConfig().TUI.Theme; got != "auto" {
		t.Errorf("DefaultConfig().TUI.Theme = %q, want %q", got, "auto")
	}
}

// Existing installs already carry "theme": "dark" — written by LoadConfig
// itself on first run, not chosen by the user. Switching the default to "auto"
// must not migrate them, neither in the returned config nor in the file
// LoadConfig rewrites when it backfills other keys.
func TestLoadConfigDoesNotMigrateExistingTheme(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cfgDir := filepath.Join(tmpDir, "lazyagent")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	existing := map[string]interface{}{
		"window_minutes": 30,
		"agents":         map[string]bool{"claude": true},
		"tui":            map[string]string{"theme": "dark"},
	}
	data, _ := json.Marshal(existing)
	path := filepath.Join(cfgDir, "config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if got := LoadConfig().TUI.Theme; got != "dark" {
		t.Errorf("LoadConfig().TUI.Theme = %q, want %q — existing configs must not be migrated", got, "dark")
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var reread Config
	if err := json.Unmarshal(written, &reread); err != nil {
		t.Fatal(err)
	}
	if reread.TUI.Theme != "dark" {
		t.Errorf("config file on disk has theme %q after LoadConfig, want %q", reread.TUI.Theme, "dark")
	}
}

// A config file with no "tui" block at all expresses no choice, so it picks up
// the new default rather than the old hardcoded dark.
func TestLoadConfigAbsentThemeGetsAuto(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cfgDir := filepath.Join(tmpDir, "lazyagent")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	minimal := map[string]interface{}{
		"window_minutes": 30,
		"agents":         map[string]bool{"claude": true},
	}
	data, _ := json.Marshal(minimal)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	if got := LoadConfig().TUI.Theme; got != "auto" {
		t.Errorf("LoadConfig().TUI.Theme = %q, want %q", got, "auto")
	}
}
```

- [ ] **Step 6: Run the config tests to verify they fail**

Run: `go test ./internal/core/ -run 'TestDefaultConfigThemeIsAuto|TestLoadConfigDoesNotMigrateExistingTheme|TestLoadConfigAbsentThemeGetsAuto' -v`
Expected: `TestDefaultConfigThemeIsAuto` and `TestLoadConfigAbsentThemeGetsAuto` FAIL with `= "dark", want "auto"`. `TestLoadConfigDoesNotMigrateExistingTheme` already PASSES — it is the regression guard for the change you are about to make, not a red test.

- [ ] **Step 7: Change the default**

In `internal/core/config.go`, line 18, change the field comment:

```go
	Theme string `json:"theme"` // "auto" (default), "dark", or "light"
```

And line 143, inside `DefaultConfig()`:

```go
		TUI:                  TUIConfig{Theme: "auto"},
```

Do NOT add a `Theme` branch to `LoadConfig`'s backfill block (the `if cfg.Agents == nil` / `if cfg.ExcludeCWDSubstrings == nil` sequence). Backfilling `Theme` there would rewrite existing config files, which is exactly the migration this design rejected.

- [ ] **Step 8: Run the config tests to verify they pass**

Run: `go test ./internal/core/ -run 'TestDefaultConfigThemeIsAuto|TestLoadConfigDoesNotMigrateExistingTheme|TestLoadConfigAbsentThemeGetsAuto' -v`
Expected: PASS, all three.

- [ ] **Step 9: Verify the whole tree**

Run: `go build ./... && go vet ./internal/... && go test ./...`
Expected: build clean, vet silent, all tests PASS.

- [ ] **Step 10: Verify against a real terminal**

Launch the TUI with `go run .` in your normal terminal and confirm the palette matches your terminal's background. Quit with `q`.

Then force the opposite and confirm the override still wins:

```bash
mkdir -p /tmp/lazyagent-theme-check/lazyagent
printf '{"tui":{"theme":"light"}}' > /tmp/lazyagent-theme-check/lazyagent/config.json
XDG_CONFIG_HOME=/tmp/lazyagent-theme-check go run .
```

Expected: the light palette regardless of your terminal's background. Record which theme `auto` picked and whether it matched your terminal. Clean up with `rm -rf /tmp/lazyagent-theme-check`.

If you cannot run an interactive TUI in this environment, say so in your report rather than claiming the check passed.

- [ ] **Step 11: Commit**

```bash
git add internal/ui/theme.go internal/ui/theme_test.go internal/core/config.go internal/core/config_test.go
git commit -m "feat(tui): resolve the theme from the terminal background with auto"
```

---

### Task 2: Documentation

**Files:**
- Modify: `docs/reference/configuration.md:35` and `docs/reference/configuration.md:130-137`
- Modify: `docs/interfaces/terminal-ui.md:55-65`

**Interfaces:**
- Consumes: the behavior implemented in Task 1. Produces no new symbols.

- [ ] **Step 1: Update the sample config**

In `docs/reference/configuration.md`, line 35, inside the full sample config block, change:

```json
    "theme": "dark"
```

to:

```json
    "theme": "auto"
```

- [ ] **Step 2: Rewrite the `tui.theme` section**

In `docs/reference/configuration.md`, replace the whole `### tui.theme` section (lines 130-137, from the heading through the "All TUI colors…" sentence) with:

````markdown
### `tui.theme`

Default: `"auto"` for new installations. Supported values:

- `"auto"` — detect the terminal's background color at startup and pick the matching palette
- `"dark"` — Catppuccin-derived palette
- `"light"` — paper-like palette for bright environments

All TUI colors (panels, activity labels, help bar, overlays) are driven by the theme.

**Existing installations are not migrated.** lazyagent writes the config file on first run, so any install predating `auto` already carries `"theme": "dark"` on disk and keeps it. Set `"theme": "auto"` by hand to opt in.

Detection is not always possible, and every failure resolves to `dark` — the value the TUI used unconditionally before `auto` existed, so nothing degrades. See [Terminal UI](../interfaces/terminal-ui.md) for the full behavior.
````

- [ ] **Step 3: Rewrite the Themes section of the TUI doc**

In `docs/interfaces/terminal-ui.md`, replace the `## Themes` section (lines 55-65, from the heading through the "Every color…" sentence) with:

````markdown
## Themes

Three values ship in: `auto` — the default for new installations — plus `dark` and `light`. Set `tui.theme` in [Configuration](../reference/configuration.md):

```json
{
  "tui": { "theme": "auto" }
}
```

Every color — panels, activity states, help bar, overlays — is driven by the theme, so both palettes are fully coherent.

### How `auto` works

At startup, before the TUI takes over the screen, lazyagent asks the terminal for its background color with an OSC 11 query and picks the matching palette. The answer is read once: changing your terminal's theme while lazyagent is running does not repaint it until you restart.

Detection does not always succeed, and every failure resolves to `dark` — what the TUI used unconditionally before `auto` existed, so nothing gets worse than the previous release:

| Situation | Result |
|-----------|--------|
| The terminal answers the query | Detected palette |
| The terminal ignores it (notably inside `tmux`) | Dark |
| `COLORFGBG` is set but the terminal has no OSC support | Derived from `COLORFGBG` |
| Output is not a terminal (piped or redirected) | Dark |

Set `"dark"` or `"light"` explicitly to skip detection entirely — an explicit value never queries the terminal.

**Existing installations keep what they have.** lazyagent writes the config file on first run, so an install predating `auto` already carries `"theme": "dark"`. Set `"theme": "auto"` by hand to opt in.
````

- [ ] **Step 4: Verify no stale claim survives**

Run: `grep -rn "Two themes\|theme\": \"dark\"" docs/ README.md | grep -v docs/superpowers`
Expected: no output. Hits under `docs/superpowers/` are expected — the spec and this plan record the old behavior on purpose.

Run: `grep -rn -i "theme" docs/ README.md | grep -v docs/superpowers`
Expected: every remaining hit is either the two files you edited or `docs/getting-started/quickstart.md:77`, which mentions the theme only in prose linking to Configuration and needs no change.

- [ ] **Step 5: Commit**

```bash
git add docs/reference/configuration.md docs/interfaces/terminal-ui.md
git commit -m "docs(tui): document the auto theme and its detection limits"
```

---

## Verification checklist

Run after Task 2, before opening a PR:

- [ ] `go build ./...` — clean
- [ ] `go vet ./internal/...` — silent
- [ ] `go test ./...` — all PASS
- [ ] `go run .` in a normal terminal — palette matches the terminal's background
- [ ] `XDG_CONFIG_HOME` pointing at a config with `"theme": "light"` — light palette regardless of the terminal
- [ ] `grep -rn "Two themes" docs/ | grep -v docs/superpowers` — no output
- [ ] `git diff main --stat` shows only `internal/ui/theme.go`, `internal/ui/theme_test.go`, `internal/core/config.go`, `internal/core/config_test.go`, `docs/reference/configuration.md`, `docs/interfaces/terminal-ui.md`, and the two `docs/superpowers/` documents
