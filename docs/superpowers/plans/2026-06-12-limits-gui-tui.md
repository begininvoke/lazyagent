# Limits in the GUI and TUI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface the existing `lazyagent limits` data inside the TUI (centered modal, Summary/Detailed tabs) and the GUI (full page, header button + `l` shortcut), reading limits on entry only.

**Architecture:** A new UI-agnostic computed view-model (`internal/limits/view.go`) does all the math once (used %, expected %, pace, severity, reset strings) and exposes a concurrent `FetchAll`. The TUI renders that `View` with its own Theme colors; the GUI consumes it as JSON via a new Wails `GetLimits` binding and renders it in Svelte. Both fetch fresh on entry and discard on exit.

**Tech Stack:** Go, bubbletea/lipgloss (TUI), Wails v3 + Svelte 5 + Tailwind (GUI).

---

## File Structure

- **Create** `internal/limits/view.go` — `Severity`, `WindowView`, `ReportView`, `SummaryCell`, `SummaryRow`, `View`, `BuildView`, `FetchAll`.
- **Create** `internal/limits/view_test.go` — tests for `BuildView`.
- **Modify** `internal/ui/theme.go`, `internal/ui/theme_dark.go`, `internal/ui/theme_light.go` — add `Danger` color.
- **Create** `internal/ui/limits.go` — TUI render functions for the limits modal.
- **Create** `internal/ui/limits_test.go` — render-function + open/close tests.
- **Modify** `internal/ui/app.go` — Model fields, keymap, msg, command, key handling, View() overlay, help hint.
- **Modify** `internal/tray/service.go` — `GetLimits` service method.
- **Create** `frontend/src/lib/LimitsPage.svelte` — GUI limits page.
- **Modify** `frontend/src/App.svelte` — header button, `l`/ESC handling, page render, footer hint.
- **Regenerate** `frontend/src/bindings/...` via `make bindings`.

---

## Task 1: limits view-model types + `BuildView`

**Files:**
- Create: `internal/limits/view.go`
- Test: `internal/limits/view_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/limits/view_test.go`:

```go
package limits

import (
	"testing"
	"time"
)

func TestBuildView(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	reports := []Report{
		{
			Provider: "Claude Code",
			Source:   "source note",
			Note:     "disclaimer",
			Windows: []Window{
				// 5h window: 50% used, reset in 2.5h => 50% elapsed => on track.
				{Label: "5-hour", WindowMinutes: 300, UsedPercent: 50, ResetsAt: now.Add(150 * time.Minute)},
				// 7d window: 95% used => danger severity.
				{Label: "7-day", WindowMinutes: 7 * 24 * 60, UsedPercent: 95, ResetsAt: now.Add(24 * time.Hour)},
			},
		},
	}

	v := BuildView(reports, now)

	if !v.Available {
		t.Fatal("View.Available = false, want true")
	}
	if len(v.Reports) != 1 || len(v.Summary) != 1 {
		t.Fatalf("Reports=%d Summary=%d, want 1/1", len(v.Reports), len(v.Summary))
	}

	fiveH := v.Reports[0].Windows[0]
	if fiveH.ExpectedPercent < 49 || fiveH.ExpectedPercent > 51 {
		t.Errorf("5h ExpectedPercent = %.1f, want ~50", fiveH.ExpectedPercent)
	}
	if !fiveH.PaceKnown || fiveH.PaceLabel != "on track" {
		t.Errorf("5h pace = %q known=%v, want 'on track' true", fiveH.PaceLabel, fiveH.PaceKnown)
	}
	if fiveH.UsedSeverity != SevInfo {
		t.Errorf("5h UsedSeverity = %q, want %q", fiveH.UsedSeverity, SevInfo)
	}
	if fiveH.ResetRelative == "" || fiveH.ResetUnix == 0 {
		t.Errorf("5h reset not populated: rel=%q unix=%d", fiveH.ResetRelative, fiveH.ResetUnix)
	}

	sevenD := v.Reports[0].Windows[1]
	if sevenD.UsedSeverity != SevDanger {
		t.Errorf("7d UsedSeverity = %q, want %q", sevenD.UsedSeverity, SevDanger)
	}

	row := v.Summary[0]
	if row.Provider != "Claude" {
		t.Errorf("summary provider = %q, want Claude", row.Provider)
	}
	if !row.FiveHour.Present || !row.WeekGlobal.Present {
		t.Errorf("summary cells not present: 5h=%v week=%v", row.FiveHour.Present, row.WeekGlobal.Present)
	}
	if row.WeekGlobal.Severity != SevDanger {
		t.Errorf("week severity = %q, want %q", row.WeekGlobal.Severity, SevDanger)
	}
}

func TestBuildViewEmpty(t *testing.T) {
	v := BuildView(nil, time.Now())
	if v.Available {
		t.Fatal("empty View.Available = true, want false")
	}
	if len(v.Reports) != 0 || len(v.Summary) != 0 {
		t.Fatalf("empty view not empty: reports=%d summary=%d", len(v.Reports), len(v.Summary))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/limits/ -run TestBuildView -v`
