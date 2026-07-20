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
