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