Expected: FAIL — `undefined: BuildView` / `undefined: SevInfo` (compile error).

- [ ] **Step 3: Write minimal implementation**

Create `internal/limits/view.go`:

```go
package limits

import (
	"context"
	"sync"
	"time"
)

// Severity is a style-free classification both UIs map to colors.
type Severity string

const (
	SevDefault Severity = "default" // neutral text
	SevOK      Severity = "ok"      // green
	SevInfo    Severity = "info"    // blue/primary
	SevWarn    Severity = "warn"    // orange
	SevDanger  Severity = "danger"  // red
)

// WindowView is a fully-computed, render-ready window (no styling, no time.Time).
type WindowView struct {
	Label           string
	UsedPercent     float64
	ExpectedPercent float64 // linear pace for elapsed window time
	PaceLabel       string  // "underutilizing" | "on track" | "overutilizing"
	PaceRatio       float64
	PaceKnown       bool
	UsedSeverity    Severity
	ResetRelative   string // "in 3h 17m" / "" if unknown
	ResetAbsolute   string // "Thu 30 Apr 20:10 CEST" / ""
	ResetUnix       int64  // 0 if unknown
}

type ReportView struct {
	Provider string
	Source   string
	Note     string
	Windows  []WindowView
}

type SummaryCell struct {
	Present         bool
	UsedPercent     float64
	ExpectedPercent float64
	Severity        Severity
	Text            string // "21.0% used / 40.0% exp" or "--"
}

type SummaryRow struct {
	Provider   string // short name: "Claude", "Kimi", ...
	FiveHour   SummaryCell
	WeekGlobal SummaryCell
}

// View is the complete, UI-agnostic limits payload consumed by the TUI and GUI.
type View struct {
	Reports   []ReportView
	Summary   []SummaryRow
	Available bool
}

// FetchAll queries every supported provider concurrently using the same
// per-agent fetchers as the CLI. Not-installed, unavailable, and transiently
// failing agents are omitted. Reports keep canonical order: claude, codex,
// grok, kimi, cursor.
func FetchAll(ctx context.Context) []Report {
	agents, _ := resolveAgents("all")
	results := make([]Report, len(agents))
	found := make([]bool, len(agents))
	var wg sync.WaitGroup
	for i, a := range agents {
		wg.Add(1)
		go func(i int, a string) {
			defer wg.Done()
			report, err := fetchReport(ctx, a)
			if err != nil {
				return
			}
			results[i] = report
			found[i] = true
		}(i, a)
	}
	wg.Wait()

	var out []Report
	for i := range agents {
		if found[i] {
			out = append(out, results[i])
		}
	}
	return out
}

// BuildView computes the render-ready view from raw reports. now is explicit
// for testability.
func BuildView(reports []Report, now time.Time) View {
	v := View{Available: len(reports) > 0}
	for _, r := range reports {
		rv := ReportView{Provider: r.Provider, Source: r.Source, Note: r.Note}
		for _, w := range r.Windows {
			rv.Windows = append(rv.Windows, buildWindowView(w, now))
		}
		v.Reports = append(v.Reports, rv)
		v.Summary = append(v.Summary, buildSummaryRow(r, now))
	}
	return v
}

func buildWindowView(w Window, now time.Time) WindowView {
	wp := paceForWindow(w, now)
	wv := WindowView{
		Label:           w.Label,
		UsedPercent:     w.UsedPercent,
		ExpectedPercent: wp.elapsedPercent,
		PaceRatio:       wp.ratio,
		PaceKnown:       wp.pace != paceUnknown,
		UsedSeverity:    usedSeverity(w.UsedPercent),
	}
	if wv.PaceKnown {
		wv.PaceLabel = paceLabel(wp.pace)
	}
	if !w.ResetsAt.IsZero() {
		wv.ResetUnix = w.ResetsAt.Unix()
		wv.ResetAbsolute = w.ResetsAt.Local().Format("Mon 2 Jan 15:04 MST")
		if rem := w.ResetsAt.Sub(now); rem > 0 {
			wv.ResetRelative = "in " + humanDuration(rem)
		}
	}
	return wv
}

func buildSummaryRow(r Report, now time.Time) SummaryRow {
	return SummaryRow{
		Provider:   summaryProviderName(r.Provider),
		FiveHour:   buildSummaryCell(r, summaryFiveHour, now),
		WeekGlobal: buildSummaryCell(r, summaryWeekGlobal, now),
	}
}

func buildSummaryCell(r Report, kind summaryWindowKind, now time.Time) SummaryCell {
	text, used, p, ok := summaryCellContent(r, kind, now)
	if !ok {
		return SummaryCell{Present: false, Text: "--", Severity: SevDefault}
	}
	w, _ := summaryWindow(r.Windows, kind)
	wp := paceForWindow(w, now)
	return SummaryCell{
		Present:         true,
		UsedPercent:     used,
		ExpectedPercent: wp.elapsedPercent,
		Severity:        summarySeverityToView(summaryCellSeverity(used, p)),
		Text:            text,
	}
}

// usedSeverity mirrors the CLI detailed bar thresholds.
func usedSeverity(p float64) Severity {
	switch {
	case p >= 90:
		return SevDanger
	case p >= 75:
		return SevWarn
	case p >= 50:
		return SevInfo
	default:
		return SevOK
	}
}

func summarySeverityToView(s summarySeverity) Severity {
	switch s {
	case sevUnder:
		return SevOK
	case sevWarn:
		return SevWarn
	case sevDanger:
		return SevDanger
	default:
		return SevDefault
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/limits/ -run TestBuildView -v`
Expected: PASS (both `TestBuildView` and `TestBuildViewEmpty`).

