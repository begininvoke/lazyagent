package core

import (
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// --- EnableCachePersistence: load-once-before-first-discovery ---

func TestEnableCachePersistence_LoadsOnceBeforeFirstDiscovery(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := &fakeCachePersister{fakeProvider: fakeProvider{
		sessions: []*model.Session{{SessionID: "s1", LastActivity: time.Now()}},
	}}
	dir := t.TempDir()

	mgr := NewSessionManager(60, p)
	mgr.EnableCachePersistence(dir)

	if p.loadCalls != 0 {
		t.Fatalf("loadCalls before any Reload = %d, want 0", p.loadCalls)
	}

	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload 1: %v", err)
	}
	if p.loadCalls != 1 {
		t.Fatalf("loadCalls after first Reload = %d, want 1", p.loadCalls)
	}
	if p.loadedDir != dir {
		t.Fatalf("loadedDir = %q, want %q", p.loadedDir, dir)
	}

	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload 2: %v", err)
	}
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload 3: %v", err)
	}
	if p.loadCalls != 1 {
		t.Fatalf("loadCalls after 3 Reloads = %d, want 1 (load must happen only before the FIRST discovery)", p.loadCalls)
	}
}

// --- EnableCachePersistence: save after first successful discovery ---

func TestEnableCachePersistence_SavesAfterFirstSuccessfulDiscovery(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := &fakeCachePersister{fakeProvider: fakeProvider{
		sessions: []*model.Session{{SessionID: "s1", LastActivity: time.Now()}},
	}}
	dir := t.TempDir()

	mgr := NewSessionManager(60, p)
	mgr.EnableCachePersistence(dir)

	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if p.saveCalls != 1 {
		t.Fatalf("saveCalls after first successful Reload = %d, want 1", p.saveCalls)
	}
	if p.savedDir != dir {
		t.Fatalf("savedDir = %q, want %q", p.savedDir, dir)
	}
}

// --- EnableCachePersistence: no save on a failed first discovery ---

func TestEnableCachePersistence_NoSaveOnFailedFirstDiscovery(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := &fakeCachePersister{fakeProvider: fakeProvider{err: errTest}}
	dir := t.TempDir()

	mgr := NewSessionManager(60, p)
	mgr.EnableCachePersistence(dir)

	if err := mgr.Reload(); err == nil {
		t.Fatal("Reload with a failing provider: got nil error, want errTest")
	}
	// Load still happens (it runs before discovery is even attempted)...
	if p.loadCalls != 1 {
		t.Fatalf("loadCalls after a failed Reload = %d, want 1", p.loadCalls)
	}
	// ...but Save must never run off the back of a failed discovery.
	if p.saveCalls != 0 {
		t.Fatalf("saveCalls after a failed Reload = %d, want 0", p.saveCalls)
	}
}

// --- EnableCachePersistence: dirty + minimum-interval save logic ---

func TestEnableCachePersistence_DirtyIntervalLogic(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := &fakeCachePersister{fakeProvider: fakeProvider{
		sessions: []*model.Session{{SessionID: "s1", LastActivity: time.Now()}},
	}}
	dir := t.TempDir()

	mgr := NewSessionManager(60, p)
	mgr.EnableCachePersistence(dir)

	fakeNow := time.Now()
	mgr.persistNow = func() time.Time { return fakeNow }

	// First successful Reload always saves, regardless of the interval.
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload 1: %v", err)
	}
	if p.saveCalls != 1 {
		t.Fatalf("saveCalls after Reload 1 = %d, want 1 (first save is unconditional)", p.saveCalls)
	}

	// A Reload well within the minimum interval marks the cache dirty but
	// must not trigger another disk write.
	fakeNow = fakeNow.Add(1 * time.Minute)
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload 2: %v", err)
	}
	if p.saveCalls != 1 {
		t.Fatalf("saveCalls after Reload 2 (within interval) = %d, want 1 (still dirty, not due)", p.saveCalls)
	}

	// Once the interval has elapsed, the next successful Reload saves again.
	fakeNow = fakeNow.Add(cacheSaveInterval)
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload 3: %v", err)
	}
	if p.saveCalls != 2 {
		t.Fatalf("saveCalls after Reload 3 (interval elapsed) = %d, want 2", p.saveCalls)
	}
}

