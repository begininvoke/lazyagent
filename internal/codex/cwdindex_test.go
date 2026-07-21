package codex

import (
	"encoding/json"
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

// TestCWDIndex_PruneRemovesDeletedFileEntries covers a full discovery+save
// cycle: a file that gets skipped (non-matching cwd) populates a cwd-index
// entry; once that file is deleted from the rollout tree, the next
// discovery pass must prune its now-stale entry so a save afterward doesn't
// persist it forever (codex rotates/prunes old sessions, so this isn't a
// hypothetical -- without pruning, the index would grow unboundedly with
// entries for files that no longer exist).
func TestCWDIndex_PruneRemovesDeletedFileEntries(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "session_index.jsonl")
	cwdIndexPath := filepath.Join(t.TempDir(), "cwdindex-codex.json")
	match := func(cwd string) bool { return cwd == "/tmp/match" }

	pathToDelete := writeRolloutFile(t, dir, 1, "sess-prune-0000-0000-000000000000", "/tmp/other")

	cwdIdx := NewCWDIndex()
	if _, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), match, cwdIdx); err != nil {
		t.Fatalf("run1: %v", err)
	}
	if err := cwdIdx.SaveTo(cwdIndexPath); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	if err := os.Remove(pathToDelete); err != nil {
		t.Fatal(err)
	}

	reloaded := NewCWDIndex()
	if err := reloaded.LoadFrom(cwdIndexPath); err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if _, ok := reloaded.entries[pathToDelete]; !ok {
		t.Fatal("expected the reloaded index to still have an entry for the (now deleted) file before this run's discovery prunes it")
	}

	if _, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), match, reloaded); err != nil {
		t.Fatalf("run2 (over the mutated tree, file deleted): %v", err)
	}
	if err := reloaded.SaveTo(cwdIndexPath); err != nil {
		t.Fatalf("SaveTo after prune: %v", err)
	}

	final := NewCWDIndex()
	if err := final.LoadFrom(cwdIndexPath); err != nil {
		t.Fatalf("LoadFrom final: %v", err)
	}
	if _, ok := final.entries[pathToDelete]; ok {
		t.Fatal("expected the deleted file's entry to be pruned from the persisted index after the discovery+save cycle")
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

// errorfer is the minimal subset of *testing.T that sessionDigest and
// assertSameSessionSet need. It exists (instead of taking *testing.T
// directly) so TestAssertSameSessionSet_CatchesStaleContentAtSamePath can
// pass a non-*testing.T fake that records a failure without that failure
// propagating to the test currently running assertSameSessionSet's own
// test — testing.TB can't be used for this, since it has an unexported
// method that only the standard library's *testing.T/B/F can implement.
// Every real call site passes a plain *testing.T, which satisfies this
// smaller interface implicitly.
type errorfer interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// sessionDigest renders s as JSON for content comparison. Session has no
// fields populated from wall-clock time (every timestamp on it comes from
// parsed JSONL content via time.Parse, never time.Now()), so there are no
// legitimately-nondeterministic fields to normalize before comparing two
// independently-produced sessions for the same file — a byte-different
// digest here always means a genuine content difference, not incidental
// representation noise (e.g. time.Time's internal monotonic-reading
// representation, which reflect.DeepEqual is sensitive to but JSON
// marshaling normalizes away, since it only ever encodes the RFC3339Nano
// wall-clock value).
func sessionDigest(t errorfer, s *model.Session) string {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal session for digest: %v", err)
	}
	return string(data)
}

// assertSameSessionSet fails t if cold and got don't contain sessions with
// the exact same JSONLPaths AND, for each shared path, byte-identical
// content. A path-only comparison would pass a stale-content-same-path
// regression (a persisted entry served for a file whose content actually
// changed) undetected, since both sides would agree on which files are
// present without ever checking what was returned for them.
func assertSameSessionSet(t errorfer, label string, cold, got []*model.Session) {
	t.Helper()
	coldByPath := make(map[string]*model.Session, len(cold))
	for _, s := range cold {
		coldByPath[s.JSONLPath] = s
	}
	gotByPath := make(map[string]*model.Session, len(got))
	for _, s := range got {
		gotByPath[s.JSONLPath] = s
	}
	if len(coldByPath) != len(gotByPath) {
		t.Errorf("%s: session count = %d, want %d", label, len(gotByPath), len(coldByPath))
	}
	for p, cs := range coldByPath {
		gs, ok := gotByPath[p]
		if !ok {
			t.Errorf("%s: missing %s (present in cold discovery)", label, p)
			continue
		}
		if wantDigest, gotDigest := sessionDigest(t, cs), sessionDigest(t, gs); wantDigest != gotDigest {
			t.Errorf("%s: session content for %s differs from cold discovery\ncold: %s\ngot:  %s", label, p, wantDigest, gotDigest)
		}
	}
	for p := range gotByPath {
		if _, ok := coldByPath[p]; !ok {
			t.Errorf("%s: unexpected extra %s (absent from cold discovery)", label, p)
		}
	}
}

