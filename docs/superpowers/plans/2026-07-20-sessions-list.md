# `lazyagent sessions` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `lazyagent sessions` subcommand that lists every session recorded for the current directory (all agents) in an interactive bubbletea picker and reopens the selected one with the originating agent's CLI.

**Architecture:** New self-contained package `internal/sessions` following the existing subcommand pattern (`Run(args []string) int` registered in `main.go`). Discovery reuses `core.BuildProvider`; filtering/sorting are pure functions; the picker is a small bubbletea model whose `Update` is unit-testable; the resume exec happens after the tea program exits. A targeted refactor adds `core.ResumeArgv` as the single source of truth for executable resume commands (dedups `search/run.go`).

**Tech Stack:** Go 1.25, bubbletea + lipgloss (already vendored), stdlib `flag`/`encoding/json`/`path/filepath`.

**Spec:** `docs/superpowers/specs/2026-07-20-sessions-list-design.md`

## Global Constraints

- Executable resume set is exactly: claude, codex, amp, pi, kimi ("openable" ⇔ `ResumeArgv != nil`).
- Copyable set: any agent where `core.ResumeCommand != ""` (adds opencode, kilo, cursor); grok has neither.
- Sidechain sessions (`IsSidechain`) are always excluded from the listing.
- Sort: `LastActivity` descending.
- Exit codes: 0 success/quit/no-sessions, 1 runtime failure (discovery, exec, clipboard), 2 usage errors.
- JSON field names (verbatim): `agent`, `session_id`, `name`, `cwd`, `last_activity`, `messages`, `resume_command`. Empty list → `[]`.
- Picker renders to **stderr** (like `chatops.PickAgents`); JSON goes to **stdout**.
- All commit messages: no Co-Authored-By lines (user preference).
- Run package tests with `go test ./internal/<pkg>/`.

---

### Task 1: `core.ResumeArgv` + dedup `search`

**Files:**
- Modify: `internal/core/resume.go`
- Modify: `internal/search/run.go` (function `resumeCommand`, ~line 239)
- Test: `internal/core/resume_test.go`

**Interfaces:**
- Consumes: existing `core.ResumeCommand(agent, sessionID string) string` (display string, unchanged).
- Produces: `core.ResumeArgv(agent, sessionID string) []string` — executable argv for claude/codex/amp/pi/kimi, `nil` for every other agent or empty sessionID. Tasks 4 and 5 rely on this exact name and nil-semantics.

- [ ] **Step 1: Write the failing test**

Append to `internal/core/resume_test.go` (package `core`; add `"slices"` to imports):

```go
func TestResumeArgv(t *testing.T) {
	cases := []struct {
		agent string
		want  []string
	}{
		{"claude", []string{"claude", "--resume", "abc"}},
		{"codex", []string{"codex", "resume", "abc"}},
		{"amp", []string{"amp", "threads", "continue", "abc"}},
		{"pi", []string{"pi", "--session", "abc"}},
		{"kimi", []string{"kimi", "--resume", "abc"}},
		// Display-only agents: copyable via ResumeCommand but not executable.
		{"opencode", nil},
		{"kilo", nil},
		{"cursor", nil},
		{"grok", nil},
		{"unknown", nil},
	}
	for _, c := range cases {
		if got := ResumeArgv(c.agent, "abc"); !slices.Equal(got, c.want) {
			t.Errorf("ResumeArgv(%q) = %v, want %v", c.agent, got, c.want)
		}
	}
	if got := ResumeArgv("claude", ""); got != nil {
		t.Errorf("empty session ID: want nil, got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestResumeArgv -v`
Expected: FAIL — `undefined: ResumeArgv` (compile error).

- [ ] **Step 3: Write minimal implementation**

Append to `internal/core/resume.go`:

```go
// ResumeArgv returns the executable argv to resume a session, or nil when
// the agent has no resume command lazyagent is willing to exec (grok has
// none; opencode/kilo/cursor have a display string only — see ResumeCommand).
// "Openable" everywhere in the codebase means ResumeArgv != nil.
func ResumeArgv(agent, sessionID string) []string {
	if sessionID == "" {
		return nil
	}
	switch agent {
	case "claude":
		return []string{"claude", "--resume", sessionID}
	case "codex":
		return []string{"codex", "resume", sessionID}
	case "amp":
		return []string{"amp", "threads", "continue", sessionID}
	case "pi":
		return []string{"pi", "--session", sessionID}
	case "kimi":
		return []string{"kimi", "--resume", sessionID}
	default:
		return nil
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run 'TestResumeArgv|TestResumeCommand' -v`
Expected: PASS (both).

