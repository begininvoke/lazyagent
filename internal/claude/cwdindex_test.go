package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func claudeSessionPaths(sessions []*model.Session) map[string]bool {
	m := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		m[s.JSONLPath] = true
	}
	return m
}

// TestCWDIndex_HeadHitAvoidsReread corrupts the file on disk after priming
// the index (preserving its exact mtime/size), then confirms
// headCWDIndexed still returns the original result -- proof it came from
// the index, not a fresh read of the now-corrupted file. Mirrors codex's
// equivalent test and the existing
// TestDiscoverSessionsFiltered_FullCacheHitUsesCachedCWDWithoutRereadingFile
// technique used elsewhere in this package.
func TestCWDIndex_HeadHitAvoidsReread(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(cwdLine("/tmp/project-a")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime, size := info.ModTime(), info.Size()

	idx := NewCWDIndex()
	cwd, ok := idx.headCWDIndexed(path, mtime, size)
	if !ok || cwd != "/tmp/project-a" {
		t.Fatalf("priming call: headCWDIndexed = (%q, %v), want (/tmp/project-a, true)", cwd, ok)
	}

	garbage := make([]byte, size)
	for i := range garbage {
		garbage[i] = 'x'
	}
	if err := os.WriteFile(path, garbage, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	cwd, ok = idx.headCWDIndexed(path, mtime, size)
	if !ok || cwd != "/tmp/project-a" {
		t.Fatalf("cached call after corruption: headCWDIndexed = (%q, %v), want (/tmp/project-a, true) from the index, not a re-read", cwd, ok)
	}
}

// TestCWDIndex_InvalidatedOnChange confirms a changed file (different mtime
// and size) is never trusted from a stale entry -- the brief's "use the
// same invalidate-on-change rule for uniformity and safety" requirement for
// claude, even though first-cwd-wins is technically stable across appends.
func TestCWDIndex_InvalidatedOnChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(cwdLine("/tmp/project-a")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime1, size1 := info.ModTime(), info.Size()

	idx := NewCWDIndex()
	cwd, ok := idx.headCWDIndexed(path, mtime1, size1)
	if !ok || cwd != "/tmp/project-a" {
		t.Fatalf("first scan = (%q, %v), want (/tmp/project-a, true)", cwd, ok)
	}

	// Replace the file with new (different-length) content under a new cwd.
	if err := os.WriteFile(path, []byte(cwdLine("/tmp/project-b")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime2, size2 := info2.ModTime(), info2.Size()

	cwd, ok = idx.headCWDIndexed(path, mtime2, size2)
	if !ok || cwd != "/tmp/project-b" {
		t.Fatalf("after replace, headCWDIndexed = (%q, %v), want (/tmp/project-b, true) -- the stale project-a entry must not be trusted", cwd, ok)
	}
}

func TestCWDIndex_CachesConservativeNotOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(noCWDLine(1)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime, size := info.ModTime(), info.Size()

	idx := NewCWDIndex()
	_, ok := idx.headCWDIndexed(path, mtime, size)
	if ok {
		t.Fatal("headCWDIndexed ok = true, want false (no cwd in window)")
	}
	_, ok = idx.headCWDIndexed(path, mtime, size)
	if ok {
		t.Fatal("cached headCWDIndexed ok = true, want false -- a cached ok=false must not be upgraded")
	}
}

// --- shouldParseForCWDIndexed must match shouldParseForCWD exactly ---

func TestShouldParseForCWDIndexed_NilIndexMatchesUnindexed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(cwdLine("/tmp/project-a")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	match := func(cwd string) bool { return cwd == "/tmp/project-a" }

	want := shouldParseForCWD(path, match)
	got := shouldParseForCWDIndexed(path, match, nil)
	if got != want {
		t.Fatalf("shouldParseForCWDIndexed(nil idx) = %v, want %v", got, want)
	}
}

func TestShouldParseForCWDIndexed_ColdEqualsUnindexed(t *testing.T) {
	dir := t.TempDir()

	headMatches := filepath.Join(dir, "a.jsonl")
	os.WriteFile(headMatches, []byte(cwdLine("/tmp/project-a")+"\n"), 0o644)
	headRulesOut := filepath.Join(dir, "b.jsonl")
	os.WriteFile(headRulesOut, []byte(cwdLine("/tmp/project-a")+"\n"), 0o644)
	noCWDInWindow := filepath.Join(dir, "c.jsonl")
	os.WriteFile(noCWDInWindow, []byte(noCWDLine(1)+"\n"), 0o644)

	match := func(cwd string) bool { return cwd == "/tmp/project-b" }

	for _, path := range []string{headMatches, headRulesOut, noCWDInWindow} {
		want := shouldParseForCWD(path, match)
		got := shouldParseForCWDIndexed(path, match, NewCWDIndex())
		if got != want {
			t.Errorf("path %s: shouldParseForCWDIndexed(cold idx) = %v, want %v", path, got, want)
		}
	}
}

func TestShouldParseForCWDIndexed_WarmEqualsUnindexed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(cwdLine("/tmp/project-a")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	match := func(cwd string) bool { return cwd == "/tmp/project-b" }

	idx := NewCWDIndex()
	want := shouldParseForCWD(path, match)
	_ = shouldParseForCWDIndexed(path, match, idx) // prime

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime, size := info.ModTime(), info.Size()
	garbage := make([]byte, size)
	for i := range garbage {
		garbage[i] = 'x'
	}
	if err := os.WriteFile(path, garbage, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	got := shouldParseForCWDIndexed(path, match, idx)
	if got != want {
		t.Fatalf("warm shouldParseForCWDIndexed (file corrupted, same mtime/size) = %v, want %v", got, want)
	}
}

// --- persistence ---

func TestCWDIndex_SaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cwdindex-claude.json")
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(cwdLine("/tmp/project-a")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime, size := info.ModTime(), info.Size()

	idx := NewCWDIndex()
	idx.headCWDIndexed(path, mtime, size)
	if err := idx.SaveTo(cachePath); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	loaded := NewCWDIndex()
	if err := loaded.LoadFrom(cachePath); err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	cwd, ok := loaded.headCWDIndexed(path, mtime, size)
	if !ok || cwd != "/tmp/project-a" {
		t.Fatalf("loaded headCWDIndexed = (%q, %v), want (/tmp/project-a, true)", cwd, ok)
	}
}

func TestCWDIndex_SaveTo_FilePermissions0600(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cwdindex-claude.json")

	idx := NewCWDIndex()
	if err := idx.SaveTo(cachePath); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("perms = %o, want 0600", perm)
	}
}

func TestCWDIndex_LoadFrom_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cwdindex-claude.json")
	if err := os.WriteFile(cachePath, []byte("{not valid"), 0o600); err != nil {
		t.Fatal(err)
	}

	idx := NewCWDIndex()
	if err := idx.LoadFrom(cachePath); err == nil {
		t.Fatal("LoadFrom corrupt JSON: want error, got nil")
	}
}

func TestCWDIndex_LoadFrom_WrongFormatVersion(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cwdindex-claude.json")
	if err := os.WriteFile(cachePath, []byte(`{"formatVersion":999,"entries":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	idx := NewCWDIndex()
	if err := idx.LoadFrom(cachePath); err == nil {
		t.Fatal("LoadFrom wrong format version: want error, got nil")
	}
}

// --- discovery-level equivalence with a persisted+reloaded CWDIndex ---

func TestDiscoverSessionsFilteredIndexed_PersistedIndexEquivalentToCold(t *testing.T) {
	configDir, projectsDir := newTestProjectsRoot(t)
	writeClaudeSession(t, projectsDir, "dir-a", "sess-a", []string{cwdLine("/tmp/project-a")})
	writeClaudeSession(t, projectsDir, "dir-b", "sess-b", []string{cwdLine("/tmp/project-b")})
	writeClaudeSession(t, projectsDir, "dir-c", "sess-c", []string{noCWDLine(1)})

	match := func(cwd string) bool { return cwd == "/tmp/project-a" }
	cwdIndexPath := filepath.Join(t.TempDir(), "cwdindex-claude.json")

	cold, err := DiscoverSessionsFilteredIndexed(model.NewSessionCache(), NewDesktopCache(), []string{configDir}, match, nil)
	if err != nil {
		t.Fatalf("cold discovery: %v", err)
	}

	firstIdx := NewCWDIndex()
	if _, err := DiscoverSessionsFilteredIndexed(model.NewSessionCache(), NewDesktopCache(), []string{configDir}, match, firstIdx); err != nil {
		t.Fatalf("priming discovery: %v", err)
	}
	if err := firstIdx.SaveTo(cwdIndexPath); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	reloadedIdx := NewCWDIndex()
	if err := reloadedIdx.LoadFrom(cwdIndexPath); err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	warm, err := DiscoverSessionsFilteredIndexed(model.NewSessionCache(), NewDesktopCache(), []string{configDir}, match, reloadedIdx)
	if err != nil {
		t.Fatalf("warm discovery: %v", err)
	}

	coldPaths := claudeSessionPaths(cold)
	warmPaths := claudeSessionPaths(warm)
	if len(coldPaths) != len(warmPaths) {
		t.Fatalf("warm session count = %d, want %d (cold)", len(warmPaths), len(coldPaths))
	}
	for p := range coldPaths {
		if !warmPaths[p] {
			t.Errorf("warm discovery missing %s, present in cold discovery", p)
		}
	}
}