// fakeErrorfer records whether Errorf/Fatalf was called, without failing
// the test actually running -- used only to verify that assertSameSessionSet
// itself reports a failure for a given input, since a real *testing.T's
// Errorf would otherwise mark the enclosing test (and, via t.Run's
// propagation, any parent) failed regardless of what's checked afterward.
type fakeErrorfer struct {
	failed bool
}

func (f *fakeErrorfer) Helper()                           {}
func (f *fakeErrorfer) Errorf(format string, args ...any) { f.failed = true }
func (f *fakeErrorfer) Fatalf(format string, args ...any) { f.failed = true }

// TestAssertSameSessionSet_CatchesStaleContentAtSamePath proves the content
// check added to assertSameSessionSet actually earns its keep: a
// same-path-but-different-content regression (the exact bug class df89efa
// fixed -- a stale cached session served for a file whose real content
// changed, without necessarily changing which files are present at all) is
// invisible to a JSONLPath-set-only comparison, since both sides agree on
// which paths are present. It manufactures two sessions at the identical
// path with different field values directly (not via real discovery) so
// this is deterministic and independent of any particular fixture mutation
// happening to also change match status.
func TestAssertSameSessionSet_CatchesStaleContentAtSamePath(t *testing.T) {
	cold := []*model.Session{
		{JSONLPath: "/tmp/x.jsonl", SessionID: "s1", CWD: "/tmp/match", TotalMessages: 5},
	}
	stale := []*model.Session{
		{JSONLPath: "/tmp/x.jsonl", SessionID: "s1-STALE", CWD: "/tmp/match", TotalMessages: 1},
	}

	fake := &fakeErrorfer{}
	assertSameSessionSet(fake, "simulated", cold, stale)
	if !fake.failed {
		t.Fatal("assertSameSessionSet did not detect a same-path, different-content regression -- the content-digest check added to it is not actually catching this bug class")
	}

	// Sanity check the other direction too: identical content at the same
	// path must NOT be flagged, so the fake and the check itself are both
	// wired correctly (not just always reporting failed).
	fakeOK := &fakeErrorfer{}
	assertSameSessionSet(fakeOK, "simulated", cold, cold)
	if fakeOK.failed {
		t.Fatal("assertSameSessionSet flagged identical content as a mismatch")
	}
}