- [ ] **Step 5: Dedup `search/run.go`**

Replace the whole `resumeCommand` function at the bottom of `internal/search/run.go` (the switch duplicating the agent→argv mapping) with:

```go
func resumeCommand(agent, sessionID string) (*exec.Cmd, string) {
	argv := core.ResumeArgv(agent, sessionID)
	if argv == nil {
		return nil, ""
	}
	return exec.Command(argv[0], argv[1:]...), core.ResumeCommand(agent, sessionID)
}
```

- [ ] **Step 6: Run search + core tests**

Run: `go test ./internal/search/ ./internal/core/`
Expected: PASS (no behavior change: same five agents, same argv).

- [ ] **Step 7: Commit**

```bash
git add internal/core/resume.go internal/core/resume_test.go internal/search/run.go
git commit -m "refactor(core): add ResumeArgv as single source of resume argv"
```

---

### Task 2: directory filter (`internal/sessions/filter.go`)

**Files:**
- Create: `internal/sessions/filter.go`
- Test: `internal/sessions/filter_test.go`

**Interfaces:**
- Consumes: `model.Session` fields `CWD`, `IsSidechain`, `LastActivity`.
- Produces: `FilterByDir(sessions []*model.Session, dir string) ([]*model.Session, error)` — matching sessions, sidechains excluded, sorted `LastActivity` desc. Task 5 calls exactly this.

- [ ] **Step 1: Write the failing tests**

Create `internal/sessions/filter_test.go`:

```go
package sessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func sess(cwd string, last time.Time, sidechain bool) *model.Session {
	return &model.Session{SessionID: cwd, CWD: cwd, LastActivity: last, IsSidechain: sidechain}
}

func TestFilterByDirMatching(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	in := []*model.Session{
		sess(base, now, false),                // exact match
		sess(sub, now.Add(-time.Hour), false), // subdirectory
		sess(base+"extra", now, false),        // false prefix — excluded
		sess("/somewhere/else", now, false),   // unrelated — excluded
		sess(base, now, true),                 // sidechain — excluded
	}
	got, err := FilterByDir(in, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(got))
	}
	if got[0].CWD != base || got[1].CWD != sub {
		t.Errorf("wrong contents/order: %s, %s", got[0].CWD, got[1].CWD)
	}
}

func TestFilterByDirSymlinkedTarget(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	got, err := FilterByDir([]*model.Session{sess(real, time.Now(), false)}, link)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("symlinked target should match resolved CWD, got %d matches", len(got))
	}
}

func TestFilterByDirSortsByLastActivityDesc(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	in := []*model.Session{
		sess(base, now.Add(-2*time.Hour), false),
		sess(base, now, false),
		sess(base, now.Add(-time.Hour), false),
	}
	got, err := FilterByDir(in, base)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].LastActivity.Equal(now) {
		t.Errorf("most recent must be first, got %v", got[0].LastActivity)
	}
	if !got[2].LastActivity.Equal(now.Add(-2 * time.Hour)) {
		t.Errorf("oldest must be last, got %v", got[2].LastActivity)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sessions/ -v`
Expected: FAIL — `undefined: FilterByDir` (compile error).

- [ ] **Step 3: Write the implementation**

Create `internal/sessions/filter.go`:

