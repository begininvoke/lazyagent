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

			// Per-session activity state must also agree, not just the
			// session set/order (review fix round, minor 5).
			for _, id := range gotAll {
				if got, want := streamMgr.ActivityFor(id), syncMgr.ActivityFor(id); got != want {
					t.Fatalf("patterns=%v: ActivityFor(%q) = %v, want %v (streaming vs synchronous Reload)", patterns, id, got, want)
				}
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
// If secondCallSessions is set, it is returned starting from the 2nd
// DiscoverSessions call onward instead of sessions -- lets a test simulate
// "the underlying data changed between the stream's own discovery and a
// later catch-up Reload" (see TestReloadStreaming_CatchesUpAbsorbedReloadOnCompletion).
// It also implements CachePersister (loadCalls/saveCalls counters, no-op
// bodies) so persistence-focused tests can reuse this same
// blocking-discovery stub instead of a separate type.
type blockingProvider struct {
	mu                 sync.Mutex
	calls              int
	loadCalls          int
	saveCalls          int
	once               sync.Once
	entered            chan struct{}
	release            chan struct{}
	sessions           []*model.Session
	secondCallSessions []*model.Session
	interval           time.Duration
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
	calls := p.calls
	p.mu.Unlock()
	p.once.Do(func() { close(p.entered) })
	<-p.release
	if calls >= 2 && p.secondCallSessions != nil {
		return p.secondCallSessions, nil
	}
	return p.sessions, nil
}

func (p *blockingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// LoadCaches/SaveCaches implement CachePersister as no-op counters, so
// tests can assert save/load call counts on a blockingProvider directly.
func (p *blockingProvider) LoadCaches(dir string) error {
	p.mu.Lock()
	p.loadCalls++
	p.mu.Unlock()
	return nil
}

func (p *blockingProvider) SaveCaches(dir string) error {
	p.mu.Lock()
	p.saveCalls++
	p.mu.Unlock()
	return nil
}

func (p *blockingProvider) saveCallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.saveCalls
}

func (p *blockingProvider) UseWatcher() bool               { return false }
func (p *blockingProvider) RefreshInterval() time.Duration { return p.interval }
func (p *blockingProvider) WatchDirs() []string            { return nil }

var _ CachePersister = (*blockingProvider)(nil)

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
// ReloadStreaming (see internal/ui's runStreamingLoadCmd -- the same
// pattern Task 11's picker uses for its own background discovery
// goroutine), so a user quitting mid-stream reaches Close() while
// ReloadStreaming is genuinely still running concurrently in another
// goroutine.
//
// Close() itself is untouched by this task (per the brief) -- it still only
// saves when persistDirty is true, and ReloadStreaming's single
// savePersistedCacheIfDue call happens after <-done, so persistDirty is
// never set during the stream. An early quit therefore reaches Close()
// before that call ever ran, so Close() ITSELF never triggers a save here
// -- verified below via a counting stub, not just by inspection. This is
// specifically about what Close() does (nothing, while the stream is still
// in flight): nothing cancels the still-running background stream
// goroutine, so if it happens to reach its own natural completion (and
// save) before the process actually exits, that save still fires
// independently of Close() -- a completion save is not guaranteed to be
// skipped for the run as a whole, only Close()'s own attempt is. Also
// proves, under -race, that Close() reading persistEnabled/persistDirty
// concurrently with ReloadStreaming's own locked mutations is race-free.
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

	if got := p.saveCallCount(); got != 0 {
		t.Fatalf("saveCalls after Close() during an in-flight stream = %d, want 0 (Close must not save while the stream's own completion save hasn't run yet)", got)
	}

	close(p.release)
	select {
	case <-streamDone:
	case <-time.After(5 * time.Second):
		t.Fatal("ReloadStreaming did not complete after release")
	}
}

// TestReloadStreaming_CatchesUpAbsorbedReloadOnCompletion pins the
// pendingReload latch (review fix round, Important 2): a Reload absorbed
// while a stream is in flight must not be lost forever for an
// all-watcher-provider setup with no periodic RefreshInterval (e.g.
// claude/pi/grok/kimi) -- there, nothing else naturally retries it; the
// next real watcher event might not arrive for a long time, or ever, for
// that exact change. Uses blockingProvider.secondCallSessions so the
// catch-up Reload's discovery (the 2nd DiscoverSessions call) can return a
// genuinely different result than the stream's own (1st) call, proving the
// catch-up actually re-discovered rather than just re-merging stale data.
func TestReloadStreaming_CatchesUpAbsorbedReloadOnCompletion(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	now := time.Now()
	p := newBlockingProvider([]*model.Session{{SessionID: "s1", LastActivity: now}}, 0)
	p.secondCallSessions = []*model.Session{
		{SessionID: "s1", LastActivity: now},
		{SessionID: "s2", LastActivity: now.Add(time.Minute)},
	}
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

	// Absorbed mid-stream -- must be latched, not dropped.
	if err := mgr.Reload(); err != nil {
		t.Fatalf("absorbed Reload: %v", err)
	}

	close(p.release)
	select {
	case <-streamDone:
	case <-time.After(5 * time.Second):
		t.Fatal("ReloadStreaming did not complete after release")
	}

	if got := p.callCount(); got != 2 {
		t.Fatalf("provider.DiscoverSessions call count after completion = %d, want 2 (the stream's own call, plus one catch-up Reload for the absorbed watcher event)", got)
	}
	ids := sessionIDOrder(mgr.Sessions())
	if len(ids) != 2 || ids[0] != "s2" || ids[1] != "s1" {
		t.Fatalf("Sessions() after completion = %v, want [s2 s1] (the catch-up Reload's fresh discovery, most-recent-first) -- the absorbed Reload's change must not be lost", ids)
	}
}

