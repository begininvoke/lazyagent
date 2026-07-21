package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// loadingModel builds a Model in the same "not loaded yet" state NewModel
// produces (loading: true, manager constructed but never Reload()'d), so
// tests can drive the progressive-first-load message sequence
// (streamStartedMsg/streamBatchMsg/streamDoneMsg) exactly as the real
// bubbletea runtime would, without a TTY.
func loadingModel(t *testing.T, provider core.SessionProvider) Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	theme := DarkTheme()
	manager := core.NewSessionManager(60, provider)
	return Model{
		theme:     theme,
		sty:       newStyles(theme),
		actColors: activityColorMap(theme),
		loading:   true,
		manager:   manager,
		width:     80,
		height:    24,
	}
}

// --- streamNextCmd: the self-re-arming Cmd, same shape as watchCmd ---

func TestStreamNextCmd_ReturnsBatchMsgOnUpdate(t *testing.T) {
	updates := make(chan struct{}, 1)
	done := make(chan struct{})
	updates <- struct{}{}

	msg := streamNextCmd(updates, done)()
	if _, ok := msg.(streamBatchMsg); !ok {
		t.Fatalf("streamNextCmd() = %T, want streamBatchMsg", msg)
	}
}

func TestStreamNextCmd_ReturnsDoneMsgWhenDoneClosed(t *testing.T) {
	updates := make(chan struct{}, 1)
	done := make(chan struct{})
	close(done)

	msg := streamNextCmd(updates, done)()
	if _, ok := msg.(streamDoneMsg); !ok {
		t.Fatalf("streamNextCmd() = %T, want streamDoneMsg", msg)
	}
}

// --- Update: state transitions driven by streamStartedMsg / streamBatchMsg
// / streamDoneMsg, at the model level (no TTY) ---

func TestUpdate_StreamStartedMsg_StoresChannelsAndArmsNextCmd(t *testing.T) {
	m := loadingModel(t, testProvider{})

	updates := make(chan struct{}, 1)
	done := make(chan struct{})

	updated, cmd := m.Update(streamStartedMsg{updates: updates, done: done})
	nm := updated.(Model)

	if nm.streamUpdates == nil || nm.streamDone == nil {
		t.Fatal("streamStartedMsg must store the update/done channels on the model")
	}
	if cmd == nil {
		t.Fatal("streamStartedMsg must return a Cmd to keep waiting on the stream")
	}
	if !nm.loading {
		t.Error("loading must still be true right after the stream starts")
	}

	// The returned Cmd must be armed against the same channels just stored.
	close(done)
	msg := cmd()
	if _, ok := msg.(streamDoneMsg); !ok {
		t.Fatalf("Cmd returned by the streamStartedMsg handler produced %T, want streamDoneMsg", msg)
	}
}

func TestUpdate_StreamBatchMsg_RefreshesVisibleAndKeepsLoading(t *testing.T) {
	now := time.Now()
	provider := testProvider{sessions: []*model.Session{
		{SessionID: "s1", CWD: "/p1", LastActivity: now},
	}}
	m := loadingModel(t, provider)
	m.streamUpdates = make(chan struct{}, 1)
	m.streamDone = make(chan struct{})

	// Simulate the manager having merged a batch (what ReloadStreaming's
	// onUpdate-triggering merge does under the hood) before the message
	// arrives -- streamBatchMsg itself carries no data, exactly like
	// fileWatchMsg, it's just a "go re-read from the manager" signal.
	if err := m.manager.Reload(); err != nil {
		t.Fatalf("seeding manager state: %v", err)
	}

	if len(m.visible) != 0 {
		t.Fatalf("precondition: m.visible should start empty, got %d", len(m.visible))
	}

	updated, cmd := m.Update(streamBatchMsg{})
	nm := updated.(Model)

	if len(nm.visible) != 1 || nm.visible[0].SessionID != "s1" {
		t.Fatalf("visible after streamBatchMsg = %#v, want [s1]", nm.visible)
	}
	if !nm.loading {
		t.Error("loading must stay true on a batch message -- only streamDoneMsg clears it")
	}
	if cmd == nil {
		t.Fatal("streamBatchMsg must re-arm the wait Cmd for the next batch/done")
	}
}