```go
// Package sessions implements the `lazyagent sessions` subcommand: list the
// sessions recorded for a directory (all agents) and reopen one.
package sessions

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// targetVariants normalizes dir for matching: the cleaned absolute path
// plus, when it differs, the symlink-resolved form — so /tmp also matches
// sessions recorded under /private/tmp on macOS.
func targetVariants(dir string) ([]string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	abs = filepath.Clean(abs)
	variants := []string{abs}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		if resolved = filepath.Clean(resolved); resolved != abs {
			variants = append(variants, resolved)
		}
	}
	return variants, nil
}

// matchesDir reports whether cwd equals a target variant or lies beneath one
// (prefix on a path boundary: /foo/bar must not match /foo/barbaz).
func matchesDir(cwd string, variants []string) bool {
	if cwd == "" {
		return false
	}
	cwd = filepath.Clean(cwd)
	for _, v := range variants {
		if cwd == v || strings.HasPrefix(cwd, v+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// FilterByDir returns the sessions whose CWD is dir or a subdirectory of it,
// excluding sidechains, sorted by LastActivity descending.
func FilterByDir(sessions []*model.Session, dir string) ([]*model.Session, error) {
	variants, err := targetVariants(dir)
	if err != nil {
		return nil, err
	}
	var out []*model.Session
	for _, s := range sessions {
		if s.IsSidechain {
			continue
		}
		if matchesDir(s.CWD, variants) {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b *model.Session) int {
		return b.LastActivity.Compare(a.LastActivity)
	})
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sessions/ -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/sessions/filter.go internal/sessions/filter_test.go
git commit -m "feat(sessions): add directory filter for session listing"
```

---

### Task 3: JSON output (`internal/sessions/json.go`)

**Files:**
- Create: `internal/sessions/json.go`
- Test: `internal/sessions/json_test.go`

**Interfaces:**
- Consumes: `core.ResumeCommand(agent, sessionID) string` (Task 1 file, pre-existing function).
- Produces: `writeJSON(w io.Writer, sessions []*model.Session, nameFor func(*model.Session) string) error`. Task 5 calls exactly this; `nameFor` resolves the display name (custom alias or agent-provided) per session.

- [ ] **Step 1: Write the failing tests**

Create `internal/sessions/json_test.go`:

```go
package sessions

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestWriteJSONFields(t *testing.T) {
	last := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	list := []*model.Session{{
		Agent: "claude", SessionID: "abc", CWD: "/proj",
		LastActivity: last, TotalMessages: 5,
	}}
	var buf bytes.Buffer
	if err := writeJSON(&buf, list, func(*model.Session) string { return "my-alias" }); err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 entry, got %d", len(out))
	}
	e := out[0]
	if e["agent"] != "claude" || e["session_id"] != "abc" || e["cwd"] != "/proj" {
		t.Errorf("identity fields wrong: %v", e)
	}
	if e["name"] != "my-alias" {
		t.Errorf("name = %v, want my-alias", e["name"])
	}
	if e["messages"] != float64(5) {
		t.Errorf("messages = %v, want 5", e["messages"])
	}
	if e["resume_command"] != "claude --resume abc" {
		t.Errorf("resume_command = %v", e["resume_command"])
	}
	if _, ok := e["last_activity"]; !ok {
		t.Error("missing last_activity")
	}
}

func TestWriteJSONEmptyList(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, nil, func(*model.Session) string { return "" }); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "[]" {
		t.Errorf("empty list must encode as [], got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sessions/ -run TestWriteJSON -v`
Expected: FAIL — `undefined: writeJSON` (compile error).

- [ ] **Step 3: Write the implementation**

Create `internal/sessions/json.go`:

```go
package sessions

import (
	"encoding/json"
	"io"
	"time"

	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// sessionJSON is the wire shape of one row in `lazyagent sessions --json`.
// Field names are part of the CLI contract — do not rename.
type sessionJSON struct {
	Agent         string    `json:"agent"`
	SessionID     string    `json:"session_id"`
	Name          string    `json:"name,omitempty"`
	CWD           string    `json:"cwd"`
	LastActivity  time.Time `json:"last_activity"`
	Messages      int       `json:"messages"`
	ResumeCommand string    `json:"resume_command,omitempty"`
}

// writeJSON emits the filtered sessions as an indented JSON array.
// nameFor resolves the display name for a session ("" omits the field).
func writeJSON(w io.Writer, sessions []*model.Session, nameFor func(*model.Session) string) error {
	out := make([]sessionJSON, 0, len(sessions)) // non-nil so empty encodes as []
	for _, s := range sessions {
		out = append(out, sessionJSON{
			Agent:         s.Agent,
			SessionID:     s.SessionID,
			Name:          nameFor(s),
			CWD:           s.CWD,
			LastActivity:  s.LastActivity,
			Messages:      s.TotalMessages,
			ResumeCommand: core.ResumeCommand(s.Agent, s.SessionID),
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sessions/ -v`
Expected: PASS (filter + json tests).