// TestDiscoverSessionsFilteredIndexed_ComprehensiveEquivalence is the
// brief's mandated end-to-end equivalence property test, covering every
// required fixture-tree scenario in one pass: discovery using a
// persisted-and-reloaded SessionCache + CWDIndex must match cold discovery
// for unchanged files, an appended file whose final cwd changes (the
// cwd-index's core codex-specific safety requirement -- the stale entry
// must be invalidated by the size change and re-scanned), a
// truncated/replaced file with the same mtime but a different size, a
// truncated/replaced file with a different mtime, a deleted file, a
// corrupted persisted cache/index JSON, and a wrong-formatVersion persisted
// cache/index JSON.
func TestDiscoverSessionsFilteredIndexed_ComprehensiveEquivalence(t *testing.T) {
	match := func(cwd string) bool { return cwd == "/tmp/match" }

	dir := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "session_index.jsonl")
	paths := map[string]string{
		"unchanged_match":    writeRolloutFile(t, dir, 1, "sess-unchanged-match-000000000000", "/tmp/match"),
		"unchanged_nonmatch": writeRolloutFile(t, dir, 2, "sess-unchanged-nonmatch-00000000000", "/tmp/other"),
		"appended": writeRolloutFileRaw(t, dir, 3, "sess-appended-0000-0000-000000000000", []string{
			`{"timestamp":"2026-03-03T11:00:00.000Z","type":"session_meta","payload":{"id":"sess-appended","cwd":"/tmp/other-a"}}`,
			`{"timestamp":"2026-03-03T11:00:01.000Z","type":"turn_context","payload":{"cwd":"/tmp/other-b"}}`,
		}),
		"truncated_same_mtime": writeRolloutFile(t, dir, 4, "sess-trunc-same-0000-0000000000000", "/tmp/other"),
		"truncated_diff_mtime": writeRolloutFile(t, dir, 5, "sess-trunc-diff-0000-0000000000000", "/tmp/other"),
		"deleted":              writeRolloutFile(t, dir, 6, "sess-deleted-0000-0000-000000000000", "/tmp/other"),
	}

	// Run 1 (cold): populate both caches over the ORIGINAL fixture tree,
	// then persist them.
	cache := model.NewSessionCache()
	cwdIdx := NewCWDIndex()
	if _, err := discoverSessionsFromDir(dir, indexPath, cache, match, cwdIdx); err != nil {
		t.Fatalf("run1: %v", err)
	}
	cacheDir := t.TempDir()
	discoveryPath := filepath.Join(cacheDir, "discovery-codex.json")
	cwdIndexPath := filepath.Join(cacheDir, "cwdindex-codex.json")
	if err := cache.SaveTo(discoveryPath); err != nil {
		t.Fatalf("SaveTo cache: %v", err)
	}
	if err := cwdIdx.SaveTo(cwdIndexPath); err != nil {
		t.Fatalf("SaveTo cwdIdx: %v", err)
	}

	// Mutate the fixture tree between runs, one mutation per scenario.
	f, err := os.OpenFile(paths["appended"], os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"timestamp":"2026-03-03T11:00:02.000Z","type":"turn_context","payload":{"cwd":"/tmp/match"}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	infoSame, err := os.Stat(paths["truncated_same_mtime"])
	if err != nil {
		t.Fatal(err)
	}
	origMtime := infoSame.ModTime()
	if err := os.WriteFile(paths["truncated_same_mtime"], []byte(`{"timestamp":"2026-03-04T11:00:00.000Z","type":"session_meta","payload":{"id":"sess-trunc-same","cwd":"/tmp/match"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(paths["truncated_same_mtime"], origMtime, origMtime); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(paths["truncated_diff_mtime"], []byte(`{"timestamp":"2026-03-05T11:00:00.000Z","type":"session_meta","payload":{"id":"sess-trunc-diff","cwd":"/tmp/match"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(paths["deleted"]); err != nil {
		t.Fatal(err)
	}

	// Cold baseline: a completely fresh discovery over the mutated tree.
	cold, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), match, nil)
	if err != nil {
		t.Fatalf("cold run: %v", err)
	}

	// Warm: reload both persisted caches (from the ORIGINAL, pre-mutation
	// state) and rerun over the mutated tree.
	reloadedCache := model.NewSessionCache()
	if err := reloadedCache.LoadFrom(discoveryPath); err != nil {
		t.Fatalf("LoadFrom cache: %v", err)
	}
	reloadedIdx := NewCWDIndex()
	if err := reloadedIdx.LoadFrom(cwdIndexPath); err != nil {
		t.Fatalf("LoadFrom cwdIdx: %v", err)
	}
	warm, err := discoverSessionsFromDir(dir, indexPath, reloadedCache, match, reloadedIdx)
	if err != nil {
		t.Fatalf("warm run: %v", err)
	}

	assertSameSessionSet(t, "warm-vs-cold after mutations", cold, warm)

	warmPaths := sessionPaths(warm)
	if !warmPaths[paths["appended"]] {
		t.Errorf("appended file (final cwd changed to match) missing from warm results -- the stale non-matching cwd-index entry must be invalidated by the size change and re-scanned")
	}
	if !warmPaths[paths["truncated_same_mtime"]] {
		t.Errorf("truncated (same mtime, different size) file missing from warm results")
	}
	if !warmPaths[paths["truncated_diff_mtime"]] {
		t.Errorf("truncated (different mtime) file missing from warm results")
	}
	if warmPaths[paths["deleted"]] {
		t.Errorf("deleted file unexpectedly present in warm results")
	}

	// A corrupted or version-mismatched persisted file (cache or cwd index)
	// must behave exactly like starting fully cold -- never error, never
	// leak stale data -- verified against the same mutated-tree cold
	// baseline computed above.
	corruptionCases := []struct {
		name string
		path string
	}{
		{"corrupted_cache_json", discoveryPath},
		{"corrupted_cwdindex_json", cwdIndexPath},
	}
	for _, tc := range corruptionCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(tc.path, []byte("{not valid json"), 0o600); err != nil {
				t.Fatal(err)
			}
			c := model.NewSessionCache()
			idx := NewCWDIndex()
			var loadErr error
			if tc.path == discoveryPath {
				loadErr = c.LoadFrom(discoveryPath)
			} else {
				loadErr = idx.LoadFrom(cwdIndexPath)
			}
			if loadErr == nil {
				t.Fatal("LoadFrom corrupted JSON: want error, got nil")
			}
			got, err := discoverSessionsFromDir(dir, indexPath, c, match, idx)
			if err != nil {
				t.Fatalf("discovery: %v", err)
			}
			assertSameSessionSet(t, tc.name, cold, got)
		})
	}

	versionCases := []struct {
		name string
		path string
	}{
		{"wrong_format_version_cache", discoveryPath},
		{"wrong_format_version_cwdindex", cwdIndexPath},
	}
	for _, tc := range versionCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(tc.path, []byte(`{"formatVersion":999,"entries":{}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			c := model.NewSessionCache()
			idx := NewCWDIndex()
			var loadErr error
			if tc.path == discoveryPath {
				loadErr = c.LoadFrom(discoveryPath)
			} else {
				loadErr = idx.LoadFrom(cwdIndexPath)
			}
			if loadErr == nil {
				t.Fatal("LoadFrom wrong format version: want error, got nil")
			}
			got, err := discoverSessionsFromDir(dir, indexPath, c, match, idx)
			if err != nil {
				t.Fatalf("discovery: %v", err)
			}
			assertSameSessionSet(t, tc.name, cold, got)
		})
	}
}
