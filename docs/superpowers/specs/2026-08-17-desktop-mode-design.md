# Desktop mode on detach — Design

**Date:** 2026-08-17
**Branch:** `feat/desktop-mode`

## Goal

Today `--gui` runs lazyagent as a menu bar accessory: a tray icon with an
attached panel, plus a "detached" window that is just the same 380px-wide
panel UI in a resizable frame. When the user detaches, the app should become
a **full desktop app**: Dock icon, Cmd-Tab presence, native macOS menus, and
a dashboard UI designed for a real window. Re-attaching returns to the
unobtrusive menu bar accessory.

Design validated interactively (visual mockups) with three decisions locked:

1. **Layout:** dashboard grid of session cards, not master–detail.
2. **Card density:** three levels — `compact`, `rich`, `live` — switchable
   from the toolbar, **default `live`**, persisted across restarts.
3. **Detail view:** side-by-side push panel (fixed right column, grid
   reflows), not an overlay drawer. `j`/`k` navigates sessions while the
   panel is open; `Esc` closes it.

## Part 1 — Core macOS behavior (`internal/tray/`)

### Activation policy toggle

Wails v3 sets `ActivationPolicyAccessory` once at startup and exposes no
runtime setter. macOS supports switching at runtime
(`[NSApp setActivationPolicy:]`) — the standard pattern for apps that
alternate menu-bar/desktop modes.

New file `internal/tray/activation_darwin.go` (build tags
`!notray && darwin`), ~30 lines of cgo/Objective-C:

```go
// setActivationPolicy switches NSApp between Regular (Dock icon,
// Cmd-Tab) and Accessory (menu bar only). Dispatched on the main
// thread via dispatch_async(dispatch_get_main_queue(), ...).
func setActivationPolicy(regular bool)
```

It does not touch Wails internals. After switching to Regular, explicitly
activate the app (`activateIgnoringOtherApps:`) so the detached window comes
to the foreground with keyboard focus.

### Detach() / Attach() wiring (`service.go`)

- `Detach()`: existing behavior, plus → policy Regular, set the app icon
  (`app.SetIcon` with the colored logo asset — not the monochrome tray
  template icon), install the native menu bar, activate.
- `Attach()`: existing behavior, plus → policy Accessory. The installed app
  menu is harmless while in accessory mode; no teardown needed.

### Native menus (desktop mode)

Standard roles via the Wails menu API (already used for the tray context
menu):

- **App menu:** About lazyagent, Hide, Quit (Cmd+Q — full quit, reusing the
  existing quit-watchdog path).
- **Edit:** standard roles (needed for copy/paste in the search field).
- **Window:** Minimize, Zoom, Close (Cmd+W).

### Window close semantics (unchanged)

Closing the detached window (red button or Cmd+W) keeps the current
behavior: intercepted, window hides, app re-attaches to the tray. Cmd+Q is
the only full exit from the GUI. The tray icon stays visible in desktop mode
(tray click still toggles the detached window).

## Part 2 — Desktop UI (frontend, Svelte)

`App.svelte` becomes a thin router on the existing `isDetached` state
(already synced via the `detach:changed` event):

- **attached** → `PanelView.svelte`: the current compact UI extracted
  verbatim. Zero visual/behavioral change to the menu bar panel.
- **detached** → `DesktopView.svelte`: the dashboard.

### DesktopView layout

- **Toolbar:** app title + active count on the left; density segmented
  control (`compact | rich | live`), activity filter, time window `+/-`,
  limits toggle, pin, attach button on the right.
- **Grid:** responsive card grid (CSS grid, `auto-fill`/`minmax`; column
  count follows window width and card density).
- **Detail:** `DetailPanel.svelte` as a fixed-width right column pushed into
  the layout when a session is selected. Grid narrows and stays interactive.
  `j`/`k` moves the selection with the panel open; `Esc` deselects/closes.
- **Limits:** full-content toggle view, as today.
- **Footer:** keyboard hints, as today.
- Existing shortcuts unchanged in both modes (`/`, `f`, `l`, `+/-`, `d`,
  `r`, `j/k`, `Esc`).

### New/changed components

| Component | Role |
|---|---|
| `PanelView.svelte` | current App.svelte layout, extracted |
| `DesktopView.svelte` | toolbar + grid + push panel |
| `SessionCard.svelte` | one card, renders all three densities |
| `DetailPanel.svelte` | detail content reorganized for the column (reuses `SessionDetail.svelte` logic) |

`Sparkline`, `ActivityBadge`, `LimitsPage`, `SessionList` (panel mode) are
reused as-is. Styling stays Tailwind + the existing Catppuccin Mocha theme
tokens in `app.css`.

### Card densities

- **compact:** name + agent glyph, activity badge, sparkline, cost, last
  activity age.
- **rich:** compact + model, git branch, message count, current tool line
  (`▸ Edit src/routes/login.ts`).
- **live** (default): rich + last agent message snippet (2-line clamp,
  italic). The dashboard reads as a live feed of what each agent is doing.

## Part 3 — Data (Go service)

### SessionItem payload extension

`live` cards need data that today only exists in the detail payload. Extend
`SessionItem` with:

- `currentTool` (string) — already on `model.Session`.
- `lastMessage` (string) — text of the newest entry in
  `sess.RecentMessages`, truncated to 140 runes (ellipsis appended when
  truncated).

No extra I/O: both fields are already in memory on `model.Session`.

### Density persistence

Config-file backed (not webview localStorage, which can be ephemeral):

- `GetCardDensity() string` / `SetCardDensity(d string) error` on
  `SessionService`; value stored in the lazyagent config JSON
  (`cardDensity`, one of `compact|rich|live`; missing/invalid → `live`).

## Edge cases

- **Re-attach focus quirks:** Regular→Accessory can leave the app without
  proper focus/window ordering (known macOS behavior, outside Wails' tested
  paths in v3 alpha). Mitigation: explicit re-activation ordering after the
  policy switch; verified manually.
- **Demo mode / `--agent`:** orthogonal to desktop mode; no changes.
- **`notray` builds:** the cgo file shares the `!notray` build tag; non-GUI
  builds are unaffected.
- **Non-macOS:** activation policy and native menu calls are darwin-only;
  on other platforms Detach()/Attach() keep today's behavior (stub
  `activation_notdarwin.go` no-op).

## Testing

- **Go unit tests:** snippet truncation (rune-safe), `lastMessage`/
  `currentTool` presence in the list payload, density get/set round-trip
  incl. invalid-value fallback.
- **Manual checklist (AppKit is not automatable here):** Dock icon appears
  on detach and disappears on attach; Cmd-Tab entry; native menus present;
  Cmd+W re-attaches; Cmd+Q quits cleanly (watchdog path); focus correct
  after repeated detach/attach cycles; tray click toggles the detached
  window.
- **Frontend:** `make dev` walkthrough of the three densities, push panel
  open/close, keyboard navigation, and an unchanged attached panel.

## Out of scope

- Real `.app` bundle / Launch Services integration ("Keep in Dock" won't
  relaunch correctly with the bare binary). Possible follow-up.
- Windows/Linux desktop-mode equivalents.
- Any TUI changes.