- [ ] **Step 5: Commit**

```bash
git add internal/sessions/json.go internal/sessions/json_test.go
git commit -m "feat(sessions): add --json output encoder"
```

---

### Task 4: interactive picker (`internal/sessions/picker.go`)

**Files:**
- Create: `internal/sessions/picker.go`
- Test: `internal/sessions/picker_test.go`

**Interfaces:**
- Consumes: `core.ResumeArgv` (Task 1), `core.ResumeCommand`, `chatops` styles (`StyleMuted`, `StyleFooter`, `StyleTableHeader`, `ColBorderDim`).
- Produces (Task 5 relies on these exact names):
  - `type pickerAction int` with constants `actionQuit`, `actionOpen`, `actionCopy`
  - `runPicker(list []*model.Session, titles []string, dirLabel string) (*model.Session, pickerAction, error)` — nil session ⇔ `actionQuit`
  - `titleFor(s *model.Session, alias string) string`
  - `relTime(t, now time.Time) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/sessions/picker_test.go`:

```go
package sessions

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/illegalstudio/lazyagent/internal/model"
)

func keyRune(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func TestPickerNavigationAndOpen(t *testing.T) {
	m := pickerModel{
		sessions: []*model.Session{
			{Agent: "claude", SessionID: "a"},
			{Agent: "grok", SessionID: "b"},
		},
		titles: []string{"one", "two"},
	}

	next, _ := m.Update(keyRune('j'))
	m = next.(pickerModel)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	// j at the bottom stays put.
	next, _ = m.Update(keyRune('j'))
	m = next.(pickerModel)
	if m.cursor != 1 {
		t.Fatalf("cursor moved past end: %d", m.cursor)
	}

	// enter on grok (no resume at all): stays open, shows a status message.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(pickerModel)
	if cmd != nil {
		t.Fatal("picker must stay open on a grok row")
	}
	if m.status == "" {
		t.Fatal("expected a status message for grok")
	}

	// back up to claude and open.
	next, _ = m.Update(keyRune('k'))
	m = next.(pickerModel)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(pickerModel)
	if m.action != actionOpen {
		t.Fatalf("action = %v, want actionOpen", m.action)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit after open")
	}
}

func TestPickerEnterFallsBackToCopy(t *testing.T) {
	// opencode: display command exists, executable argv does not → copy.
	m := pickerModel{sessions: []*model.Session{{Agent: "opencode", SessionID: "x"}}, titles: []string{"t"}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(pickerModel)
	if m.action != actionCopy {
		t.Fatalf("action = %v, want actionCopy", m.action)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit after copy")
	}
}

func TestPickerQuitKeys(t *testing.T) {
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyCtrlC}, keyRune('q')} {
		m := pickerModel{sessions: []*model.Session{{Agent: "claude", SessionID: "a"}}, titles: []string{"t"}}
		next, cmd := m.Update(k)
		m = next.(pickerModel)
		if m.action != actionQuit || cmd == nil {
			t.Fatalf("key %v: action=%v cmd=%v, want quit", k, m.action, cmd)
		}
	}
}

func TestRelTime(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-2 * time.Hour), "2h ago"},
		{now.Add(-30 * time.Hour), "yesterday"},
		{now.Add(-3 * 24 * time.Hour), "3d ago"},
		{now.Add(-40 * 24 * time.Hour), "2026-06-10"},
		{time.Time{}, "unknown"},
	}
	for _, c := range cases {
		if got := relTime(c.in, now); got != c.want {
			t.Errorf("relTime(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTitleFor(t *testing.T) {
	s := &model.Session{
		Name: "agent-name",
		RecentMessages: []model.ConversationMessage{
			{Role: "assistant", Text: "hi"},
			{Role: "user", Text: "fix the build\nplease"},
		},
	}
	if got := titleFor(s, "custom"); got != "custom" {
		t.Errorf("alias must win, got %q", got)
	}
	if got := titleFor(s, ""); got != "agent-name" {
		t.Errorf("agent name second, got %q", got)
	}
	s.Name = ""
	if got := titleFor(s, ""); got != "fix the build please" {
		t.Errorf("user message preview third, got %q", got)
	}
	if got := titleFor(&model.Session{}, ""); got != "(no messages)" {
		t.Errorf("fallback, got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sessions/ -v`
