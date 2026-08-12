# Automatic TUI theme detection — Design

**Date:** 2026-08-11
**Branch:** `feat/auto-theme-detection`

## Goal

The TUI ships two themes, dark and light, selected by the `tui.theme` config
key. The key defaults to `dark`, so a user on a light terminal sees a dark-tuned
palette until they find the setting and change it.

Add a third value, `auto`, which picks the theme from the terminal's actual
background color, and make it the default for new installations.

## Background

### The selection point is already a single seam

`internal/ui/app.go:156` calls `LoadTheme(cfg.TUI.Theme)` once, in `NewModel`.
That is the only caller. `LoadTheme` (`internal/ui/theme.go:43`) is a two-case
switch over the name with a dark fallback.

`ui.NewModel(provider, eventBus)` is evaluated as an argument to
`tea.NewProgram(...)` at `main.go:244-248`, so anything `LoadTheme` does runs
*before* Bubble Tea takes over the terminal. That is the only safe moment to
query the terminal, and it is where the seam already sits.

The main TUI renders to stdout (no `tea.WithOutput`), which is the same stream
lipgloss's default renderer queries.

### Detection needs no new dependency

`lipgloss.HasDarkBackground()` exists in the pinned
`lipgloss v1.1.1-0.20250404203927-76690c660834`
(`renderer.go:131`), and `termenv v0.16.0` — which implements it — is already in
the module graph as an indirect dependency.

Underneath it queries the terminal with OSC 11, falls back to the `COLORFGBG`
environment variable, applies its own timeout, caches the answer behind a
`sync.Once`, and reports *dark* when it cannot determine the background.

That last property matters: every failure mode lands on today's default, so no
user's appearance degrades relative to the current release.

### Existing installs already have `dark` on disk

`LoadConfig` (`internal/core/config.go:180-192`) **materializes the config file
on first run**: when the file is absent it writes `DefaultConfig()` out, which
includes `TUI: TUIConfig{Theme: "dark"}`.

So every existing user has `"theme": "dark"` written to disk — not because they
chose it, but because lazyagent wrote it. Nothing in the file distinguishes a
deliberate `dark` from a materialized default.

## Decisions (confirmed)

1. **`auto` is a third value of the existing key**, not a separate setting.
   `dark` and `light` keep working and keep overriding detection.
2. **`DefaultConfig()` changes to `auto`.** New installations detect by default.
3. **No migration.** Existing config files are left exactly as they are, so
   users who already run lazyagent see no change until they set `"auto"`
   themselves. This trades reach for zero surprise, and was chosen deliberately
   over rewriting `dark` → `auto` on upgrade.
4. **Unknown and empty values still fall back to `dark`**, matching today's
   behavior, rather than falling back to `auto`.

## Theme resolution

`LoadTheme` gains an `auto` case. Because detection performs terminal I/O, the
detector is injected into a pure inner function so the resolution logic stays
unit-testable without a terminal:

```go
// LoadTheme returns the theme for the given name. "auto" resolves against the
// terminal's background color; unrecognized names fall back to dark.
func LoadTheme(name string) Theme {
	return loadTheme(name, lipgloss.HasDarkBackground)
}

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

`dark` gets an explicit case even though `default` would still cover it. The
behavior is identical; stating it makes the fallback deliberate rather than
incidental, which matters now that a third named value exists.

No call site changes: `LoadTheme`'s signature is unchanged, so
`internal/ui/app.go:156` stays as it is.

## Configuration

`TUIConfig.Theme`'s comment changes to name all three values. `DefaultConfig()`
sets `Theme: "auto"`.

`LoadConfig`'s backfill block is **not** extended to touch `Theme`. It currently
backfills `Agents` and `ExcludeCWDSubstrings` when absent and re-saves; adding
`Theme` there would rewrite existing files and amount to the migration decision
3 rejected.

## Failure modes

| Condition | Result |
|-----------|--------|
| Terminal answers the OSC 11 query | Detected theme |
| Terminal ignores the query (some multiplexers, notably `tmux`) | Dark |
| `COLORFGBG` set but no OSC support | Value derived from `COLORFGBG` |
| stdout is not a TTY (piped, redirected) | Dark |

Detection never blocks indefinitely — termenv applies its own timeout — and
never errors out: it answers dark when unsure. There is no user-visible error
path to design, and nothing to log.

## Testing

Table-driven tests over `loadTheme`, with the detector replaced by a stub, so no
test performs terminal I/O:

| Name | Detector | Expected |
|------|----------|----------|
| `light` | not called | `LightTheme()` |
| `dark` | not called | `DarkTheme()` |
| `auto` | returns true | `DarkTheme()` |
| `auto` | returns false | `LightTheme()` |
| `nonsense` | not called | `DarkTheme()` |
| `""` | not called | `DarkTheme()` |

Assertions compare the `Text` field, which differs unambiguously between the two
themes (`#F9FAFB` dark, `#111827` light).

A separate assertion covers decision 2: `DefaultConfig().TUI.Theme == "auto"`.

The `light`, `dark`, unknown and empty cases additionally assert the detector was
**not** called — that is what proves an explicit choice overrides detection
rather than merely agreeing with it.

## Documentation

- `docs/reference/configuration.md` — the sample config at line 35 still shows
  `"theme": "dark"`, and the `### tui.theme` section at line 130 documents two
  values. Both gain `auto`, documented as the default for new installations,
  with the explicit note that existing installs are not migrated and how to opt
  in.
- `docs/interfaces/terminal-ui.md:57` — "Two themes ship in: `dark` (default)
  and `light`" becomes three values, with the failure table above so the `tmux`
  case is documented rather than discovered.

`docs/getting-started/quickstart.md:77` mentions the theme only in prose that
links to Configuration, and carries no sample config — it needs no change.

## Out of scope

- **The macOS GUI.** It is a Wails/Svelte app with no light/dark handling at all
  (no `prefers-color-scheme` anywhere in `frontend/src`), and a terminal's
  background does not apply to it.
- **The session picker and the chatops selector.**
  `internal/sessions/picker.go` and `internal/chatops/selector.go` run their own
  Bubble Tea programs against stderr with their own hardcoded styling; neither
  references `ui.Theme`.
- **`lazyagent limits` CLI colors.** `internal/limits/format.go` uses hardcoded
  lipgloss styles that never pass through a `Theme`.
- **Live re-detection.** If the terminal's theme changes while the TUI is
  running, the palette does not follow. Detection happens once at startup.
- **Migrating existing configs** — see decision 3.