- [ ] **Step 5: Commit**

```bash
git add internal/limits/view.go internal/limits/view_test.go
git commit -m "feat(limits): add UI-agnostic view-model and concurrent FetchAll"
```

---

## Task 2: TUI Theme gains a `Danger` color

**Files:**
- Modify: `internal/ui/theme.go`
- Modify: `internal/ui/theme_dark.go`
- Modify: `internal/ui/theme_light.go`

- [ ] **Step 1: Add the field**

In `internal/ui/theme.go`, add `Danger` right after `Warning` in the `Theme` struct:

```go
	Primary     lipgloss.Color
	Accent      lipgloss.Color
	Warning     lipgloss.Color
	Danger      lipgloss.Color
	Muted       lipgloss.Color
```

- [ ] **Step 2: Set it in the dark theme**

In `internal/ui/theme_dark.go`, add after the `Warning:` line:

```go
		Warning:     lipgloss.Color("#F59E0B"),
		Danger:      lipgloss.Color("#EF4444"),
```

- [ ] **Step 3: Set it in the light theme**

In `internal/ui/theme_light.go`, add after the `Warning:` line:

```go
		Warning:     lipgloss.Color("#D97706"),
		Danger:      lipgloss.Color("#DC2626"),
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/ui/`
Expected: no output (success).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/theme.go internal/ui/theme_dark.go internal/ui/theme_light.go
git commit -m "feat(ui): add Danger color to TUI theme"
```

---

## Task 3: TUI limits render functions

**Files:**
- Create: `internal/ui/limits.go`
- Test: `internal/ui/limits_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ui/limits_test.go`:

```go
package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/limits"
)

func limitsTestModel(tab int, loading bool, view limits.View) Model {
	theme := DarkTheme()
	return Model{
		theme:         theme,
		width:         100,
		height:        40,
		limitsOpen:    true,
		limitsTab:     tab,
		limitsLoading: loading,
		limitsView:    view,
	}
}

func sampleView() limits.View {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	return limits.BuildView([]limits.Report{
		{
			Provider: "Claude Code",
			Windows: []limits.Window{
				{Label: "5-hour", WindowMinutes: 300, UsedPercent: 50, ResetsAt: now.Add(150 * time.Minute)},
			},
		},
	}, now)
}

func TestRenderLimitsModal_Loading(t *testing.T) {
	m := limitsTestModel(0, true, limits.View{})
	out := m.renderLimitsModal()
	if !strings.Contains(out, "Loading limits") {
		t.Fatalf("loading modal missing spinner text: %q", out)
	}
}

func TestRenderLimitsModal_Empty(t *testing.T) {
	m := limitsTestModel(0, false, limits.View{Available: false})
	out := m.renderLimitsModal()
	if !strings.Contains(out, "No supported agents") {
		t.Fatalf("empty modal missing empty text: %q", out)
	}
}

