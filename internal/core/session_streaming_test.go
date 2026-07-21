package core

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// --- exclusionMatcher: the single place that decides the predicate shared
// by discoverHonoringExclusions (Reload, UpdateActivities) and
// ReloadStreaming ---

func TestExclusionMatcher_NilWhenNoPatterns(t *testing.T) {
	mgr := NewSessionManager(60, fakeProvider{})
	if m := mgr.exclusionMatcher(); m != nil {
		t.Fatal("exclusionMatcher() with no patterns configured = non-nil, want nil (the streaming path's DiscoverMatchingStream call must receive a literal nil, not a permissive predicate)")
	}
}

func TestExclusionMatcher_PredicateMatchesExcludeCWDPredicateWhenPatternsSet(t *testing.T) {
	mgr := NewSessionManager(60, fakeProvider{})
	patterns := []string{"/tmp/scratch", ".claude-mem"}
	mgr.SetExcludeCWDSubstrings(patterns)

	got := mgr.exclusionMatcher()
	if got == nil {
		t.Fatal("exclusionMatcher() with patterns set = nil, want a predicate")
	}
	want := excludeCWDPredicate(patterns)
	cases := []string{"/tmp/scratch/build", "/home/user/.claude-mem/x", "/home/user/project"}
	for _, cwd := range cases {
		if got(cwd) != want(cwd) {
			t.Errorf("exclusionMatcher()(%q) = %v, want %v (excludeCWDPredicate(patterns)(%q))", cwd, got(cwd), want(cwd), cwd)
		}
	}
}

// --- ReloadStreaming: batches grow the snapshot, onUpdate fires per batch
// outside the lock ---