Expected: FAIL — `undefined: pickerModel` etc. (compile error).

- [ ] **Step 3: Write the implementation**

Create `internal/sessions/picker.go`:

```go
package sessions

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/illegalstudio/lazyagent/internal/chatops"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// pickerAction is what the user chose when the picker exited.
type pickerAction int

const (
	actionQuit pickerAction = iota
	actionOpen
	actionCopy
)

// agentColors maps agent keys to identity colors. The keys shared with the
// prune/compact selectors reuse their palette; the rest stay in the family.
var agentColors = map[string]lipgloss.Color{
	"claude":   "#E7A15E",
	"pi":       "#F38BA8",
	"codex":    "#A6E3A1",
	"grok":     "#89B4FA",
	"kimi":     "#CBA6F7",
	"opencode": "#94E2D5",
	"kilo":     "#F9E2AF",
	"cursor":   "#B4BEFE",
	"amp":      "#EBA0AC",
}

var (
	stylePickerBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(chatops.ColBorderDim).
		Padding(0, 1)
	styleCursor = lipgloss.NewStyle().Foreground(chatops.ColPrimary).Bold(true)
	styleStatus = lipgloss.NewStyle().Foreground(chatops.ColWarn)
)

type pickerModel struct {
	sessions []*model.Session
	titles   []string // pre-computed row titles, same indexing as sessions
	cursor   int
	action   pickerAction
	status   string
	dirLabel string
	now      time.Time
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		m.status = ""
	case "down", "j":
		if m.cursor < len(m.sessions)-1 {
			m.cursor++
		}
		m.status = ""
	case "enter":
		s := m.sessions[m.cursor]
		switch {
		case core.ResumeArgv(s.Agent, s.SessionID) != nil:
			m.action = actionOpen
			return m, tea.Quit
		case core.ResumeCommand(s.Agent, s.SessionID) != "":
			// Not executable from here, but the command exists: copy it.
			m.action = actionCopy
			return m, tea.Quit
		default:
			m.status = fmt.Sprintf("no resume available for %s sessions", s.Agent)
		}
	case "c":
		s := m.sessions[m.cursor]
		if core.ResumeCommand(s.Agent, s.SessionID) != "" {
			m.action = actionCopy
			return m, tea.Quit
		}
		m.status = fmt.Sprintf("no resume command for %s sessions", s.Agent)
	case "q", "esc", "ctrl+c":
		m.action = actionQuit
		return m, tea.Quit
	}
	return m, nil
}

func (m pickerModel) View() string {
	var rows []string
	for i, s := range m.sessions {
		marker := "  "
		if i == m.cursor {
			marker = styleCursor.Render("▸ ")
		}
		agent := lipgloss.NewStyle().Foreground(agentColor(s.Agent)).Render(fmt.Sprintf("%-8s", s.Agent))
		row := fmt.Sprintf("%s%s %-10s %4d  %s",
			marker, agent, relTime(s.LastActivity, m.now), s.TotalMessages, m.titles[i])
		if core.ResumeCommand(s.Agent, s.SessionID) == "" {
			row += chatops.StyleMuted.Render("  (no resume)")
		}
		rows = append(rows, row)
	}
	title := chatops.StyleTableHeader.Render(fmt.Sprintf("Sessions in %s (%d)", m.dirLabel, len(m.sessions)))
	box := stylePickerBox.Render(title + "\n\n" + strings.Join(rows, "\n"))
	footer := chatops.StyleFooter.Render("  ↑/↓ move · enter open · c copy resume cmd · q quit")
	if m.status != "" {
		footer += "\n" + styleStatus.Render("  "+m.status)
	}
	return box + "\n" + footer + "\n"
}

func agentColor(agent string) lipgloss.Color {
	if c, ok := agentColors[agent]; ok {
		return c
	}
	return chatops.ColTextSubtle
}

// runPicker shows the interactive list and returns the chosen session and
// action. A nil session means the user quit without choosing.
func runPicker(list []*model.Session, titles []string, dirLabel string) (*model.Session, pickerAction, error) {
	m := pickerModel{sessions: list, titles: titles, dirLabel: dirLabel, now: time.Now()}
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stderr))
	final, err := p.Run()
	if err != nil {
		return nil, actionQuit, fmt.Errorf("session picker: %w", err)
	}
	res := final.(pickerModel)
	if res.action == actionQuit {
		return nil, actionQuit, nil
	}
	return res.sessions[res.cursor], res.action, nil
}

// relTime renders t relative to now, degrading to an absolute date after
// 30 days. Zero times render as "unknown".
func relTime(t, now time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Format("2006-01-02")
}

// titleFor picks the row title: user alias, then agent-provided name, then
// the earliest user message still in the RecentMessages window.
func titleFor(s *model.Session, alias string) string {
	if alias != "" {
		return alias
	}
	if s.Name != "" {
		return s.Name
	}
	for _, msg := range s.RecentMessages {
		if msg.Role == "user" && strings.TrimSpace(msg.Text) != "" {
			return truncate(msg.Text, 60)
		}
	}
	return "(no messages)"
}

// truncate collapses whitespace and cuts s to max runes with an ellipsis.
func truncate(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sessions/ -v`
Expected: PASS (all picker, filter, json tests).