func TestRenderLimitsModal_SummaryAndDetailed(t *testing.T) {
	v := sampleView()
	summary := limitsTestModel(0, false, v).renderLimitsModal()
	if !strings.Contains(summary, "Summary") || !strings.Contains(summary, "Claude") {
		t.Fatalf("summary modal missing content: %q", summary)
	}
	detailed := limitsTestModel(1, false, v).renderLimitsModal()
	if !strings.Contains(detailed, "5-hour window") {
		t.Fatalf("detailed modal missing window label: %q", detailed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestRenderLimitsModal -v`
Expected: FAIL — `renderLimitsModal` undefined, and `limitsOpen`/`limitsTab`/`limitsLoading`/`limitsView` are unknown Model fields (compile error). They are added in Task 4; this test compiles only after Task 4. **Proceed to Step 3 to write `limits.go`, then this test will be exercised at the end of Task 4.**

> Note: Tasks 3 and 4 both touch the Model. Write `limits.go` now (Step 3); the test from Step 1 passes once Task 4 adds the fields. Commit `limits.go` together with Task 4 if `go build` fails standalone.

- [ ] **Step 3: Write the render functions**

Create `internal/ui/limits.go`:

```go
package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/illegalstudio/lazyagent/internal/limits"
)

const (
	limitsTabSummary  = 0
	limitsTabDetailed = 1
)

// limitsSeverityColor maps a computed severity to a TUI theme color.
func (m Model) limitsSeverityColor(sev limits.Severity) lipgloss.Color {
	switch sev {
	case limits.SevOK:
		return m.theme.Accent
	case limits.SevInfo:
		return m.theme.Primary
	case limits.SevWarn:
		return m.theme.Warning
	case limits.SevDanger:
		return m.theme.Danger
	default:
		return m.theme.Text
	}
}

// renderLimitsModal renders the centered limits overlay (tabs + body + hint).
func (m Model) renderLimitsModal() string {
	width := m.width - 4
	if width > 80 {
		width = 80
	}
	if width < 24 {
		width = 24
	}

	maxBodyH := m.height - 10
	if maxBodyH < 3 {
		maxBodyH = 3
	}

	var bodyLines []string
	switch {
	case m.limitsLoading:
		bodyLines = []string{"", lipgloss.NewStyle().Foreground(m.theme.Subtext).Render("  Loading limits…")}
	case !m.limitsView.Available:
		bodyLines = []string{"", lipgloss.NewStyle().Foreground(m.theme.Subtext).Render("  No supported agents detected.")}
	case m.limitsTab == limitsTabSummary:
		bodyLines = m.renderLimitsSummaryLines()
	default:
		bodyLines = m.renderLimitsDetailedLines()
	}

	scroll := 0
	if m.limitsTab == limitsTabDetailed {
		scroll = m.limitsScroll
	}
	visible, moreBelow := windowLines(bodyLines, scroll, maxBodyH)
	body := strings.Join(visible, "\n")

	title := lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true).Render("Limits")
	tabs := m.renderLimitsTabs()
	hint := m.renderLimitsHint(moreBelow)
	content := title + "\n" + tabs + "\n\n" + body + "\n\n" + hint

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.BorderFocus).
		Background(m.theme.ModalBg).
		Foreground(m.theme.Text).
		Padding(1, 2).
		Width(width).
		Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(m.theme.OverlayBg),
	)
}

func (m Model) renderLimitsTabs() string {
	mk := func(label string, active bool) string {
		if active {
			return lipgloss.NewStyle().Foreground(m.theme.Text).Background(m.theme.SelectionBg).Bold(true).Padding(0, 1).Render(label)
		}
		return lipgloss.NewStyle().Foreground(m.theme.Subtext).Padding(0, 1).Render(label)
	}
	return mk("Summary", m.limitsTab == limitsTabSummary) + "  " + mk("Detailed", m.limitsTab == limitsTabDetailed)
}

func (m Model) renderLimitsHint(moreBelow bool) string {
	h := "tab/←→ switch · j/k scroll · l/esc close"
	if moreBelow {
		h = "↓ more · " + h
	}
	return lipgloss.NewStyle().Foreground(m.theme.Muted).Render(h)
}

func (m Model) renderLimitsSummaryLines() []string {
	lines := []string{
		lipgloss.NewStyle().Foreground(m.theme.Subtext).Bold(true).Render(
			fmt.Sprintf("  %-8s  %-24s  %-24s", "Agent", "5h", "Week / Global")),
	}
	for _, row := range m.limitsView.Summary {
		prov := lipgloss.NewStyle().Foreground(m.theme.Text).Render(fmt.Sprintf("%-8s", row.Provider))
		lines = append(lines, "  "+prov+"  "+m.summaryCellText(row.FiveHour)+"  "+m.summaryCellText(row.WeekGlobal))
	}
	return lines
}

func (m Model) summaryCellText(c limits.SummaryCell) string {
	padded := fmt.Sprintf("%-24s", c.Text)
	return lipgloss.NewStyle().Foreground(m.limitsSeverityColor(c.Severity)).Render(padded)
}

