package core

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
	_ "modernc.org/sqlite"
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

// TestOpenCodeProvider_DiscoverSessionsMatching_FiltersByDirectory is a
// wiring test proving core.OpenCodeProvider.DiscoverSessionsMatching is
// reachable through the DirScopedProvider interface end-to-end (down
// through internal/opencode into internal/opencodefamily) and that it
// actually filters by directory.
func TestOpenCodeProvider_DiscoverSessionsMatching_FiltersByDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_DATA_DIR", dir)
	writeOpenCodeFamilyFixtureDB(t, filepath.Join(dir, "opencode.db"), "/tmp/core-opencode-a", "/tmp/core-opencode-b")

	p := NewOpenCodeProvider()
	var dsp DirScopedProvider = p
	sessions, err := dsp.DiscoverSessionsMatching(func(cwd string) bool { return cwd == "/tmp/core-opencode-a" })
	if err != nil {
		t.Fatalf("DiscoverSessionsMatching() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].CWD != "/tmp/core-opencode-a" {
		t.Fatalf("sessions = %#v, want one session in /tmp/core-opencode-a", sessions)
	}

	all, err := p.DiscoverSessions()
	if err != nil {
		t.Fatalf("DiscoverSessions() error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2 (unfiltered discovery must still return everything)", len(all))
	}
}

// TestKiloProvider_DiscoverSessionsMatching_FiltersByDirectory mirrors
// TestOpenCodeProvider_DiscoverSessionsMatching_FiltersByDirectory for
// core.KiloProvider, which wraps the same opencodefamily engine.
func TestKiloProvider_DiscoverSessionsMatching_FiltersByDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KILO_DATA_DIR", dir)
	writeOpenCodeFamilyFixtureDB(t, filepath.Join(dir, "kilo.db"), "/tmp/core-kilo-a", "/tmp/core-kilo-b")

	p := NewKiloProvider()
	var dsp DirScopedProvider = p
	sessions, err := dsp.DiscoverSessionsMatching(func(cwd string) bool { return cwd == "/tmp/core-kilo-a" })
	if err != nil {
		t.Fatalf("DiscoverSessionsMatching() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].CWD != "/tmp/core-kilo-a" {
		t.Fatalf("sessions = %#v, want one session in /tmp/core-kilo-a", sessions)
	}

	all, err := p.DiscoverSessions()
	if err != nil {
		t.Fatalf("DiscoverSessions() error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2 (unfiltered discovery must still return everything)", len(all))
	}
}

// writeOpenCodeFamilyFixtureDB creates two sessions, one in each of dirA and
// dirB, using the minimal OpenCode-family schema (session/message/part).
func writeOpenCodeFamilyFixtureDB(t *testing.T, dbPath, dirA, dirB string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE session (
			id text PRIMARY KEY,
			project_id text NOT NULL,
			parent_id text,
			directory text NOT NULL,
			title text NOT NULL,
			version text NOT NULL,
			time_created integer NOT NULL,
			time_updated integer NOT NULL,
			time_compacting integer,
			time_archived integer
		)`,
		`CREATE TABLE message (
			id text PRIMARY KEY,
			session_id text NOT NULL,
			time_created integer NOT NULL,
			time_updated integer NOT NULL,
			data text NOT NULL
		)`,
		`CREATE TABLE part (
			id text PRIMARY KEY,
			message_id text NOT NULL,
			session_id text NOT NULL,
			time_created integer NOT NULL,
			time_updated integer NOT NULL,
			data text NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}

	insert := func(id, directory string, timeUpdated int64) {
		if _, err := db.Exec(
			`INSERT INTO session (id, project_id, parent_id, directory, title, version, time_created, time_updated, time_compacting, time_archived)
			 VALUES (?, ?, NULL, ?, ?, '1.0.0', 1700000000000, ?, NULL, NULL)`,
			id, id+"_proj", directory, id, timeUpdated,
		); err != nil {
			t.Fatalf("insert session %s: %v", id, err)
		}
	}
	insert("ses_dir_a", dirA, 1700000001000)
	insert("ses_dir_b", dirB, 1700000002000)
}

// --- DiscoverMatchingStream ---

// collectStream runs DiscoverMatchingStream, gathers every emitted batch
// (protected by a mutex, since emit is called concurrently from the
// discovery goroutines) and blocks until done closes or a timeout elapses,
// then returns the batches in emission order plus their count.
func collectStream(t *testing.T, p SessionProvider, cwdMatch func(string) bool) (batches [][]*model.Session) {
	t.Helper()
	var mu sync.Mutex
	done := DiscoverMatchingStream(p, cwdMatch, func(sessions []*model.Session) {
		mu.Lock()
		defer mu.Unlock()
		batches = append(batches, sessions)
	})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("DiscoverMatchingStream: done never closed")
	}
	return batches
}