- [ ] **Step 5: Commit**

```bash
git add internal/sessions/picker.go internal/sessions/picker_test.go
git commit -m "feat(sessions): add interactive session picker"
```

---

### Task 5: `Run` wiring + `main.go` registration

**Files:**
- Create: `internal/sessions/sessions.go`
- Modify: `main.go` (subcommand switch ~line 40, usage text ~line 88)
- Test: `internal/sessions/run_test.go`

**Interfaces:**
- Consumes: `FilterByDir` (Task 2), `writeJSON` (Task 3), `runPicker`/`titleFor`/`actionOpen`/`actionCopy` (Task 4), `core.LoadConfig`, `core.BuildProvider`, `core.NewSessionNames().Get(sessionID)`, `core.CopyToClipboard`, `core.ResumeArgv`, `core.ResumeCommand`.
- Produces: `sessions.Run(args []string) int` — the subcommand entrypoint `main.go` calls.

- [ ] **Step 1: Write the failing tests**

Create `internal/sessions/run_test.go` (only hermetic paths — they must return before any discovery or config I/O):

```go
package sessions

import "testing"

func TestRunRejectsUnknownAgent(t *testing.T) {
	if code := Run([]string{"--agent", "nope"}); code != 2 {
		t.Errorf("unknown agent: exit = %d, want 2", code)
	}
}

func TestRunRejectsMissingDir(t *testing.T) {
	if code := Run([]string{"--dir", "/nonexistent-lazyagent-test-dir"}); code != 2 {
		t.Errorf("missing dir: exit = %d, want 2", code)
	}
}

func TestRunHelp(t *testing.T) {
	if code := Run([]string{"--help"}); code != 0 {
		t.Errorf("--help: exit = %d, want 0", code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sessions/ -run TestRun -v`
Expected: FAIL — `undefined: Run` (compile error).

- [ ] **Step 3: Write the implementation**

Create `internal/sessions/sessions.go`:

```go
package sessions

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/illegalstudio/lazyagent/internal/chatops"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
	"github.com/mattn/go-isatty"
)

var validAgents = map[string]bool{
	"claude": true, "pi": true, "opencode": true, "kilo": true, "cursor": true,
	"codex": true, "amp": true, "grok": true, "kimi": true, "all": true,
}

// Run implements `lazyagent sessions`. It lists the sessions recorded for
// the current (or --dir) directory across agents and reopens the chosen one.
func Run(args []string) int {
	fs := flag.NewFlagSet("sessions", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	agent := fs.String("agent", "all", "Agent to list: claude, pi, opencode, kilo, cursor, codex, amp, grok, kimi, all")
	jsonOut := fs.Bool("json", false, "Print the session list as JSON and exit")
	dirFlag := fs.String("dir", "", "List sessions for this directory instead of the current one")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `lazyagent sessions — list sessions for a directory and reopen one

Lists every recorded session whose working directory is the current
directory (or --dir) or a subdirectory of it, across all agents.
Selecting a session resumes it with the originating agent's CLI.