func (m Model) renderLimitsDetailedLines() []string {
	const barW = 16
	var lines []string
	for i, r := range m.limitsView.Reports {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(m.theme.Primary).Bold(true).Render(r.Provider))
		for _, w := range r.Windows {
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true).Render(w.Label+" window"))
			lines = append(lines, fmt.Sprintf("    Used:     %5.1f%%  %s",
				w.UsedPercent, m.limitsBar(w.UsedPercent, barW, m.limitsSeverityColor(w.UsedSeverity))))
			lines = append(lines, fmt.Sprintf("    Expected: %5.1f%%  %s",
				w.ExpectedPercent, m.limitsBar(w.ExpectedPercent, barW, m.theme.Subtext)))

			reset := "reset time unknown"
			if w.ResetRelative != "" {
				reset = w.ResetRelative + " (" + w.ResetAbsolute + ")"
			} else if w.ResetAbsolute != "" {
				reset = w.ResetAbsolute
			}
			lines = append(lines, "    Resets:   "+lipgloss.NewStyle().Foreground(m.theme.Subtext).Render(reset))

			pace := "— (window just reset)"
			paceColor := m.theme.Muted
			if w.PaceKnown {
				pace = fmt.Sprintf("%s (%.2f× of expected %.1f%%)", w.PaceLabel, w.PaceRatio, w.ExpectedPercent)
				switch w.PaceLabel {
				case "overutilizing":
					paceColor = m.theme.Danger
				case "on track":
					paceColor = m.theme.Accent
				default:
					paceColor = m.theme.Subtext
				}
			}
			lines = append(lines, "    Pace:     "+lipgloss.NewStyle().Foreground(paceColor).Render(pace))
		}
		if r.Source != "" {
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(m.theme.Muted).Render(r.Source))
		}
		if r.Note != "" {
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(m.theme.Muted).Render(r.Note))
		}
	}
	return lines
}

func (m Model) limitsBar(percent float64, w int, color lipgloss.Color) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	n := int(percent/100*float64(w) + 0.5)
	if n > w {
		n = w
	}
	filled := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", n))
	empty := lipgloss.NewStyle().Foreground(m.theme.Muted).Render(strings.Repeat("░", w-n))
	return filled + empty
}

// windowLines returns the slice of lines visible at the given scroll offset and
// whether more lines exist below the window.
func windowLines(lines []string, offset, height int) (visible []string, moreBelow bool) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(lines) {
		offset = len(lines)
	}
	end := offset + height
	if end >= len(lines) {
		return lines[offset:], false
	}
	return lines[offset:end], true
}
```

- [ ] **Step 4: Defer running the test until Task 4**

`internal/ui/limits.go` references Model fields added in Task 4. Do **not** run the test yet. Continue to Task 4.

- [ ] **Step 5: (No commit yet — committed at end of Task 4.)**

---

## Task 4: TUI Model wiring (open/close/tabs/scroll/load)

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add imports**

In `internal/ui/app.go`, add `"context"` to the standard-library import group and the limits package to the project imports:

```go
	"context"
```

and (with the other `github.com/illegalstudio/lazyagent/internal/...` imports):

```go
	"github.com/illegalstudio/lazyagent/internal/limits"
```

- [ ] **Step 2: Add Model fields**

In the `Model` struct (after the `renameSessionID string` field), add:

```go
	// Limits modal
	limitsOpen    bool
	limitsTab     int // 0 = summary, 1 = detailed
	limitsLoading bool
	limitsView    limits.View
	limitsScroll  int
```

- [ ] **Step 3: Add keymap entry**

In the `keyMap` struct add `Limits key.Binding`, and in the `keys` var add:

```go
	Limits: key.NewBinding(key.WithKeys("l")),
```

- [ ] **Step 4: Add the message type and load command**

Near `editorFinishedMsg` (top of file), add:

```go
type limitsLoadedMsg struct{ view limits.View }

func loadLimitsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return limitsLoadedMsg{view: limits.BuildView(limits.FetchAll(ctx), time.Now())}
	}
}
```

- [ ] **Step 5: Handle the loaded message**

In `Update`, in the top-level `switch msg := msg.(type)` (alongside the other custom message cases, e.g. near where `updateAvailableMsg`/`tickMsg` are handled), add:

```go
	case limitsLoadedMsg:
		if m.limitsOpen {
			m.limitsView = msg.view
			m.limitsLoading = false
		}
		return m, nil
```

- [ ] **Step 6: Intercept keys while the modal is open**

In `Update`, inside `case tea.KeyMsg:`, immediately **after** the flash-dismiss block (`if m.flashMsg != "" { ... }`) and **before** the editor-picker block, add:

```go
		// Limits modal: intercept keys while open.
		if m.limitsOpen {
			switch msg.String() {
			case "l", "esc", "q":
				m.limitsOpen = false
				m.limitsLoading = false
				m.limitsScroll = 0
				m.limitsView = limits.View{}
			case "tab", "left", "right":
				m.limitsTab = (m.limitsTab + 1) % 2
				m.limitsScroll = 0
			case "down", "j":
				m.limitsScroll++
			case "up", "k":
				if m.limitsScroll > 0 {
					m.limitsScroll--
				}
			case "pgdown":
				m.limitsScroll += 5
			case "pgup":
				m.limitsScroll -= 5
				if m.limitsScroll < 0 {
					m.limitsScroll = 0
				}
			}
			return m, nil
		}
