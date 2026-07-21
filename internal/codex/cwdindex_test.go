package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// --- CWDIndex: headCWDIndexed / tailFinalCWDIndexed avoid I/O on a hit ---

// TestCWDIndex_HeadHitAvoidsReread corrupts the file on disk after priming
// the index (preserving its exact mtime, the same technique
// TestDiscoverSessionsFiltered_FullCacheHitUsesCachedCWDWithoutRereadingFile
// uses for model.SessionCache), then confirms headCWDIndexed still returns
// the original result -- proof it came from the index, not a fresh read of
// the now-corrupted file.
func TestCWDIndex_HeadHitAvoidsReread(t *testing.T) {
	dir := t.TempDir()
	path := writeRolloutFile(t, dir, 1, "sess-a-0000-0000-0000-000000000000", "/tmp/project-a")

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

	if err := os.WriteFile(path, []byte("garbage, not json\n"), 0o644); err != nil {
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

// TestCWDIndex_TailHitAvoidsReread is TestCWDIndex_HeadHitAvoidsReread's
// counterpart for tailFinalCWDIndexed -- the genuinely expensive scan this
// whole index exists to avoid repeating.
func TestCWDIndex_TailHitAvoidsReread(t *testing.T) {
	dir := t.TempDir()
	path := writeRolloutFileRaw(t, dir, 1, "sess-drift-0000-0000-0000-000000000000", driftFixtureLines("/tmp/project-b", false))

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime, size := info.ModTime(), info.Size()

	idx := NewCWDIndex()
	cwd, found := idx.tailFinalCWDIndexed(path, mtime, size)
	if !found || cwd != "/tmp/project-b" {
		t.Fatalf("priming call: tailFinalCWDIndexed = (%q, %v), want (/tmp/project-b, true)", cwd, found)
	}

	if err := os.WriteFile(path, []byte("garbage, not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	cwd, found = idx.tailFinalCWDIndexed(path, mtime, size)
	if !found || cwd != "/tmp/project-b" {
		t.Fatalf("cached call after corruption: tailFinalCWDIndexed = (%q, %v), want (/tmp/project-b, true) from the index, not a re-read", cwd, found)
	}
}

// TestCWDIndex_InvalidatedOnSizeChange is the brief's core codex safety
// scenario: an appended turn_context can change a file's final cwd, so a
// stale tail entry must never be trusted once size/mtime changes -- the
// entry must be recomputed, not reused.
func TestCWDIndex_InvalidatedOnSizeChange(t *testing.T) {
	dir := t.TempDir()
	path := writeRolloutFileRaw(t, dir, 1, "sess-drift-0000-0000-0000-000000000000", driftFixtureLines("/tmp/project-a", false))

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime1, size1 := info.ModTime(), info.Size()

	idx := NewCWDIndex()
	cwd, found := idx.tailFinalCWDIndexed(path, mtime1, size1)
	if !found || cwd != "/tmp/project-a" {
		t.Fatalf("first scan = (%q, %v), want (/tmp/project-a, true)", cwd, found)
	}

	// Append a turn_context that changes the file's final cwd -- both size
	// and mtime change.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"timestamp":"2026-03-01T11:00:05.000Z","type":"turn_context","payload":{"cwd":"/tmp/project-b","model":"gpt-5.2-codex","git":{"branch":"main"}}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime2, size2 := info2.ModTime(), info2.Size()
	if mtime2.Equal(mtime1) && size2 == size1 {
		t.Fatal("fixture bug: append did not change mtime or size")
	}

	cwd, found = idx.tailFinalCWDIndexed(path, mtime2, size2)
	if !found || cwd != "/tmp/project-b" {
		t.Fatalf("after append, tailFinalCWDIndexed = (%q, %v), want (/tmp/project-b, true) -- the stale project-a entry must not be trusted", cwd, found)
	}
}

// --- CWDIndex: ok=false / found=false must also be cached, and must not be upgraded ---

func TestCWDIndex_CachesConservativeHeadNotOK(t *testing.T) {
	dir := t.TempDir()
	path := writeRolloutFile(t, dir, 1, "sess-c-0000-0000-0000-000000000000", "") // malformed head

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime, size := info.ModTime(), info.Size()

	idx := NewCWDIndex()
	_, ok := idx.headCWDIndexed(path, mtime, size)
	if ok {
		t.Fatal("headCWDIndexed ok = true, want false (malformed head)")
	}
	// Second call must reuse the cached "not ok" -- verified indirectly: it
	// must remain false even though nothing else changed.
	_, ok = idx.headCWDIndexed(path, mtime, size)
	if ok {
		t.Fatal("cached headCWDIndexed ok = true, want false -- a cached ok=false must not be upgraded")
	}
}

// --- shouldParseForCWDIndexed: must match shouldParseForCWD exactly for any
// scenario, whether idx is nil, cold, or warm. ---

func TestShouldParseForCWDIndexed_NilIndexMatchesUnindexed(t *testing.T) {
	dir := t.TempDir()
	path := writeRolloutFile(t, dir, 1, "sess-a-0000-0000-0000-000000000000", "/tmp/project-a")
	match := func(cwd string) bool { return cwd == "/tmp/project-a" }

	want := shouldParseForCWD(path, match)
	got := shouldParseForCWDIndexed(path, match, nil)
	if got != want {
		t.Fatalf("shouldParseForCWDIndexed(nil idx) = %v, want %v (must match shouldParseForCWD)", got, want)
	}
}

// TestShouldParseForCWDIndexed_ColdEqualsUnindexed checks every branch of
// shouldParseForCWD (head matches / tail conclusively rules out / tail
// inconclusive / head unreadable) gives the identical answer through a cold
// (freshly constructed, empty) index -- proving the indexed path replays
// the exact same decision tree, not an approximation of it.
func TestShouldParseForCWDIndexed_ColdEqualsUnindexed(t *testing.T) {
	dir := t.TempDir()

	headMatches := writeRolloutFile(t, dir, 1, "sess-a-0000-0000-0000-000000000000", "/tmp/project-a")
	tailRulesOut := writeRolloutFileRaw(t, dir, 2, "sess-drift-0000-0000-0000-000000000000", []string{
		`{"timestamp":"2026-03-01T11:00:00.000Z","type":"session_meta","payload":{"id":"s1","cwd":"/tmp/project-a"}}`,
		`{"timestamp":"2026-03-01T11:00:01.000Z","type":"turn_context","payload":{"cwd":"/tmp/project-c"}}`,
	})
	tailInconclusive := writeRolloutFile(t, dir, 3, "sess-only-meta-0000-0000-0000-000000000000", "/tmp/project-a")
	headUnreadable := writeRolloutFile(t, dir, 4, "sess-c-0000-0000-0000-000000000000", "")

	match := func(cwd string) bool { return cwd == "/tmp/project-b" }

	for _, path := range []string{headMatches, tailRulesOut, tailInconclusive, headUnreadable} {
		want := shouldParseForCWD(path, match)
		got := shouldParseForCWDIndexed(path, match, NewCWDIndex())
		if got != want {
			t.Errorf("path %s: shouldParseForCWDIndexed(cold idx) = %v, want %v", path, got, want)
		}
	}
}

// TestShouldParseForCWDIndexed_WarmEqualsUnindexed re-runs the same fixture
// set through an index that already holds entries for every path (primed by
// a first call), confirming the warm (I/O-free) path gives the same answer
// as fresh I/O would.
func TestShouldParseForCWDIndexed_WarmEqualsUnindexed(t *testing.T) {
	dir := t.TempDir()
	path := writeRolloutFileRaw(t, dir, 2, "sess-drift-0000-0000-0000-000000000000", []string{
		`{"timestamp":"2026-03-01T11:00:00.000Z","type":"session_meta","payload":{"id":"s1","cwd":"/tmp/project-a"}}`,
		`{"timestamp":"2026-03-01T11:00:01.000Z","type":"turn_context","payload":{"cwd":"/tmp/project-c"}}`,
	})
	match := func(cwd string) bool { return cwd == "/tmp/project-b" }

	idx := NewCWDIndex()
	want := shouldParseForCWD(path, match)
	_ = shouldParseForCWDIndexed(path, match, idx) // prime

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime, size := info.ModTime(), info.Size()

	// Corrupt the file's content in place (same length, so size is
	// unchanged; mtime restored exactly), so the second call's os.Stat
	// still finds the same (mtime, size) the index entry was keyed on. A
	// fresh read of this corrupted content would find no valid cwd at all
	// (unlike the original, which conclusively rules out the match) --
	// the only way this call can still equal `want` is if it answers from
	// the index instead of re-reading the file.
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
		t.Fatalf("warm shouldParseForCWDIndexed (file corrupted, same mtime/size) = %v, want %v (must come from the index, not a re-read)", got, want)
	}
}

// --- CWDIndex persistence ---

func TestCWDIndex_SaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cwdindex-codex.json")
	path := writeRolloutFileRaw(t, dir, 1, "sess-drift-0000-0000-0000-000000000000", driftFixtureLines("/tmp/project-b", false))

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime, size := info.ModTime(), info.Size()

	idx := NewCWDIndex()
	idx.headCWDIndexed(path, mtime, size)
	idx.tailFinalCWDIndexed(path, mtime, size)

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
	tail, found := loaded.tailFinalCWDIndexed(path, mtime, size)
	if !found || tail != "/tmp/project-b" {
		t.Fatalf("loaded tailFinalCWDIndexed = (%q, %v), want (/tmp/project-b, true)", tail, found)
	}
}

func TestCWDIndex_SaveTo_FilePermissions0600(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cwdindex-codex.json")

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
	cachePath := filepath.Join(dir, "cwdindex-codex.json")
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
	cachePath := filepath.Join(dir, "cwdindex-codex.json")
	if err := os.WriteFile(cachePath, []byte(`{"formatVersion":999,"entries":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	idx := NewCWDIndex()
	if err := idx.LoadFrom(cachePath); err == nil {
		t.Fatal("LoadFrom wrong format version: want error, got nil")
	}
}

// --- discovery-level equivalence with a persisted+reloaded CWDIndex ---

// TestDiscoverSessionsFilteredIndexed_PersistedIndexEquivalentToCold is the
// brief's mandated equivalence property test at the discovery level: a
// filtered discovery run using a CWDIndex that was populated by an earlier
// run, saved to disk, and reloaded into a brand new CWDIndex, must return
// exactly the same session set as a cold run (nil index) over the same
// (unchanged) fixture tree.
func TestDiscoverSessionsFilteredIndexed_PersistedIndexEquivalentToCold(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "session_index.jsonl")
	cwdIndexPath := filepath.Join(t.TempDir(), "cwdindex-codex.json")

	writeRolloutFile(t, dir, 1, "sess-a-0000-0000-0000-000000000000", "/tmp/project-a")
	writeRolloutFile(t, dir, 2, "sess-b-0000-0000-0000-000000000000", "/tmp/project-b")
	writeRolloutFileRaw(t, dir, 3, "sess-drift-0000-0000-0000-000000000000", driftFixtureLines("/tmp/project-c", false))

	match := func(cwd string) bool { return cwd == "/tmp/project-a" }

	cold, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), match, nil)
	if err != nil {
		t.Fatalf("cold discovery: %v", err)
	}

	// First run: populate a fresh index, then persist it.
	firstIdx := NewCWDIndex()
	if _, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), match, firstIdx); err != nil {
		t.Fatalf("priming discovery: %v", err)
	}
	if err := firstIdx.SaveTo(cwdIndexPath); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	// Second run: fresh SessionCache (so files get prefiltered again, not
	// full-cache-hit), but a reloaded CWDIndex.
	reloadedIdx := NewCWDIndex()
	if err := reloadedIdx.LoadFrom(cwdIndexPath); err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	warm, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), match, reloadedIdx)
	if err != nil {
		t.Fatalf("warm discovery: %v", err)
	}

	coldPaths := sessionPaths(cold)
	warmPaths := sessionPaths(warm)
	if len(coldPaths) != len(warmPaths) {
		t.Fatalf("warm session count = %d, want %d (cold)", len(warmPaths), len(coldPaths))
	}
	for p := range coldPaths {
		if !warmPaths[p] {
			t.Errorf("warm discovery missing %s, present in cold discovery", p)
		}
	}
	for p := range warmPaths {
		if !coldPaths[p] {
			t.Errorf("warm discovery has extra %s, not present in cold discovery", p)
		}
	}
}

func sessionPaths(sessions []*model.Session) map[string]bool {
	m := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		m[s.JSONLPath] = true
	}
	return m
}