Usage:
  lazyagent sessions
  lazyagent sessions --agent claude
  lazyagent sessions --json
  lazyagent sessions --dir ~/projects/foo

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if !validAgents[*agent] {
		fmt.Fprintf(os.Stderr, "Error: unknown --agent value %q (use claude, pi, opencode, kilo, cursor, codex, amp, grok, kimi, or all)\n", *agent)
		return 2
	}

	dir := *dirFlag
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot determine working directory: %v\n", err)
			return 2
		}
		dir = wd
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: %q is not a directory\n", dir)
		return 2
	}

	cfg := core.LoadConfig()
	provider := core.BuildProvider(*agent, cfg)
	all, err := provider.DiscoverSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	filtered, err := FilterByDir(all, dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	names := core.NewSessionNames()
	nameFor := func(s *model.Session) string {
		if alias := names.Get(s.SessionID); alias != "" {
			return alias
		}
		return s.Name
	}

	if *jsonOut {
		if err := writeJSON(os.Stdout, filtered, nameFor); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		return 0
	}

	if len(filtered) == 0 {
		fmt.Fprintf(os.Stderr, "No sessions found in %s.\n", abbreviateHome(dir))
		return 0
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		fmt.Fprintln(os.Stderr, "Error: the interactive picker needs a terminal (use --json for scripted output)")
		return 2
	}

	titles := make([]string, len(filtered))
	for i, s := range filtered {
		titles[i] = titleFor(s, names.Get(s.SessionID))
	}
	chosen, action, err := runPicker(filtered, titles, abbreviateHome(dir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	switch action {
	case actionOpen:
		return openSession(chosen)
	case actionCopy:
		cmdStr := core.ResumeCommand(chosen.Agent, chosen.SessionID)
		if err := core.CopyToClipboard(cmdStr); err != nil {
			fmt.Fprintf(os.Stderr, "Copy failed: %v\nCommand: %s\n", err, cmdStr)
			return 1
		}
		fmt.Fprintf(os.Stderr, "Copied to clipboard: %s\n", cmdStr)
	}
	return 0
}

// openSession execs the agent's resume command in the current terminal,
// running from the session's own CWD when it still exists (claude --resume
// locates sessions by project directory).
func openSession(s *model.Session) int {
	argv := core.ResumeArgv(s.Agent, s.SessionID)
	if argv == nil {
		fmt.Fprintf(os.Stderr, "No resume command available for %s sessions.\n", s.Agent)
		return 1
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	if s.CWD != "" {
		if info, err := os.Stat(s.CWD); err == nil && info.IsDir() {
			cmd.Dir = s.CWD
		}
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Fprintf(os.Stderr, "%s %s\n", chatops.StyleMuted.Render("Opening:"), core.ResumeCommand(s.Agent, s.SessionID))
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

// abbreviateHome shortens a path with the user's home directory to ~/...
func abbreviateHome(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sessions/ -v`
Expected: PASS (all package tests).

- [ ] **Step 5: Register in `main.go`**

In the subcommand switch at the top of `main()` (after the `"passphrase"` case), add:

```go
		case "sessions":
			os.Exit(sessions.Run(os.Args[2:]))
```

Add the import `"github.com/illegalstudio/lazyagent/internal/sessions"` to the import block.

In `flag.Usage`, under the `Subcommands:` heading, after the `lazyagent search` lines, add:

```
  lazyagent sessions            List sessions for the current directory and reopen one
  lazyagent sessions --help     Show sessions options (--agent, --json, --dir)
```

- [ ] **Step 6: Build and smoke-test**

Run: `go build . && go vet ./... && go test ./internal/...`
Expected: clean build, vet clean, all tests PASS.

Run: `./lazyagent sessions --json --dir "$(pwd)" | head -20`
Expected: a JSON array (possibly `[]`) — no picker, exit 0.

Run: `./lazyagent sessions` (in a terminal, from the repo root)
Expected: the picker renders with this repo's sessions; `q` exits with code 0.

- [ ] **Step 7: Commit**

```bash
git add internal/sessions/sessions.go internal/sessions/run_test.go main.go
git commit -m "feat(sessions): add lazyagent sessions subcommand"
```

---

### Task 6: documentation

**Files:**
- Create: `docs/usage/sessions.md`
- Modify: `docs/README.md` (Usage section, ~line 30)
- Modify: `docs/usage/cli.md` (subcommand list, ~line 10)
- Modify: `README.md` (News section subcommand list, ~line 40)

**Interfaces:**
- Consumes: the final CLI behavior from Task 5 (flags, keys, exit codes).
- Produces: user-facing docs; no code.

- [ ] **Step 1: Write the docs page**

Create `docs/usage/sessions.md`:

````markdown
---
title: "Sessions for a directory"
description: "List every recorded session for the current directory — across all agents — and reopen one."
sidebar:
  order: 3
---

`lazyagent sessions` lists every session whose working directory is the
current directory (or a subdirectory of it), across all supported agents,
newest first. Selecting a session resumes it with the originating agent's
own CLI.

## Synopsis

```
lazyagent sessions [--agent NAME] [--json] [--dir PATH]
```

## The picker

```
┌─ Sessions in ~/projects/foo (12) ───────────────────────────┐
│ ▸ claude  2h ago      84  fix build embed placeholder       │
│   codex   yesterday   31  webhook config models             │
│   grok    3d ago      12  docs limits   (no resume)         │
└─────────────────────────────────────────────────────────────┘
  ↑/↓ move · enter open · c copy resume cmd · q quit
```

Each row shows the agent, relative last-activity time, message count, and a
title (your custom session name when set, otherwise a preview of the first
user message).

| Key | Action |
|-----|--------|
| `↑`/`k`, `↓`/`j` | Move the cursor |
| `enter` | Reopen the session in this terminal |
| `c` | Copy the resume command to the clipboard |
| `q` / `esc` / `ctrl+c` | Quit without opening |

**Opening** runs the agent's resume command (e.g. `claude --resume <id>`)
with this terminal attached, from the session's own working directory when
it still exists. Agents lazyagent can exec directly: Claude Code, Codex,
Amp, pi, and Kimi. For OpenCode, Kilo, and Cursor the resume command is
copied to the clipboard instead; Grok has no resume command.

## Flags

| Flag | Type | Default | Summary |
|------|------|---------|---------|
| `--agent NAME` | string | `all` | Restrict the listing to one agent |
| `--json` | bool | `false` | Print the list as JSON on stdout and exit (no picker) |
| `--dir PATH` | string | current dir | List sessions for another directory |

## JSON output

`--json` emits an array (possibly `[]`), one object per session:

```json
[
  {
    "agent": "claude",
    "session_id": "abc123",
    "name": "fix-build",
    "cwd": "/Users/me/projects/foo",
    "last_activity": "2026-07-20T09:12:33Z",
    "messages": 84,
    "resume_command": "claude --resume abc123"
  }
]
```

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success — including quitting the picker or an empty listing |
| 1 | Runtime failure (discovery, resume exec, clipboard) |
| 2 | Usage error (unknown `--agent`, `--dir` not a directory, no TTY without `--json`) |
````

- [ ] **Step 2: Link it from the indexes**

In `docs/README.md`, in the **Usage** section after the Recipes line, add:

```markdown
- [Sessions for a directory](usage/sessions.md) — list every recorded session for a directory and reopen one, across all agents
```

In `docs/usage/cli.md`, in the subcommand list at the top, after the `lazyagent limits` line, add:

```markdown
- [`lazyagent sessions`](sessions.md) — list sessions for the current directory and reopen one
```

In the root `README.md`, in the **News** subcommand list (after the `lazyagent limits` bullet), add:

```markdown
- **[`lazyagent sessions`](docs/usage/sessions.md)** — list every session recorded for the current directory — across all agents — and reopen one with the originating agent's CLI. Interactive picker, `--json` for scripts.
```

- [ ] **Step 3: Verify links and build**

Run: `go build . && go test ./internal/sessions/`
Expected: still green (docs-only change; build proves no accidental code edits).

Check: every relative link added above resolves to an existing file.

- [ ] **Step 4: Commit**

```bash
git add docs/usage/sessions.md docs/README.md docs/usage/cli.md README.md
git commit -m "docs(sessions): document the sessions subcommand"
```
