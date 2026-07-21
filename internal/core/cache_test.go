package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// fakeCachePersister records LoadCaches/SaveCaches calls for testing
// LoadProviderCaches/SaveProviderCaches's dispatch logic without touching
// disk.
type fakeCachePersister struct {
	fakeProvider
	loadedDir string
	savedDir  string
	loadErr   error
	saveErr   error
}

func (f *fakeCachePersister) LoadCaches(dir string) error {
	f.loadedDir = dir
	return f.loadErr
}

func (f *fakeCachePersister) SaveCaches(dir string) error {
	f.savedDir = dir
	return f.saveErr
}

var _ CachePersister = (*fakeCachePersister)(nil)

func TestLoadProviderCaches_NoopForNonImplementer(t *testing.T) {
	p := fakeProvider{sessions: nil}
	// Must not panic and must simply do nothing.
	LoadProviderCaches(p, t.TempDir())
}

func TestSaveProviderCaches_NoopForNonImplementer(t *testing.T) {
	p := fakeProvider{sessions: nil}
	SaveProviderCaches(p, t.TempDir())
}

func TestLoadProviderCaches_CallsImplementer(t *testing.T) {
	p := &fakeCachePersister{}
	dir := t.TempDir()
	LoadProviderCaches(p, dir)
	if p.loadedDir != dir {
		t.Fatalf("loadedDir = %q, want %q", p.loadedDir, dir)
	}
}

func TestSaveProviderCaches_CallsImplementer(t *testing.T) {
	p := &fakeCachePersister{}
	dir := t.TempDir()
	SaveProviderCaches(p, dir)
	if p.savedDir != dir {
		t.Fatalf("savedDir = %q, want %q", p.savedDir, dir)
	}
}

// TestLoadProviderCaches_ErrorIsSwallowed and its Save counterpart document
// that a non-nil error from LoadCaches/SaveCaches never propagates or
// panics -- the cache is advisory and must never fail discovery.
func TestLoadProviderCaches_ErrorIsSwallowed(t *testing.T) {
	p := &fakeCachePersister{loadErr: errTest}
	LoadProviderCaches(p, t.TempDir())
}

func TestSaveProviderCaches_ErrorIsSwallowed(t *testing.T) {
	p := &fakeCachePersister{saveErr: errTest}
	SaveProviderCaches(p, t.TempDir())
}

func TestLoadSaveProviderCaches_RecurseIntoMultiProvider(t *testing.T) {
	implementer := &fakeCachePersister{}
	nonImplementer := fakeProvider{}
	mp := MultiProvider{Providers: []SessionProvider{implementer, nonImplementer}}

	dir := t.TempDir()
	LoadProviderCaches(mp, dir)
	SaveProviderCaches(mp, dir)

	if implementer.loadedDir != dir {
		t.Fatalf("loadedDir = %q, want %q", implementer.loadedDir, dir)
	}
	if implementer.savedDir != dir {
		t.Fatalf("savedDir = %q, want %q", implementer.savedDir, dir)
	}
}

// --- compile-time interface assertions (also enforced alongside each
// provider in provider.go, but keeping an explicit test-side check
// documents the contract in one place) ---

var (
	_ CachePersister = (*LiveProvider)(nil)
	_ CachePersister = (*CodexProvider)(nil)
	_ CachePersister = (*PiProvider)(nil)
	_ CachePersister = (*AmpProvider)(nil)
	_ CachePersister = (*GrokProvider)(nil)
	_ CachePersister = (*KimiProvider)(nil)
)

// --- CodexProvider: real Save/Load round trip ---