```

- [ ] **Step 7: Add the open handler**

In the main key `switch { case key.Matches(...) }` block (the one starting with `case key.Matches(msg, keys.Quit):`), add a new case — place it before `keys.Quit` so `l` is handled before any single-letter fallthrough:

```go
		case key.Matches(msg, keys.Limits):
			m.limitsOpen = true
			m.limitsLoading = true
			m.limitsTab = 0
			m.limitsScroll = 0
			m.limitsView = limits.View{}
			return m, loadLimitsCmd()
```

- [ ] **Step 8: Render the overlay in View()**

In `View()`, after the flash-message overlay block (`if m.flashMsg != "" { ... }`) and before `return out`, add:

```go
	// Overlay limits modal.
	if m.limitsOpen {
		out = m.renderLimitsModal()
	}
```

- [ ] **Step 9: Add the help hint**

In `renderHelp()`, in the common (non-search) parts — find the slice that contains `m.sty.helpKey.Render("f")+m.sty.help.Render(" filter")` — add an `l limits` entry to it:

```go
		m.sty.helpKey.Render("l")+m.sty.help.Render(" limits"),
```

- [ ] **Step 10: Build and run all TUI + limits tests**

Run: `go build ./... && go test ./internal/ui/ ./internal/limits/ -v`
Expected: PASS, including `TestRenderLimitsModal_Loading`, `TestRenderLimitsModal_Empty`, `TestRenderLimitsModal_SummaryAndDetailed`.

- [ ] **Step 11: Add an open/close model test**

Append to `internal/ui/limits_test.go`:

```go
func TestLimitsOpenClose(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 40

	opened, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	om := opened.(Model)
	if !om.limitsOpen || !om.limitsLoading {
		t.Fatalf("after 'l': open=%v loading=%v, want true/true", om.limitsOpen, om.limitsLoading)
	}
	if cmd == nil {
		t.Fatal("expected a load command after opening limits")
	}

	closed, _ := om.Update(tea.KeyMsg{Type: tea.KeyEsc})
	cm := closed.(Model)
	if cm.limitsOpen {
		t.Fatal("after esc: limitsOpen = true, want false")
	}
	if cm.limitsView.Available {
		t.Fatal("after close: view not cleared")
	}
}
```

Add `tea "github.com/charmbracelet/bubbletea"` to the test imports if not already present.

- [ ] **Step 12: Run the new test**

Run: `go test ./internal/ui/ -run TestLimitsOpenClose -v`
Expected: PASS.

- [ ] **Step 13: Commit**

```bash
git add internal/ui/app.go internal/ui/limits.go internal/ui/limits_test.go
git commit -m "feat(tui): add limits modal with summary/detailed tabs"
```

---

## Task 5: GUI service method `GetLimits`

**Files:**
- Modify: `internal/tray/service.go`

- [ ] **Step 1: Add the import**

In `internal/tray/service.go`, add to the project imports:

```go
	"github.com/illegalstudio/lazyagent/internal/limits"
