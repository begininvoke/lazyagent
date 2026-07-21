package sessions

import (
	"fmt"
	"os"
	"slices"
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
	height   int // terminal height from the last tea.WindowSizeMsg; 0 = unknown
}

// defaultVisibleRows is the row budget View falls back to when the terminal
// height is unknown (height == 0, e.g. in unit tests that never deliver a
// tea.WindowSizeMsg).
const defaultVisibleRows = 20

// chromeLines is the number of non-row lines View always emits: the box's
// top border, the title, the blank line separating the title from the rows,
// the box's bottom border, and the footer. The status line, when present,
// and the "N more" clip indicators are counted on top of this.
const chromeLines = 5

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m pickerModel) updateKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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

// rowBudget returns how many session rows View can render: the terminal
// height minus chromeLines minus one more if a status line is showing, with
// a floor of 1. Falls back to defaultVisibleRows when the height is unknown.
func (m pickerModel) rowBudget() int {
	if m.height <= 0 {
		return defaultVisibleRows
	}
	budget := m.height - chromeLines
	if m.status != "" {
		budget--
	}
	if budget < 1 {
		budget = 1
	}
	return budget
}

// visibleRange returns the half-open row window [start, end) of at most
// height rows that contains cursor, scrolling the window — never the
// cursor — as needed to stay within [0, total). When the whole list fits,
// it returns the full range.
func visibleRange(cursor, total, height int) (start, end int) {
	if height <= 0 || total <= height {
		return 0, total
	}
	start = cursor - height/2
	if start < 0 {
		start = 0
	}
	end = start + height
	if end > total {
		end = total
		start = end - height
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

// mergeSessions merges an incoming discovery batch (batchSessions/
// batchTitles, parallel slices) into the already-accumulated sessions/
// titles (also parallel), returning a new pair of parallel slices sorted by
// bySessionRecency — the same ordering rule FilterByDir uses, so the merged
// list is deterministic at any point in the stream regardless of the order
// batches arrive in. Titles travel alongside their session through the sort
// so the two slices never desync.
func mergeSessions(sessions []*model.Session, titles []string, batchSessions []*model.Session, batchTitles []string) ([]*model.Session, []string) {
	type row struct {
		session *model.Session
		title   string
	}
	rows := make([]row, 0, len(sessions)+len(batchSessions))
	for i, s := range sessions {
		rows = append(rows, row{s, titles[i]})
	}
	for i, s := range batchSessions {
		rows = append(rows, row{s, batchTitles[i]})
	}
	slices.SortStableFunc(rows, func(a, b row) int {
		return bySessionRecency(a.session, b.session)
	})
	mergedSessions := make([]*model.Session, len(rows))
	mergedTitles := make([]string, len(rows))
	for i, r := range rows {
		mergedSessions[i] = r.session
		mergedTitles[i] = r.title
	}
	return mergedSessions, mergedTitles
}

// relocateCursor computes where the cursor should land in merged given
// where it was before the merge (prevCursor into prevSessions).
//
// If the user hasn't navigated away from the top row yet (prevCursor <= 0),
// the cursor stays pinned to the top: there's no meaningful selection to
// preserve, so we don't try to "follow" whichever session happened to be
// first. Otherwise the previously selected session's SessionID is looked up
// in merged and the cursor follows it to its new index; if that session is
// no longer present, the cursor clamps to the nearest valid index.
func relocateCursor(prevSessions []*model.Session, prevCursor int, merged []*model.Session) int {
	if len(merged) == 0 {
		return 0
	}
	if prevCursor <= 0 || prevCursor >= len(prevSessions) {
		return 0
	}
	selectedID := prevSessions[prevCursor].SessionID
	for i, s := range merged {
		if s.SessionID == selectedID {
			return i
		}
	}
	// The previously selected session disappeared: clamp to the nearest
	// valid index (sessions are only ever added by this feature's merges,
	// so this branch is defensive rather than something that happens in
	// practice today).
	if prevCursor >= len(merged) {
		return len(merged) - 1
	}
	return prevCursor
}

func (m pickerModel) View() string {
	total := len(m.sessions)
	budget := m.rowBudget()
	start, end := visibleRange(m.cursor, total, budget)
	clippedAbove := start > 0
	clippedBelow := end < total
	// The clip indicators themselves consume rows out of the same budget,
	// so once we know we need one, shrink the budget and re-window.
	if clippedAbove {
		budget--
	}
	if clippedBelow {
		budget--
	}
	if clippedAbove || clippedBelow {
		if budget < 1 {
			budget = 1
		}
		start, end = visibleRange(m.cursor, total, budget)
		clippedAbove = start > 0
		clippedBelow = end < total
	}

	var rows []string
	if clippedAbove {
		rows = append(rows, chatops.StyleMuted.Render(fmt.Sprintf("  … %d more above", start)))
	}
	for i := start; i < end; i++ {
		s := m.sessions[i]
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
	if clippedBelow {
		rows = append(rows, chatops.StyleMuted.Render(fmt.Sprintf("  … %d more below", total-end)))
	}

	title := chatops.StyleTableHeader.Render(fmt.Sprintf("Sessions in %s (%d)", m.dirLabel, total))
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
	if len(list) == 0 {
		return nil, actionQuit, nil
	}
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