// --- Important 1 (review fix round): streamInFlight must be true through
// the ENTIRE streaming reload, including loadPersistedCacheOnce (a
// potentially slow, multi-MB JSON parse) -- not just once discovery itself
// starts. blockingLoadProvider blocks LoadCaches (not DiscoverSessions)
// until released, isolating that specific window.

type blockingLoadProvider struct {
	mu            sync.Mutex
	discoverCalls int
	once          sync.Once
	entered       chan struct{}
	release       chan struct{}
	sessions      []*model.Session
}

func newBlockingLoadProvider(sessions []*model.Session) *blockingLoadProvider {
	return &blockingLoadProvider{
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
		sessions: sessions,
	}
}

func (p *blockingLoadProvider) DiscoverSessions() ([]*model.Session, error) {
	p.mu.Lock()
	p.discoverCalls++
	p.mu.Unlock()
	return p.sessions, nil
}

func (p *blockingLoadProvider) discoverCallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.discoverCalls
}

func (p *blockingLoadProvider) LoadCaches(dir string) error {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return nil
}

func (p *blockingLoadProvider) SaveCaches(dir string) error { return nil }

func (p *blockingLoadProvider) UseWatcher() bool               { return false }
func (p *blockingLoadProvider) RefreshInterval() time.Duration { return time.Millisecond }
func (p *blockingLoadProvider) WatchDirs() []string            { return nil }

var _ CachePersister = (*blockingLoadProvider)(nil)

// TestReloadStreaming_GuardsHoldThroughCacheLoad is the Important-1 review
// fix: with the pre-fix implementation, ReloadStreaming set streamInFlight
// only AFTER calling loadPersistedCacheOnce, so a watcher event or
// UpdateActivities tick landing during that (potentially slow) load would
// pass the guard, run its own full discovery, and race the stream's own
// per-batch appends into duplicate SessionIDs (which, over the Wails IPC
// boundary, breaks the Svelte frontend's keyed session list).
// BeginReloadStreaming fixes this by setting streamInFlight synchronously
// before returning the run func -- before ANY of the streaming reload's own
// work begins.
func TestReloadStreaming_GuardsHoldThroughCacheLoad(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := newBlockingLoadProvider([]*model.Session{{SessionID: "s1", LastActivity: time.Now()}})
	mgr := NewSessionManager(60, p)
	mgr.EnableCachePersistence(t.TempDir())

	run := mgr.BeginReloadStreaming()
	streamDone := make(chan struct{})
	go func() {
		run(nil)
		close(streamDone)
	}()

	select {
	case <-p.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("LoadCaches was never entered")
	}

	// We are inside the persisted-cache load, well before
	// DiscoverMatchingStream's own discovery has even started -- both
	// Reload and UpdateActivities must already be guarded here, not just
	// once discovery begins.
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload during the cache-load window: %v", err)
	}
	_ = mgr.UpdateActivities()

	if got := p.discoverCallCount(); got != 0 {
		t.Fatalf("DiscoverSessions call count after Reload+UpdateActivities during the cache-load window = %d, want 0 (both must be guarded before discovery even starts, not just once it does)", got)
	}

	close(p.release)
	select {
	case <-streamDone:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not complete after release")
	}

	// 2, not 1: the Reload() above was absorbed while streamInFlight was
	// true, which also latches pendingReload (Important 2) -- so its own
	// catch-up Reload legitimately runs one more discovery once the stream
	// completes. This is the two fixes working together, not a leak: had
	// the guard failed to hold through the cache-load window, the call
	// count observed *during* the window (asserted above) would already
	// have been > 0.
	if got := p.discoverCallCount(); got != 2 {
		t.Fatalf("final DiscoverSessions call count = %d, want 2 (the stream's own discovery, plus the absorbed Reload's catch-up)", got)
	}
}
