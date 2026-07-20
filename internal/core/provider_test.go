package core

import (
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// fakeProvider is a test helper that returns pre-configured sessions.
type fakeProvider struct {
	sessions []*model.Session
	err      error
	watcher  bool
	interval time.Duration
	dirs     []string
}

func (f fakeProvider) DiscoverSessions() ([]*model.Session, error) {
	return f.sessions, f.err
}
func (f fakeProvider) UseWatcher() bool               { return f.watcher }
func (f fakeProvider) RefreshInterval() time.Duration { return f.interval }
func (f fakeProvider) WatchDirs() []string            { return f.dirs }

func TestMultiProvider_MergesSessions(t *testing.T) {
	p1 := fakeProvider{sessions: []*model.Session{
		{SessionID: "s1", CWD: "/project1"},
	}}
	p2 := fakeProvider{sessions: []*model.Session{
		{SessionID: "s2", CWD: "/project2"},
		{SessionID: "s3", CWD: "/project3"},
	}}

	mp := MultiProvider{Providers: []SessionProvider{p1, p2}}
	sessions, err := mp.DiscoverSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("got %d sessions, want 3", len(sessions))
	}
}

func TestMultiProvider_SkipsFailingProvider(t *testing.T) {
	failing := fakeProvider{err: errTest}
	working := fakeProvider{sessions: []*model.Session{
		{SessionID: "s1"},
	}}

	mp := MultiProvider{Providers: []SessionProvider{failing, working}}
	sessions, err := mp.DiscoverSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
}

func TestMultiProvider_UseWatcher(t *testing.T) {
	noWatch := fakeProvider{watcher: false}
	watch := fakeProvider{watcher: true}

	mp1 := MultiProvider{Providers: []SessionProvider{noWatch}}
	if mp1.UseWatcher() {
		t.Error("expected false when no provider uses watcher")
	}

	mp2 := MultiProvider{Providers: []SessionProvider{noWatch, watch}}
	if !mp2.UseWatcher() {
		t.Error("expected true when at least one provider uses watcher")
	}
}

func TestMultiProvider_WatchDirs(t *testing.T) {
	p1 := fakeProvider{dirs: []string{"/dir1"}}
	p2 := fakeProvider{dirs: []string{"/dir2", "/dir3"}}

	mp := MultiProvider{Providers: []SessionProvider{p1, p2}}
	dirs := mp.WatchDirs()
	if len(dirs) != 3 {
		t.Fatalf("got %d dirs, want 3", len(dirs))
	}
}

func TestMultiProvider_RefreshInterval(t *testing.T) {
	p1 := fakeProvider{interval: 0}
	p2 := fakeProvider{interval: 30 * time.Second}
	p3 := fakeProvider{interval: 10 * time.Second}

	mp := MultiProvider{Providers: []SessionProvider{p1, p2, p3}}
	got := mp.RefreshInterval()
	if got != 10*time.Second {
		t.Errorf("RefreshInterval = %v, want 10s", got)
	}
}

func TestBuildProvider_Grok(t *testing.T) {
	p := BuildProvider("grok", DefaultConfig())
	if _, ok := p.(*GrokProvider); !ok {
		t.Fatalf("BuildProvider(\"grok\") = %T, want *GrokProvider", p)
	}
}

func TestBuildProvider_Kimi(t *testing.T) {
	p := BuildProvider("kimi", DefaultConfig())
	if _, ok := p.(*KimiProvider); !ok {
		t.Fatalf("BuildProvider(\"kimi\") = %T, want *KimiProvider", p)
	}
}

func TestBuildProvider_Kilo(t *testing.T) {
	p := BuildProvider("kilo", DefaultConfig())
	if _, ok := p.(*KiloProvider); !ok {
		t.Fatalf("BuildProvider(\"kilo\") = %T, want *KiloProvider", p)
	}
}

func TestDefaultConfig_GrokEnabled(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.AgentEnabled("grok") {
		t.Error("grok must be enabled by default")
	}
}

func TestDefaultConfig_KiloEnabled(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.AgentEnabled("kilo") {
		t.Error("kilo must be enabled by default")
	}
}

func TestDefaultConfig_KimiEnabled(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.AgentEnabled("kimi") {
		t.Error("kimi must be enabled by default")
	}
}

var errTest = errorString("test error")

type errorString string

func (e errorString) Error() string { return string(e) }

// dirScopedFakeProvider is a test-local provider that implements
// DirScopedProvider so DiscoverMatching's fast path can be exercised. It
// records the matcher it received and returns its own preconfigured, already
// filtered sessions (ignoring the matcher's actual verdicts) so the test can
// assert DiscoverMatching used the fast-path result rather than falling back
// to DiscoverSessions.
type dirScopedFakeProvider struct {
	sessions        []*model.Session
	err             error
	unfiltered      []*model.Session // returned by DiscoverSessions if called (should not be, when fast path is used)
	receivedMatcher bool
	watcher         bool
	interval        time.Duration
	dirs            []string
}

func (f *dirScopedFakeProvider) DiscoverSessions() ([]*model.Session, error) {
	return f.unfiltered, nil
}

func (f *dirScopedFakeProvider) DiscoverSessionsMatching(cwdMatch func(string) bool) ([]*model.Session, error) {
	f.receivedMatcher = cwdMatch != nil
	return f.sessions, f.err
}

func (f *dirScopedFakeProvider) UseWatcher() bool               { return f.watcher }
func (f *dirScopedFakeProvider) RefreshInterval() time.Duration { return f.interval }
func (f *dirScopedFakeProvider) WatchDirs() []string            { return f.dirs }

func TestDiscoverMatching_UsesDirScopedFastPath(t *testing.T) {
	p := &dirScopedFakeProvider{
		sessions:   []*model.Session{{SessionID: "matched"}},
		unfiltered: []*model.Session{{SessionID: "should-not-be-used"}},
	}
	matcher := func(cwd string) bool { return true }

	sessions, err := DiscoverMatching(p, matcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !p.receivedMatcher {
		t.Error("expected DiscoverSessionsMatching to receive a non-nil matcher")
	}
	if len(sessions) != 1 || sessions[0].SessionID != "matched" {
		t.Fatalf("sessions = %#v, want the fast-path result", sessions)
	}
}

func TestDiscoverMatching_FallsBackToPlainDiscoverSessions(t *testing.T) {
	p := fakeProvider{sessions: []*model.Session{
		{SessionID: "s1"},
	}}

	sessions, err := DiscoverMatching(p, func(string) bool { return true })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != "s1" {
		t.Fatalf("sessions = %#v, want plain DiscoverSessions result", sessions)
	}
}

func TestDiscoverMatching_MultiProviderFanOut(t *testing.T) {
	scoped := &dirScopedFakeProvider{
		sessions: []*model.Session{{SessionID: "scoped-hit"}},
	}
	plain := fakeProvider{sessions: []*model.Session{
		{SessionID: "plain-hit"},
	}}
	failing := fakeProvider{err: errTest}

	mp := MultiProvider{Providers: []SessionProvider{scoped, plain, failing}}
	sessions, err := DiscoverMatching(mp, func(string) bool { return true })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !scoped.receivedMatcher {
		t.Error("expected the dir-scoped member to receive the matcher")
	}

	ids := make(map[string]bool)
	for _, s := range sessions {
		ids[s.SessionID] = true
	}
	if !ids["scoped-hit"] || !ids["plain-hit"] {
		t.Fatalf("sessions = %#v, want scoped-hit and plain-hit present", sessions)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2 (failing member contributes nothing)", len(sessions))
	}
}