```

(`context` and `time` are already imported.)

- [ ] **Step 2: Add the method**

After `GetWindowMinutes` (or any logical spot among the exported getters), add:

```go
// GetLimits fetches all supported providers and returns the computed limits
// view. It is called fresh each time the GUI opens the limits page; it does not
// poll. Missing/errored agents are omitted.
func (s *SessionService) GetLimits() limits.View {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return limits.BuildView(limits.FetchAll(ctx), time.Now())
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build -tags '!notray' ./internal/tray/ || go build ./internal/tray/`
Expected: success (no output).

- [ ] **Step 4: Regenerate Wails bindings**

Run: `make bindings`
Expected: regenerates `frontend/src/bindings/...`; a new `internal/limits/models.ts` (or equivalent) appears and `sessionservice.ts` gains a `GetLimits` export. If `wails3` is not installed, install it: `go install github.com/wailsapp/wails/v3/cmd/wails3@latest` and retry.

- [ ] **Step 5: Commit**

```bash
git add internal/tray/service.go frontend/src/bindings
git commit -m "feat(gui): expose GetLimits service binding"
```

---

## Task 6: GUI LimitsPage component

**Files:**
- Create: `frontend/src/lib/LimitsPage.svelte`

- [ ] **Step 1: Create the component**

Create `frontend/src/lib/LimitsPage.svelte`:

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import * as SessionService from "../bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";

  let loading = $state(true);
  let view = $state<any>(null);
  let tab = $state<"summary" | "detailed">("summary");
  let mounted = true;

  onMount(() => {
    SessionService.GetLimits()
      .then((v) => {
        if (mounted) {
          view = v;
          loading = false;
        }
      })
      .catch(() => {
        if (mounted) {
          view = { Reports: [], Summary: [], Available: false };
          loading = false;
        }
      });
    return () => {
      mounted = false;
    };
  });

  function sevText(sev: string): string {
    switch (sev) {
      case "ok": return "text-green-500";
      case "info": return "text-blue-400";
      case "warn": return "text-amber-500";
      case "danger": return "text-red-500";
      default: return "text-text";
    }
  }

  function sevBar(sev: string): string {
    switch (sev) {
      case "ok": return "bg-green-500";
      case "info": return "bg-blue-400";
      case "warn": return "bg-amber-500";
      case "danger": return "bg-red-500";
      default: return "bg-subtext";
    }
  }
</script>

<div class="flex flex-col h-full bg-surface">
  <div class="flex items-center gap-2 px-3 py-2 border-b border-border">
    <button
      class="rounded px-2 py-0.5 text-[12px] font-medium {tab === 'summary' ? 'text-accent bg-accent/10' : 'text-subtext hover:text-text'}"
      onclick={() => (tab = "summary")}
    >Summary</button>
    <button
      class="rounded px-2 py-0.5 text-[12px] font-medium {tab === 'detailed' ? 'text-accent bg-accent/10' : 'text-subtext hover:text-text'}"
      onclick={() => (tab = "detailed")}
    >Detailed</button>
    <span class="ml-auto text-[10px] text-subtext">esc / l to close</span>
  </div>

  <div class="flex-1 overflow-auto p-3">
    {#if loading}
      <div class="text-[13px] text-subtext">Loading limits…</div>
    {:else if !view || !view.Available}
      <div class="text-[13px] text-subtext">No supported agents detected.</div>
    {:else if tab === "summary"}
      <table class="w-full text-[12px]">
        <thead>
          <tr class="text-subtext text-left">
            <th class="font-medium py-1 pr-3">Agent</th>
            <th class="font-medium py-1 pr-3">5h</th>
            <th class="font-medium py-1">Week / Global</th>
          </tr>
        </thead>
        <tbody>
          {#each view.Summary as row}
            <tr class="border-t border-border/50">
              <td class="py-1 pr-3 text-text">{row.Provider}</td>
              <td class="py-1 pr-3 {sevText(row.FiveHour.Severity)}">{row.FiveHour.Text}</td>
              <td class="py-1 {sevText(row.WeekGlobal.Severity)}">{row.WeekGlobal.Text}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {:else}
      <div class="flex flex-col gap-4">
        {#each view.Reports as report}
          <div class="rounded border border-border p-2">
            <div class="text-[13px] font-bold text-accent mb-1">{report.Provider}</div>
            {#each report.Windows as w}
              <div class="mb-2">
                <div class="text-[12px] font-medium text-text">{w.Label} window</div>
                <div class="flex items-center gap-2 text-[11px] text-subtext">
                  <span class="w-16">Used {w.UsedPercent.toFixed(1)}%</span>
                  <div class="flex-1 h-1.5 rounded bg-border overflow-hidden">
                    <div class="h-full {sevBar(w.UsedSeverity)}" style="width: {Math.min(100, w.UsedPercent)}%"></div>
                  </div>
                </div>
                <div class="text-[11px] text-subtext">Expected {w.ExpectedPercent.toFixed(1)}%</div>
                {#if w.ResetRelative}
                  <div class="text-[11px] text-subtext">Resets {w.ResetRelative} ({w.ResetAbsolute})</div>
                {/if}
                {#if w.PaceKnown}
                  <div class="text-[11px] {w.PaceLabel === 'overutilizing' ? 'text-red-500' : w.PaceLabel === 'on track' ? 'text-green-500' : 'text-subtext'}">
                    {w.PaceLabel} ({w.PaceRatio.toFixed(2)}× of expected {w.ExpectedPercent.toFixed(1)}%)
                  </div>
                {/if}
              </div>
            {/each}
            {#if report.Source}<div class="text-[10px] text-subtext">{report.Source}</div>{/if}
            {#if report.Note}<div class="text-[10px] text-subtext">{report.Note}</div>{/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>
```

- [ ] **Step 2: Type-check the frontend**

Run: `cd frontend && npm run build`
Expected: build succeeds (Svelte + TS compile with no errors). If `GetLimits` is missing from the bindings, re-run Task 5 Step 4 (`make bindings`).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/LimitsPage.svelte
git commit -m "feat(gui): add LimitsPage component"
```

---

## Task 7: GUI App.svelte wiring (button, shortcut, ESC, footer)

**Files:**
- Modify: `frontend/src/App.svelte`

- [ ] **Step 1: Import the page and add state**

In the `<script>` block, add the import near the other lib imports:

```js
  import LimitsPage from "./lib/LimitsPage.svelte";
```

and add to the state declarations (near `let searching = $state(false);`):

```js
  let showLimits = $state(false);
```

- [ ] **Step 2: Handle `l` and ESC in `handleKeydown`**

In `handleKeydown`, after the `if (searching) { ... }` block, add an early ESC/`l` handler for the limits page (before the existing `if (e.key === "Escape")` detail handler):

```js
    if (showLimits) {
      if (e.key === "Escape" || e.key === "l" || e.key === "L") {
        e.preventDefault();
        showLimits = false;
      }
      return;
    }
```

Then add `l` as an opener alongside the other shortcuts (e.g. after the `else if (e.key === "/")` branch):

```js
    } else if (e.key === "l" || e.key === "L") {
      e.preventDefault();
      showLimits = true;
```

- [ ] **Step 3: Add the header button**

In the header's `no-drag` controls cluster (the `<div class="flex items-center gap-2 no-drag">` on the right), add a limits button as the first child:

```svelte
      <button
        class="rounded px-1.5 py-0.5 text-[11px] font-medium {showLimits ? 'text-accent bg-accent/10' : 'text-subtext hover:text-text'}"
        onclick={() => (showLimits = !showLimits)}
        title="Show limits (l)"
      >limits</button>
```

- [ ] **Step 4: Render the page in the content area**

Replace the content `<div class="flex-1 flex min-h-0">...</div>` block so the limits page takes over when open:

```svelte
  <!-- Content -->
  <div class="flex-1 flex min-h-0">
    {#if showLimits}
      <div class="flex-1 overflow-hidden">
        <LimitsPage />
      </div>
    {:else if showDetail}
      <div class="w-[45%] border-r border-border overflow-hidden">
        <SessionList />
      </div>
      <div class="flex-1 overflow-hidden">
        <SessionDetail />
      </div>
    {:else}
      <div class="flex-1 overflow-hidden">
        <SessionList />
      </div>
    {/if}
  </div>
```

- [ ] **Step 5: Add the footer hint**

In the footer shortcut row, add an `l` hint (e.g. after the `f filter` span):

```svelte
      <span><kbd class="text-text/60">l</kbd> limits</span>
```

- [ ] **Step 6: Build the frontend**

Run: `cd frontend && npm run build`
Expected: build succeeds.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/App.svelte
git commit -m "feat(gui): open limits page via header button and l shortcut"
```

---

## Task 8: Full build + docs note

**Files:**
- Modify: `docs/maintenance/limits.md`

- [ ] **Step 1: Full build**

Run: `make build`
Expected: frontend builds, bindings present, Go binary compiles (TUI + GUI).

- [ ] **Step 2: Run the whole Go test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 3: Add a docs note**

In `docs/maintenance/limits.md`, after the intro paragraph, add a short note that the same data is viewable interactively:

```markdown
The same limits are viewable interactively without leaving lazyagent: in the
**TUI** press `l` to open a centered modal with **Summary** and **Detailed**
tabs (the Detailed tab scrolls inside the modal); in the **GUI** click **limits**
in the header or press `l` to open the limits page (`l` or `Esc` closes it).
Both read limits on entry only — leave and re-enter to refresh.
```

- [ ] **Step 4: Commit**

```bash
git add docs/maintenance/limits.md
git commit -m "docs(limits): mention TUI modal and GUI page"
```

---

## Self-Review Notes

- **Spec coverage:** TUI modal + 2 tabs + scroll (Tasks 2-4); GUI header button + `l` toggle + ESC + new page (Tasks 5-7); read-on-entry / re-read-on-reentry (open handlers fetch fresh, close handlers clear state — Task 4 Steps 6-7, Task 6 onMount/unmount); async loading (loading flags — Task 4, Task 6); omit-missing-silently + empty message (`FetchAll` drops failures, `Available` flag + "No supported agents detected" — Tasks 1, 3, 6); TUI-native theme colors (`Danger` color + `limitsSeverityColor` — Tasks 2-3).
- **Type consistency:** `View`/`ReportView`/`WindowView`/`SummaryRow`/`SummaryCell`/`Severity` constants used identically across `view.go`, `limits.go`, `service.go`, and the Svelte field accesses (`Reports`, `Summary`, `Provider`, `Windows`, `UsedPercent`, `ExpectedPercent`, `UsedSeverity`, `PaceKnown`, `PaceLabel`, `PaceRatio`, `ResetRelative`, `ResetAbsolute`, `Text`, `Present`, `Severity`, `Available`). Severity values: `default`/`ok`/`info`/`warn`/`danger`.
- **Cross-task dependency:** Tasks 3 and 4 both touch the Model; `limits.go` is written in Task 3 but only builds/commits in Task 4 (noted in both tasks).
```