func TestUpdate_StreamDoneMsg_ClearsLoadingAndRefreshesVisible(t *testing.T) {
	now := time.Now()
	provider := testProvider{sessions: []*model.Session{
		{SessionID: "s1", CWD: "/p1", LastActivity: now},
	}}
	m := loadingModel(t, provider)
	if err := m.manager.Reload(); err != nil {
		t.Fatalf("seeding manager state: %v", err)
	}

	updated, cmd := m.Update(streamDoneMsg{})
	nm := updated.(Model)

	if nm.loading {
		t.Error("loading must be false once streamDoneMsg arrives")
	}
	if len(nm.visible) != 1 {
		t.Fatalf("visible after streamDoneMsg = %d, want 1", len(nm.visible))
	}
	if cmd != nil {
		t.Error("streamDoneMsg must not re-arm any further wait Cmd")
	}
}

// TestUpdate_SessionsMsg_DoesNotResetLoading guards the interplay between
// the progressive first load and a normal (watcher/tick-driven) reload that
// might race it: since core.SessionManager.Reload() is absorbed while a
// stream is in flight, a fileWatchMsg/tickMsg landing mid-stream can still
// produce a sessionsMsg (with whatever's currently in the manager). That
// must not flip the loading indicator off early -- only streamDoneMsg owns
// that transition now.
func TestUpdate_SessionsMsg_DoesNotResetLoading(t *testing.T) {
	m := loadingModel(t, testProvider{})

	updated, _ := m.Update(sessionsMsg{sessions: nil})
	nm := updated.(Model)

	if !nm.loading {
		t.Error("sessionsMsg must not clear loading -- that's streamDoneMsg's job during the progressive first load")
	}
}

// --- runStreamingLoadCmd: end-to-end through the manager (no TTY) ---

func TestRunStreamingLoadCmd_EndToEndReachesStreamDone(t *testing.T) {
	now := time.Now()
	p1 := testProvider{sessions: []*model.Session{{SessionID: "s1", CWD: "/p1", LastActivity: now}}}
	p2 := testProvider{sessions: []*model.Session{{SessionID: "s2", CWD: "/p2", LastActivity: now}}}
	mp := core.MultiProvider{Providers: []core.SessionProvider{p1, p2}}

	m := loadingModel(t, mp)

	run := m.manager.BeginReloadStreaming()
	startMsg := runStreamingLoadCmd(run)()
	started, ok := startMsg.(streamStartedMsg)
	if !ok {
		t.Fatalf("runStreamingLoadCmd()() = %T, want streamStartedMsg", startMsg)
	}

	updated, cmd := m.Update(started)
	m = updated.(Model)

	// Drain batch/done messages until streamDoneMsg, bounded so a bug can't
	// hang the test suite.
	deadline := time.After(5 * time.Second)
	for i := 0; i < 10; i++ {
		msgCh := make(chan tea.Msg, 1)
		go func(c tea.Cmd) { msgCh <- c() }(cmd)

		var msg tea.Msg
		select {
		case msg = <-msgCh:
		case <-deadline:
			t.Fatal("stream never completed (streamDoneMsg not observed within timeout)")
		}

		updated, cmd = m.Update(msg)
		m = updated.(Model)
		if _, done := msg.(streamDoneMsg); done {
			break
		}
	}

	if m.loading {
		t.Fatal("model still loading after the stream reached streamDoneMsg")
	}
	if got := m.manager.Sessions(); len(got) != 2 {
		t.Fatalf("manager.Sessions() after the stream completed = %d, want 2", len(got))
	}
}

// --- Loading indicator in the title bar ---

func TestRenderTitleBar_ShowsLoadingIndicatorWhileLoading(t *testing.T) {
	m := loadingModel(t, testProvider{})
	bar := m.renderTitleBar()
	if !strings.Contains(bar, "loading") {
		t.Errorf("title bar while loading = %q, want it to mention loading", bar)
	}
}

func TestRenderTitleBar_HidesLoadingIndicatorOnceLoaded(t *testing.T) {
	m := loadingModel(t, testProvider{})
	m.loading = false
	bar := m.renderTitleBar()
	if strings.Contains(bar, "loading") {
		t.Errorf("title bar once loaded = %q, want no loading mention", bar)
	}
}