func TestReloadStreaming_BatchesGrowSnapshotAndFireOnUpdateOutsideLock(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	now := time.Now()
	p1 := fakeProvider{sessions: []*model.Session{{SessionID: "s1", LastActivity: now}}}
	p2 := fakeProvider{sessions: []*model.Session{
		{SessionID: "s2", LastActivity: now},
		{SessionID: "s3", LastActivity: now},
	}}
	mp := MultiProvider{Providers: []SessionProvider{p1, p2}}

	mgr := NewSessionManager(60, mp)

	var mu sync.Mutex
	var updates int
	var sizes []int

	done := make(chan struct{})
	go func() {
		mgr.ReloadStreaming(func() {
			// Calling back into the manager from onUpdate must not deadlock:
			// this only returns if onUpdate is invoked with m.mu NOT held.
			n := len(mgr.Sessions())
			mu.Lock()
			updates++
			sizes = append(sizes, n)
			mu.Unlock()
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ReloadStreaming did not complete (onUpdate likely deadlocked while m.mu was held)")
	}

	mu.Lock()
	defer mu.Unlock()
	if updates != 2 {
		t.Fatalf("onUpdate called %d times, want 2 (one per provider member)", updates)
	}
	for i := 1; i < len(sizes); i++ {
		if sizes[i] < sizes[i-1] {
			t.Fatalf("snapshot sizes observed at each onUpdate = %v, want non-decreasing growth", sizes)
		}
	}
	if sizes[len(sizes)-1] != 3 {
		t.Fatalf("snapshot size at the last onUpdate = %d, want 3 (1 + 2 sessions merged)", sizes[len(sizes)-1])
	}
	if got := mgr.Sessions(); len(got) != 3 {
		t.Fatalf("Sessions() after ReloadStreaming = %d, want 3", len(got))
	}
}

// --- ReloadStreaming completion must equal a synchronous Reload: same set,
// same sort, with and without exclusion patterns, over a DirScoped + plain
// provider mix (property test) ---

func TestReloadStreaming_EquivalentToSynchronousReload(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	base := time.Now()
	dirScopedSessions := []*model.Session{
		{SessionID: "ds-keep", CWD: "/home/user/project", LastActivity: base.Add(4 * time.Minute)},
		{SessionID: "ds-excluded", CWD: "/home/user/.claude-mem/observer-sessions/x", LastActivity: base.Add(3 * time.Minute)},
	}
	plainSessions := []*model.Session{
		{SessionID: "plain-keep", CWD: "/home/user/other-project", LastActivity: base.Add(2 * time.Minute)},
		{SessionID: "plain-excluded", CWD: "/tmp/scratch/build", LastActivity: base.Add(1 * time.Minute)},
	}

	patternSets := [][]string{
		nil,
		{},
		{".claude-mem/observer-sessions"},
		{"/tmp/scratch"},
		{".claude-mem/observer-sessions", "/tmp/scratch"},
		{"no-match-anywhere"},
	}

	for _, patterns := range patternSets {
		t.Run(strings.Join(patterns, "+"), func(t *testing.T) {
			dirScoped := &filteringDirScopedProvider{sessions: cloneSessions(dirScopedSessions)}
			plain := fakeProvider{sessions: cloneSessions(plainSessions)}
			streamProvider := MultiProvider{Providers: []SessionProvider{dirScoped, plain}}

			streamMgr := NewSessionManager(60, streamProvider)
			streamMgr.SetExcludeCWDSubstrings(patterns)
			streamMgr.ReloadStreaming(nil)

			dirScoped2 := &filteringDirScopedProvider{sessions: cloneSessions(dirScopedSessions)}
			plain2 := fakeProvider{sessions: cloneSessions(plainSessions)}
			syncProvider := MultiProvider{Providers: []SessionProvider{dirScoped2, plain2}}

			syncMgr := NewSessionManager(60, syncProvider)
			syncMgr.SetExcludeCWDSubstrings(patterns)
			if err := syncMgr.Reload(); err != nil {
				t.Fatalf("synchronous Reload failed: %v", err)
			}

			gotAll := sessionIDOrder(streamMgr.Sessions())
			wantAll := sessionIDOrder(syncMgr.Sessions())
			if !reflect.DeepEqual(gotAll, wantAll) {
				t.Fatalf("patterns=%v: ReloadStreaming Sessions() = %v, want (synchronous Reload) %v", patterns, gotAll, wantAll)
			}

			gotVisible := sessionIDOrder(streamMgr.VisibleSessions())
			wantVisible := sessionIDOrder(syncMgr.VisibleSessions())
			if !reflect.DeepEqual(gotVisible, wantVisible) {
				t.Fatalf("patterns=%v: ReloadStreaming VisibleSessions() = %v, want %v", patterns, gotVisible, wantVisible)
			}
		})
	}
}

func sessionIDOrder(sessions []*model.Session) []string {
	ids := make([]string, len(sessions))
	for i, s := range sessions {
		ids[i] = s.SessionID
	}
	return ids
}

// --- Persistence: exactly one save on completion, never mid-stream ---

func TestReloadStreaming_ExactlyOneSaveOnCompletion(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p1 := &fakeCachePersister{fakeProvider: fakeProvider{sessions: []*model.Session{{SessionID: "s1", LastActivity: time.Now()}}}}
	p2 := &fakeCachePersister{fakeProvider: fakeProvider{sessions: []*model.Session{{SessionID: "s2", LastActivity: time.Now()}}}}
	mp := MultiProvider{Providers: []SessionProvider{p1, p2}}

	mgr := NewSessionManager(60, mp)
	mgr.EnableCachePersistence(t.TempDir())

	var mu sync.Mutex
	var maxSaveCallsMidStream int
	mgr.ReloadStreaming(func() {
		mu.Lock()
		defer mu.Unlock()
		if c := p1.saveCalls + p2.saveCalls; c > maxSaveCallsMidStream {
			maxSaveCallsMidStream = c
		}
	})

	if maxSaveCallsMidStream != 0 {
		t.Fatalf("saveCalls observed during onUpdate (mid-stream) = %d, want 0 (no save before stream completion)", maxSaveCallsMidStream)
	}
	if p1.saveCalls != 1 || p2.saveCalls != 1 {
		t.Fatalf("saveCalls after completion = p1:%d p2:%d, want 1/1 (exactly one save call, dispatched to every CachePersister member)", p1.saveCalls, p2.saveCalls)
	}
	if p1.loadCalls != 1 || p2.loadCalls != 1 {
		t.Fatalf("loadCalls after completion = p1:%d p2:%d, want 1/1 (loaded once before discovery started)", p1.loadCalls, p2.loadCalls)
	}
}

// TestReloadStreaming_SavesOnceEvenWhenAllMembersFail documents the accepted
// divergence from Reload's error-gated save: DiscoverMatchingStream has no
// error channel (best-effort per member, by design), so "every member
// failed" is indistinguishable from "every member found nothing" at the
// stream level. ReloadStreaming's completion signal is "the stream
// finished", not "discovery succeeded" -- so it always saves once on
// completion.
func TestReloadStreaming_SavesOnceEvenWhenAllMembersFail(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p1 := &fakeCachePersister{fakeProvider: fakeProvider{err: errTest}}
	p2 := &fakeCachePersister{fakeProvider: fakeProvider{err: errTest}}
	mp := MultiProvider{Providers: []SessionProvider{p1, p2}}

	mgr := NewSessionManager(60, mp)
	mgr.EnableCachePersistence(t.TempDir())

	mgr.ReloadStreaming(nil)

	if got := mgr.Sessions(); len(got) != 0 {
		t.Fatalf("Sessions() after an all-members-failed stream = %d, want 0 (empty snapshot, same observable outcome as Reload's error path)", len(got))
	}
	if p1.saveCalls != 1 || p2.saveCalls != 1 {
		t.Fatalf("saveCalls = p1:%d p2:%d, want 1/1 (stream completion always saves once)", p1.saveCalls, p2.saveCalls)
	}
}

// --- Guard: UpdateActivities must not re-discover while a stream is
// in-flight; Reload must be absorbed rather than run a second, concurrent
// discovery ---

// blockingProvider's DiscoverSessions blocks on release until it is closed,
// closing entered exactly once right before blocking so a test can wait
// deterministically for "this call has started" without sleeping. calls
// counts every invocation, letting tests prove a guarded caller never
// triggers a second, concurrent discovery while one is already in flight.
type blockingProvider struct {
	mu       sync.Mutex
	calls    int
	once     sync.Once
	entered  chan struct{}
	release  chan struct{}
	sessions []*model.Session
	interval time.Duration
}

func newBlockingProvider(sessions []*model.Session, interval time.Duration) *blockingProvider {
	return &blockingProvider{
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
		sessions: sessions,
		interval: interval,
	}
}

func (p *blockingProvider) DiscoverSessions() ([]*model.Session, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return p.sessions, nil
}

func (p *blockingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *blockingProvider) UseWatcher() bool               { return false }
func (p *blockingProvider) RefreshInterval() time.Duration { return p.interval }
func (p *blockingProvider) WatchDirs() []string            { return nil }

func TestUpdateActivities_SkipsRediscoveryWhileStreamInFlight(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := newBlockingProvider([]*model.Session{{SessionID: "s1", LastActivity: time.Now()}}, time.Millisecond)
	mgr := NewSessionManager(60, p)

	streamDone := make(chan struct{})
	go func() {
		mgr.ReloadStreaming(nil)
		close(streamDone)
	}()

	select {
	case <-p.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("blockingProvider.DiscoverSessions was never entered")
	}

	// The stream's one member is now blocked mid-discovery -- streamInFlight
	// must be true. UpdateActivities' periodic re-discovery branch would
	// normally fire here (m.sessions is still empty, RefreshInterval > 0);
	// it must instead skip re-discovery and fall through to the
	// activity-only recompute (safe against an empty/partial snapshot).
	_ = mgr.UpdateActivities()

	if got := p.callCount(); got != 1 {
		t.Fatalf("provider.DiscoverSessions call count after UpdateActivities during an in-flight stream = %d, want 1 (must not trigger a second, concurrent discovery)", got)
	}

	close(p.release)

	select {
	case <-streamDone:
	case <-time.After(5 * time.Second):
		t.Fatal("ReloadStreaming did not complete after release")
	}

	if got := mgr.Sessions(); len(got) != 1 || got[0].SessionID != "s1" {
		t.Fatalf("Sessions() after stream completion = %#v, want [s1]", got)
	}
	if got := p.callCount(); got != 1 {
		t.Fatalf("final provider.DiscoverSessions call count = %d, want 1 (only the stream's own call)", got)
	}
}

func TestReload_SkippedWhileStreamInFlight(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := newBlockingProvider([]*model.Session{{SessionID: "s1", LastActivity: time.Now()}}, 0)
	mgr := NewSessionManager(60, p)

	streamDone := make(chan struct{})
	go func() {
		mgr.ReloadStreaming(nil)
		close(streamDone)
	}()

	select {
	case <-p.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("blockingProvider.DiscoverSessions was never entered")
	}

	// A watcher-triggered Reload landing mid-stream (TUI's
	// fileWatchMsg/tickMsg and tray/API's watchLoop all call Reload
	// unconditionally on every event, with no way to know a stream is
	// running) must be absorbed: return immediately, without starting a
	// second, independent discovery.
	reloadReturned := make(chan error, 1)
	go func() { reloadReturned <- mgr.Reload() }()

	select {
	case err := <-reloadReturned:
		if err != nil {
			t.Fatalf("Reload during an in-flight stream returned %v, want nil (absorbed)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Reload during an in-flight stream blocked instead of being absorbed immediately")
	}

	if got := p.callCount(); got != 1 {
		t.Fatalf("provider.DiscoverSessions call count after the absorbed Reload = %d, want 1 (Reload must not run its own discovery while a stream is in flight)", got)
	}

	close(p.release)
	select {
	case <-streamDone:
	case <-time.After(5 * time.Second):
		t.Fatal("ReloadStreaming did not complete after release")
	}

	if got := mgr.Sessions(); len(got) != 1 || got[0].SessionID != "s1" {
		t.Fatalf("Sessions() after stream completion = %#v, want [s1] (no corruption from the absorbed concurrent Reload)", got)
	}
}

// TestReload_RunsNormallyAfterStreamCompletes proves the streamInFlight
// guard is cleared once ReloadStreaming returns, so a later watcher- or
// poll-driven Reload works exactly as it always has.
func TestReload_RunsNormallyAfterStreamCompletes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := &mutableStubProvider{sessions: []*model.Session{{SessionID: "s1", LastActivity: time.Now()}}}
	mgr := NewSessionManager(60, p)
	mgr.ReloadStreaming(nil)

	p.sessions = append(p.sessions, &model.Session{SessionID: "s2", LastActivity: time.Now()})
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload after stream completion: %v", err)
	}
	if got := mgr.Sessions(); len(got) != 2 {
		t.Fatalf("Sessions() after post-stream Reload = %d, want 2 (streamInFlight must be cleared once the stream finishes)", len(got))
	}
}

// TestClose_DuringInFlightStream_DoesNotSaveAndDoesNotRace documents and
// pins "shutdown save behavior unchanged" for the specific new scenario
// ReloadStreaming introduces: the TUI never joins the goroutine running
// ReloadStreaming (see internal/ui's startStreamingLoadCmd -- the same
// pattern Task 11's picker uses for its own background discovery
// goroutine), so a user quitting mid-stream reaches Close() while
// ReloadStreaming is genuinely still running concurrently in another
// goroutine.
//
// Close() itself is untouched by this task (per the brief) -- it still only
// saves when persistDirty is true, and ReloadStreaming's single
// savePersistedCacheIfDue call happens after <-done, so persistDirty is
// never set during the stream. An early quit therefore reaches Close()
// before that call ever ran: no save happens for this run at all (not even
// a partial one) -- the previous run's persisted cache, if any, is simply
// left on disk untouched. That is safe and lossless (advisory cache, see
// CachePersister), just not a speed win for the aborted run; this test
// exists to pin that exact behavior and, under -race, prove Close() reading
// persistEnabled/persistDirty concurrently with ReloadStreaming's own
// locked mutations is race-free.
func TestClose_DuringInFlightStream_DoesNotSaveAndDoesNotRace(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := newBlockingProvider([]*model.Session{{SessionID: "s1", LastActivity: time.Now()}}, 0)
	mgr := NewSessionManager(60, p)
	mgr.EnableCachePersistence(t.TempDir())

	streamDone := make(chan struct{})
	go func() {
		mgr.ReloadStreaming(nil)
		close(streamDone)
	}()

	select {
	case <-p.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("blockingProvider.DiscoverSessions was never entered")
	}

	// Simulate the TUI's shutdown path: p.Run() has returned (the user
	// quit) and main.go calls Manager().Close() immediately, with no
	// guarantee the background ReloadStreaming goroutine has finished.
	mgr.Close()

	close(p.release)
	select {
	case <-streamDone:
	case <-time.After(5 * time.Second):
		t.Fatal("ReloadStreaming did not complete after release")
	}
}