func TestDiscoverMatchingStream_PerMemberEmission(t *testing.T) {
	p1 := fakeProvider{sessions: []*model.Session{{SessionID: "s1"}}}
	p2 := fakeProvider{sessions: []*model.Session{{SessionID: "s2"}, {SessionID: "s3"}}}
	mp := MultiProvider{Providers: []SessionProvider{p1, p2}}

	batches := collectStream(t, mp, func(string) bool { return true })
	if len(batches) != 2 {
		t.Fatalf("got %d batches, want 2 (one per member)", len(batches))
	}
	ids := make(map[string]bool)
	for _, batch := range batches {
		for _, s := range batch {
			ids[s.SessionID] = true
		}
	}
	if !ids["s1"] || !ids["s2"] || !ids["s3"] {
		t.Fatalf("batches = %#v, want s1, s2, s3 all present", batches)
	}
}

func TestDiscoverMatchingStream_FailingMemberEmitsNothing(t *testing.T) {
	failing := fakeProvider{err: errTest}
	working := fakeProvider{sessions: []*model.Session{{SessionID: "s1"}}}
	mp := MultiProvider{Providers: []SessionProvider{failing, working}}

	batches := collectStream(t, mp, func(string) bool { return true })
	if len(batches) != 1 {
		t.Fatalf("got %d batches, want 1 (the failing member emits nothing)", len(batches))
	}
	if len(batches[0]) != 1 || batches[0][0].SessionID != "s1" {
		t.Fatalf("batches = %#v, want [[s1]]", batches)
	}
}

// TestDiscoverMatchingStream_SingleFailingProviderEmitsNothingButCloses
// covers the non-multi counterpart to
// TestDiscoverMatchingStream_FailingMemberEmitsNothing: a single provider
// (not wrapped in a MultiProvider) that errors is exactly one member, and
// that member failing must behave the same way a failing member of a
// larger fan-out does -- no emit, but done still closes. This is the
// scenario behind the picker's accepted divergence for a single
// misconfigured provider in interactive mode (see internal/sessions.Run):
// since DiscoverMatchingStream has no error channel by design, this failure
// is indistinguishable from "found nothing" to any caller.
func TestDiscoverMatchingStream_SingleFailingProviderEmitsNothingButCloses(t *testing.T) {
	p := fakeProvider{err: errTest}

	batches := collectStream(t, p, func(string) bool { return true })
	if len(batches) != 0 {
		t.Fatalf("got %d batches, want 0 (the single failing provider emits nothing)", len(batches))
	}
}

func TestDiscoverMatchingStream_SingleProviderNonMulti(t *testing.T) {
	p := &dirScopedFakeProvider{sessions: []*model.Session{{SessionID: "scoped"}}}

	batches := collectStream(t, p, func(string) bool { return true })
	if len(batches) != 1 {
		t.Fatalf("got %d batches, want 1 (a non-multi provider is one member)", len(batches))
	}
	if len(batches[0]) != 1 || batches[0][0].SessionID != "scoped" {
		t.Fatalf("batches = %#v, want [[scoped]]", batches)
	}
	if !p.receivedMatcher {
		t.Error("expected the single member to receive the matcher (dir-scoped fast path reused)")
	}
}

func TestDiscoverMatchingStream_EmptyMultiProviderClosesDoneImmediately(t *testing.T) {
	mp := MultiProvider{}

	batches := collectStream(t, mp, func(string) bool { return true })
	if len(batches) != 0 {
		t.Fatalf("got %d batches, want 0 (no members)", len(batches))
	}
}

func TestDiscoverMatchingStream_DoneNotClosedBeforeEmitReturns(t *testing.T) {
	// Neither member here is actually slow (fakeProvider returns
	// instantly) -- what this asserts is the ordering guarantee itself:
	// done must not close before every member's emit call has actually
	// been delivered, since it's only closed after every member's
	// goroutine (which calls emit synchronously before returning) has
	// finished, regardless of how long any given member takes in practice.
	memberA := fakeProvider{sessions: []*model.Session{{SessionID: "a"}}}
	memberB := fakeProvider{sessions: []*model.Session{{SessionID: "b"}}}
	mp := MultiProvider{Providers: []SessionProvider{memberA, memberB}}

	var mu sync.Mutex
	var emitted int
	done := DiscoverMatchingStream(mp, func(string) bool { return true }, func(sessions []*model.Session) {
		mu.Lock()
		emitted++
		mu.Unlock()
	})
	<-done
	mu.Lock()
	defer mu.Unlock()
	if emitted != 2 {
		t.Fatalf("emitted = %d, want 2 emits to have happened by the time done closed", emitted)
	}
}