// --- Close(): final save if dirty, no-op otherwise ---

func TestClose_FinalSaveWhenDirty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := &fakeCachePersister{fakeProvider: fakeProvider{
		sessions: []*model.Session{{SessionID: "s1", LastActivity: time.Now()}},
	}}
	dir := t.TempDir()

	mgr := NewSessionManager(60, p)
	mgr.EnableCachePersistence(dir)

	fakeNow := time.Now()
	mgr.persistNow = func() time.Time { return fakeNow }

	if err := mgr.Reload(); err != nil { // saves (first), dirty -> false
		t.Fatalf("Reload 1: %v", err)
	}
	fakeNow = fakeNow.Add(1 * time.Minute)
	if err := mgr.Reload(); err != nil { // within interval, dirty -> true
		t.Fatalf("Reload 2: %v", err)
	}
	if p.saveCalls != 1 {
		t.Fatalf("saveCalls before Close = %d, want 1", p.saveCalls)
	}

	mgr.Close()

	if p.saveCalls != 2 {
		t.Fatalf("saveCalls after Close (was dirty) = %d, want 2 (final save)", p.saveCalls)
	}
}

func TestClose_NoFinalSaveWhenNotDirty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := &fakeCachePersister{fakeProvider: fakeProvider{
		sessions: []*model.Session{{SessionID: "s1", LastActivity: time.Now()}},
	}}
	dir := t.TempDir()

	mgr := NewSessionManager(60, p)
	mgr.EnableCachePersistence(dir)

	if err := mgr.Reload(); err != nil { // saves once, dirty -> false
		t.Fatalf("Reload: %v", err)
	}
	if p.saveCalls != 1 {
		t.Fatalf("saveCalls before Close = %d, want 1", p.saveCalls)
	}

	mgr.Close()

	if p.saveCalls != 1 {
		t.Fatalf("saveCalls after Close (was clean) = %d, want 1 (no redundant final save)", p.saveCalls)
	}
}

func TestClose_NoopWhenPersistenceNeverEnabled(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := &fakeCachePersister{fakeProvider: fakeProvider{
		sessions: []*model.Session{{SessionID: "s1", LastActivity: time.Now()}},
	}}

	mgr := NewSessionManager(60, p)
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	mgr.Close() // must not panic and must not save

	if p.loadCalls != 0 || p.saveCalls != 0 {
		t.Fatalf("loadCalls=%d saveCalls=%d, want 0/0 when persistence was never enabled", p.loadCalls, p.saveCalls)
	}
}

// --- Persistence disabled entirely: no load, no save ---

func TestPersistence_DisabledByDefault_NoLoadNoSave(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := &fakeCachePersister{fakeProvider: fakeProvider{
		sessions: []*model.Session{{SessionID: "s1", LastActivity: time.Now()}},
	}}

	mgr := NewSessionManager(60, p) // EnableCachePersistence never called
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if p.loadCalls != 0 {
		t.Fatalf("loadCalls = %d, want 0 (persistence never enabled)", p.loadCalls)
	}
	if p.saveCalls != 0 {
		t.Fatalf("saveCalls = %d, want 0 (persistence never enabled)", p.saveCalls)
	}
}

// --- Non-persister provider: enabling persistence is a clean no-op ---

func TestEnableCachePersistence_NonPersisterProviderIsCleanNoop(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := fakeProvider{sessions: []*model.Session{{SessionID: "s1", LastActivity: time.Now()}}}
	dir := t.TempDir()

	mgr := NewSessionManager(60, p)
	mgr.EnableCachePersistence(dir) // p does not implement CachePersister

	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload 2: %v", err)
	}
	mgr.Close()

	// Nothing to assert on p directly (it has no counters), but discovery
	// must still have worked normally through all of this.
	if got := mgr.Sessions(); len(got) != 1 || got[0].SessionID != "s1" {
		t.Fatalf("Sessions() = %+v, want the single fakeProvider session untouched by persistence", got)
	}
}