func TestCodexProvider_SaveLoadCaches_RoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	sessionPath := filepath.Join(dataDir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	mtime := info.ModTime()

	p := NewCodexProvider()
	p.cache.Put(sessionPath, mtime, 10, &model.Session{SessionID: "s1", CWD: "/tmp/project-a", Agent: "codex"})

	cacheDir := filepath.Join(t.TempDir(), "cachedir")
	if err := p.SaveCaches(cacheDir); err != nil {
		t.Fatalf("SaveCaches: %v", err)
	}
	for _, name := range []string{"discovery-codex.json", "cwdindex-codex.json"} {
		if _, err := os.Stat(filepath.Join(cacheDir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}

	p2 := NewCodexProvider()
	if err := p2.LoadCaches(cacheDir); err != nil {
		t.Fatalf("LoadCaches: %v", err)
	}

	got, offset, gotMtime := p2.cache.GetIncremental(sessionPath)
	if got == nil {
		t.Fatal("GetIncremental after LoadCaches: got nil, want a full hit")
	}
	if offset != 0 || !gotMtime.Equal(mtime) || got.SessionID != "s1" {
		t.Fatalf("got = (%+v, offset=%d, mtime=%v), want SessionID=s1 offset=0 mtime=%v", got, offset, gotMtime, mtime)
	}
}

// TestCodexProvider_SaveCaches_CreatesDirWith0700 confirms SaveCaches
// creates the cache directory itself (with 0700 perms) when it doesn't
// already exist, since sessions.Run calls it without a separate mkdir step.
func TestCodexProvider_SaveCaches_CreatesDirWith0700(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "does-not-exist-yet")

	p := NewCodexProvider()
	if err := p.SaveCaches(cacheDir); err != nil {
		t.Fatalf("SaveCaches: %v", err)
	}

	info, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatalf("expected cache dir to be created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("cache dir path is not a directory")
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("cache dir perms = %o, want 0700", perm)
	}
}

// TestCodexProvider_LoadCaches_MissingDirIsAdvisory confirms LoadCaches on
// a nonexistent directory (e.g. the very first run, before any cache was
// ever saved) doesn't panic or leave the provider unusable -- it's
// best-effort.
func TestCodexProvider_LoadCaches_MissingDirIsAdvisory(t *testing.T) {
	p := NewCodexProvider()
	_ = p.LoadCaches(filepath.Join(t.TempDir(), "never-existed"))
	got, _, _ := p.cache.GetIncremental(filepath.Join(t.TempDir(), "anything.jsonl"))
	if got != nil {
		t.Fatalf("cache after missing-dir LoadCaches = %+v, want nil (cold, untouched)", got)
	}
}

// --- LiveProvider: real Save/Load round trip ---

func TestLiveProvider_SaveLoadCaches_RoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	sessionPath := filepath.Join(dataDir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	mtime := info.ModTime()

	p := NewLiveProvider(nil)
	p.cache.Put(sessionPath, mtime, 10, &model.Session{SessionID: "s1", CWD: "/tmp/project-a", Agent: "claude"})

	cacheDir := t.TempDir()
	if err := p.SaveCaches(cacheDir); err != nil {
		t.Fatalf("SaveCaches: %v", err)
	}
	for _, name := range []string{"discovery-claude.json", "cwdindex-claude.json"} {
		if _, err := os.Stat(filepath.Join(cacheDir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}

	p2 := NewLiveProvider(nil)
	if err := p2.LoadCaches(cacheDir); err != nil {
		t.Fatalf("LoadCaches: %v", err)
	}
	got, offset, gotMtime := p2.cache.GetIncremental(sessionPath)
	if got == nil || offset != 0 || !gotMtime.Equal(mtime) || got.SessionID != "s1" {
		t.Fatalf("got = (%+v, offset=%d, mtime=%v), want SessionID=s1 offset=0 mtime=%v", got, offset, gotMtime, mtime)
	}
}

// --- Pi/Amp/Grok/Kimi: file naming + round trip (they only wrap a plain
// model.SessionCache, no cwd index) ---

func TestPiProvider_SaveLoadCaches_RoundTrip(t *testing.T) {
	p1, p2 := NewPiProvider(), NewPiProvider()
	testSessionCacheOnlyProviderRoundTrip(t, "discovery-pi.json", p1, p1.cache, p2, p2.cache)
}

func TestAmpProvider_SaveLoadCaches_RoundTrip(t *testing.T) {
	p1, p2 := NewAmpProvider(), NewAmpProvider()
	testSessionCacheOnlyProviderRoundTrip(t, "discovery-amp.json", p1, p1.cache, p2, p2.cache)
}

func TestGrokProvider_SaveLoadCaches_RoundTrip(t *testing.T) {
	p1, p2 := NewGrokProvider(), NewGrokProvider()
	testSessionCacheOnlyProviderRoundTrip(t, "discovery-grok.json", p1, p1.cache, p2, p2.cache)
}

func TestKimiProvider_SaveLoadCaches_RoundTrip(t *testing.T) {
	p1, p2 := NewKimiProvider(), NewKimiProvider()
	testSessionCacheOnlyProviderRoundTrip(t, "discovery-kimi.json", p1, p1.cache, p2, p2.cache)
}

func testSessionCacheOnlyProviderRoundTrip(t *testing.T, wantFile string, p1 CachePersister, cache1 *model.SessionCache, p2 CachePersister, cache2 *model.SessionCache) {
	t.Helper()
	dataDir := t.TempDir()
	sessionPath := filepath.Join(dataDir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	mtime := info.ModTime()

	cache1.Put(sessionPath, mtime, 10, &model.Session{SessionID: "s1"})

	cacheDir := t.TempDir()
	if err := p1.SaveCaches(cacheDir); err != nil {
		t.Fatalf("SaveCaches: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, wantFile)); err != nil {
		t.Fatalf("expected %s to exist: %v", wantFile, err)
	}

	if err := p2.LoadCaches(cacheDir); err != nil {
		t.Fatalf("LoadCaches: %v", err)
	}
	got, offset, gotMtime := cache2.GetIncremental(sessionPath)
	if got == nil || offset != 0 || !gotMtime.Equal(mtime) || got.SessionID != "s1" {
		t.Fatalf("got = (%+v, offset=%d, mtime=%v), want SessionID=s1 offset=0 mtime=%v", got, offset, gotMtime, mtime)
	}
}
