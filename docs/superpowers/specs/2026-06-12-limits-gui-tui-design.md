# Limits in the GUI and TUI — Design

**Date:** 2026-06-12
**Branch:** `feat/limits-gui-tui`

## Goal

Surface the existing `lazyagent limits` data (Claude, Codex, Grok, Kimi, Cursor
rate-limit / billing windows) inside the two interactive surfaces:

- **TUI:** press `l` to open a centered modal with two tabs — **Summary** and
  **Detailed**. The Detailed tab scrolls inside the modal.
- **GUI:** a **limits** button in the header, plus the `l` shortcut, opens a
  full limits **page**. `l` toggles it; `ESC` also goes back. The page has the
  same two views (Summary / Detailed) for consistency with the TUI.

Both surfaces read limits **on entry only** (no polling, no auto-refresh).
Leaving and re-entering the limits section re-fetches.

## Decisions (confirmed)

1. **Async + loading state.** Open the section immediately, show "Loading
   limits…", populate as results arrive. The section stays responsive and can
   be closed mid-load.
2. **Omit missing agents silently** (match the CLI). Only agents with real data
   are shown. If none are available, show a single "No supported agents
   detected" message.
3. **TUI renders with its own Theme colors** (not the CLI's chatops palette).

## Architecture

The work splits into three layers:

```
internal/limits/view.go   ← NEW: UI-agnostic computed view-model + concurrent FetchAll
        │
        ├──────────────► internal/ui (TUI)        renders View with Theme colors
        │
        └──────────────► internal/tray/service.go  GetLimits() returns View as JSON
                                 │
                                 └──► frontend LimitsPage.svelte renders View
```

The key idea: **all computation happens once** in the limits package
(used %, expected/elapsed %, pace, severity, reset times). Both UIs receive a
fully-computed, style-free `View` and only map severity → colors. This avoids
duplicating the pace/threshold logic across Go-TUI and TypeScript.

### Layer 1 — `internal/limits/view.go` (new)

Reuses the existing unexported helpers (`paceForWindow`, `elapsedPercent`,
`classifyPace`, `summaryWindow`, `summaryCellSeverity`, `summaryProviderName`,
`isFiveHourWindow`/`isWeeklyWindow`/`isGlobalWindow`, `humanDuration`) — no logic
is reimplemented. The existing CLI renderers in `format.go` / `run.go` are left
untouched.

**Concurrent fetch:**

```go
// FetchAll fetches every supported provider concurrently using the same
// per-agent fetchers as the CLI. Not-installed and unavailable agents are
// skipped silently; transient errors are dropped (the agent is simply omitted).
// Reports are returned in canonical order: claude, codex, grok, kimi, cursor.
func FetchAll(ctx context.Context) []Report
```

**Computed view-model (all fields exported, JSON-friendly — no `time.Time`):**

```go
type Severity string // "low" | "medium" | "high" | "critical" | "under" | "default"

type WindowView struct {
    Label           string   // "5-hour" / "7-day" / "Monthly"
    UsedPercent     float64  // 0-100
    ExpectedPercent float64  // linear pace for elapsed window time (0-100)
    PaceLabel       string   // "underutilizing" | "on track" | "overutilizing" | ""
    PaceRatio       float64  // used / elapsed
    PaceKnown       bool     // false when window just reset (<1% elapsed)
    UsedSeverity    Severity // bar color: low ≤50, medium ≤75, high ≤90, critical >90
    ResetRelative   string   // "in 3h 17m" / "" if unknown
    ResetAbsolute   string   // "Thu 30 Apr 20:10 CEST" / ""
    ResetUnix       int64    // 0 if unknown
}

type ReportView struct {
    Provider string       // "Claude Code"
    Source   string
    Note     string
    Windows  []WindowView
}

type SummaryCell struct {
    Present         bool
    UsedPercent     float64
    ExpectedPercent float64
    Severity        Severity // blended (used + pace), mirrors summaryCellSeverity
    Text            string   // "21.0% used / 40.0% exp"
}

type SummaryRow struct {
    Provider   string // short name: "Claude", "Kimi", ...
    FiveHour   SummaryCell
    WeekGlobal SummaryCell
}

type View struct {
    Reports   []ReportView
    Summary   []SummaryRow
    Available bool // len(Reports) > 0
}

func BuildView(reports []Report, now time.Time) View
```

`Severity` is a string so it serializes cleanly to JSON and both UIs switch on
it. Two severity scales: `UsedSeverity` (per-window bar, four levels) and the
summary `Severity` (blended used+pace, mirrors the CLI summary table).

**Tests:** table-driven `view_test.go` with synthetic `Report`s and a fixed
`now`, asserting `ExpectedPercent`, `PaceLabel`, both severities, summary cell
text/presence, and canonical ordering.

### Layer 2 — TUI (`internal/ui`)

**Theme:** add `Danger lipgloss.Color` (red) to `Theme`, set in `theme_dark.go`
(`#EF4444`-ish) and `theme_light.go`. Color mapping: green = `Accent`, blue =
`Primary`, orange = `Warning`, red = `Danger`, muted = `Muted`.

**Model fields (`app.go`):**

```go
limitsOpen    bool
limitsTab     int  // 0 = summary, 1 = detailed
limitsLoading bool
limitsView    limits.View
limitsScroll  int  // detailed-tab scroll offset
```

**Message + command:**

```go
type limitsLoadedMsg struct{ view limits.View }

func loadLimitsCmd() tea.Cmd // ctx 30s → limits.FetchAll → BuildView(now) → limitsLoadedMsg
```

**Key handling (`Update`):**

- Top-level (no search/rename/picker/flash active): `l` → set
  `limitsOpen=true, limitsLoading=true, limitsTab=0, limitsScroll=0`; return
  `loadLimitsCmd()`.
- When `limitsOpen`, intercept keys early (like the editor picker):
  - `l` / `esc` / `q` → close: `limitsOpen=false`, clear `limitsView` (so a
    re-open re-fetches).
  - `tab` / `left` / `right` → toggle `limitsTab` (reset `limitsScroll` to 0).
  - `up`/`k`, `down`/`j`, `pgup`, `pgdn` → adjust `limitsScroll` (Detailed tab
    only), clamped to content height.
- `limitsLoadedMsg` is applied only if `limitsOpen` is still true (guards
  against a result arriving after the user closed the modal).

**Rendering (`internal/ui/limits.go`, new):** pure functions
`renderLimitsModal(view, theme, loading, tab, scroll, w, h) string` plus
`renderLimitsSummary` / `renderLimitsDetailed` helpers. The modal uses the same
`lipgloss.Place` + rounded-border + `ModalBg`/`OverlayBg` pattern as the
existing overlays. Layout:

```
╭─ Limits ───────────────────────────────╮
│  [ Summary ]  Detailed                  │   ← tab bar, active tab highlighted
│                                         │
│  <summary table | detailed scroll body> │
│                                         │
│  tab switch · j/k scroll · esc close    │   ← hint line
╰─────────────────────────────────────────╯
```

Width = `min(m.width-4, 80)`. The Detailed body is clipped to the modal's inner
height using `limitsScroll`, with a "↓ N more" affordance when content
overflows. Loading state replaces the body with a centered "Loading limits…".
Empty state (`!view.Available`) shows "No supported agents detected."

The Detailed tab renders, per provider, the same four facts as the CLI detailed
view (Used / Elapsed-as-Expected / Resets / Pace) but with Theme colors and
themed bars. Summary renders a compact two-column table (5h, Week/Global) using
`SummaryRow`.

**Help bar:** add `l limits` to `renderHelp`.

**Tests (`app_test.go` / new):** render functions are pure — assert the modal
string contains expected substrings for loading, populated, and empty `View`s
without panicking across small/large widths. Model test: `l` opens + sets
loading; `esc` closes and clears `limitsView`.

### Layer 3 — GUI (`internal/tray` + `frontend`)

**Service (`internal/tray/service.go`):**

```go
// GetLimits fetches all supported providers and returns the computed view.
// Called fresh each time the GUI opens the limits page.
func (s *SessionService) GetLimits() limits.View {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    return limits.BuildView(limits.FetchAll(ctx), time.Now())
}
```

Regenerate bindings with `make bindings` (wails3) — this adds `GetLimits` and
the `View`/`ReportView`/`WindowView`/`SummaryRow`/`SummaryCell` types under
`frontend/src/bindings/...`.

**App.svelte:**

- `let showLimits = $state(false)`.
- Header: a **limits** button (in the `no-drag` controls cluster) that toggles
  `showLimits`.
- `handleKeydown`: `l` toggles `showLimits` (when not `searching`). `Escape`:
  if `showLimits`, close it (checked before the existing detail-close branch).
- Content area: when `showLimits`, render `<LimitsPage />` in place of the
  list/detail split (full page).
- The limits search/filter/detach shortcuts remain inert while the page is open
  (the page owns the view).

**`frontend/src/lib/LimitsPage.svelte` (new):**

- On mount, calls `SessionService.GetLimits()`; shows a loading spinner until it
  resolves. A result is applied only if the component is still mounted (re-entry
  always re-fetches because the component remounts each time `showLimits`
  flips true).
- Two tabs (Summary / Detailed) mirroring the TUI. Tailwind classes consistent
  with the existing app (`bg-surface`, `text-accent`, `text-subtext`,
  `border-border`). Severity → color classes (green/blue/orange/red) via a small
  `severityClass(sev)` map.
- Summary: a table with Agent / 5h / Week-or-Global columns, cells colored by
  `SummaryCell.Severity`, "--" when not present.
- Detailed: one card per provider, each window showing Used (colored bar),
  Expected, Resets (relative + absolute), and Pace label.
- Empty state: "No supported agents detected."

**Footer:** add an `l limits` hint to the existing shortcut row.

## Data flow

1. User presses `l` (or clicks the header button / triggers the modal).
2. UI sets a loading state and kicks off an async fetch.
3. `limits.FetchAll(ctx)` queries the 5 providers concurrently; missing/errored
   agents are dropped.
4. `limits.BuildView(reports, now)` computes the style-free `View`.
5. TUI renders it with Theme colors; GUI renders it from the JSON binding.
6. Closing the section discards the `View`; re-opening repeats from step 2.

## Error handling

- Network/transient per-agent failures: the agent is omitted (matches the
  "omit silently" decision). No error toast.
- Zero reports: both surfaces show "No supported agents detected."
- Late results (arriving after the user closed the section): discarded via the
  still-open guard (TUI) / still-mounted guard (GUI).
- `ctx` timeout is 30s, same as the CLI.

## Testing strategy

- **limits:** unit-test `BuildView` (deterministic `now`, synthetic reports).
  `FetchAll` is network-bound and not unit-tested directly; its per-agent
  fetchers already have tests.
- **TUI:** pure-render-function tests + a Model open/close test.
- **GUI:** `GetLimits` is a thin wrapper (covered by `BuildView` tests);
  Svelte rendering verified via `make build` and manual run.

## Out of scope (YAGNI)

- Auto-refresh / polling of limits.
- Mouse-wheel scrolling inside the TUI modal (keyboard only for now).
- Refactoring the existing CLI renderers to use the new `View` (left as-is to
  avoid risk; mild, isolated duplication of formatting strings only).
- Per-agent error surfacing in the UI.
