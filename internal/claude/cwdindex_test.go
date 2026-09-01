package claude

import (
	"encoding/json"
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
	const replacementCWD = "/tmp/project-b-longer"

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
	if err := os.WriteFile(path, []byte(cwdLine(replacementCWD)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime2, size2 := info2.ModTime(), info2.Size()
	if size2 == size1 {
		t.Fatalf("replacement size = %d, want a different size from original %d", size2, size1)
	}

	cwd, ok = idx.headCWDIndexed(path, mtime2, size2)
	if !ok || cwd != replacementCWD {
		t.Fatalf("after replace, headCWDIndexed = (%q, %v), want (%s, true) -- the stale project-a entry must not be trusted", cwd, ok, replacementCWD)
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

// TestCWDIndex_PruneRemovesDeletedFileEntries covers a full discovery+save
// cycle: a file that gets skipped (non-matching cwd) populates a cwd-index
// entry; once that file is deleted, the next discovery pass must prune its
// now-stale entry so a save afterward doesn't persist it forever.
func TestCWDIndex_PruneRemovesDeletedFileEntries(t *testing.T) {
	configDir, projectsDir := newTestProjectsRoot(t)
	pathToDelete := writeClaudeSession(t, projectsDir, "dir-prune", "sess-prune", []string{cwdLine("/tmp/other")})
	cwdIndexPath := filepath.Join(t.TempDir(), "cwdindex-claude.json")
	match := func(cwd string) bool { return cwd == "/tmp/match" }

	cwdIdx := NewCWDIndex()
	if _, err := DiscoverSessionsFilteredIndexed(model.NewSessionCache(), NewDesktopCache(), []string{configDir}, match, cwdIdx); err != nil {
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

	if _, err := DiscoverSessionsFilteredIndexed(model.NewSessionCache(), NewDesktopCache(), []string{configDir}, match, reloaded); err != nil {
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

// claudeErrorfer is the minimal subset of *testing.T that claudeSessionDigest
// and claudeAssertSameSessionSet need. It exists (instead of taking
// *testing.T directly) so
// TestClaudeAssertSameSessionSet_CatchesStaleContentAtSamePath can pass a
// non-*testing.T fake that records a failure without that failure
// propagating to the test currently running claudeAssertSameSessionSet's
// own test — testing.TB can't be used for this, since it has an unexported
// method that only the standard library's *testing.T/B/F can implement.
// Every real call site passes a plain *testing.T, which satisfies this
// smaller interface implicitly.
type claudeErrorfer interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// claudeSessionDigest renders s as JSON for content comparison. Session has
// no fields populated from wall-clock time (every timestamp on it comes
// from parsed JSONL content via time.Parse, never time.Now()), so there are
// no legitimately-nondeterministic fields to normalize before comparing two
// independently-produced sessions for the same file — a byte-different
// digest here always means a genuine content difference, not incidental
// representation noise (e.g. time.Time's internal monotonic-reading
// representation, which reflect.DeepEqual is sensitive to but JSON
// marshaling normalizes away, since it only ever encodes the RFC3339Nano
// wall-clock value).
func claudeSessionDigest(t claudeErrorfer, s *model.Session) string {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal session for digest: %v", err)
	}
	return string(data)
}

// claudeAssertSameSessionSet fails t if cold and got don't contain sessions
// with the exact same JSONLPaths AND, for each shared path, byte-identical
// content. A path-only comparison would pass a stale-content-same-path
// regression (a persisted entry served for a file whose content actually
// changed) undetected, since both sides would agree on which files are
// present without ever checking what was returned for them.
func claudeAssertSameSessionSet(t claudeErrorfer, label string, cold, got []*model.Session) {
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
		if wantDigest, gotDigest := claudeSessionDigest(t, cs), claudeSessionDigest(t, gs); wantDigest != gotDigest {
			t.Errorf("%s: session content for %s differs from cold discovery\ncold: %s\ngot:  %s", label, p, wantDigest, gotDigest)
		}
	}
	for p := range gotByPath {
		if _, ok := coldByPath[p]; !ok {
			t.Errorf("%s: unexpected extra %s (absent from cold discovery)", label, p)
		}
	}
}

// claudeFakeErrorfer records whether Errorf/Fatalf was called, without
// failing the test actually running -- used only to verify that
// claudeAssertSameSessionSet itself reports a failure for a given input.
type claudeFakeErrorfer struct {
	failed bool
}

func (f *claudeFakeErrorfer) Helper()                           {}
func (f *claudeFakeErrorfer) Errorf(format string, args ...any) { f.failed = true }
func (f *claudeFakeErrorfer) Fatalf(format string, args ...any) { f.failed = true }

// TestClaudeAssertSameSessionSet_CatchesStaleContentAtSamePath is claude's
// counterpart to codex's TestAssertSameSessionSet_CatchesStaleContentAtSamePath
// -- proves the content check added to claudeAssertSameSessionSet actually
// catches a same-path-but-different-content regression, which a
// JSONLPath-set-only comparison would miss entirely.
func TestClaudeAssertSameSessionSet_CatchesStaleContentAtSamePath(t *testing.T) {
	cold := []*model.Session{
		{JSONLPath: "/tmp/x.jsonl", SessionID: "s1", CWD: "/tmp/match", TotalMessages: 5},
	}
	stale := []*model.Session{
		{JSONLPath: "/tmp/x.jsonl", SessionID: "s1-STALE", CWD: "/tmp/match", TotalMessages: 1},
	}

	fake := &claudeFakeErrorfer{}
	claudeAssertSameSessionSet(fake, "simulated", cold, stale)
	if !fake.failed {
		t.Fatal("claudeAssertSameSessionSet did not detect a same-path, different-content regression -- the content-digest check added to it is not actually catching this bug class")
	}

	fakeOK := &claudeFakeErrorfer{}
	claudeAssertSameSessionSet(fakeOK, "simulated", cold, cold)
	if fakeOK.failed {
		t.Fatal("claudeAssertSameSessionSet flagged identical content as a mismatch")
	}
}

// TestDiscoverSessionsFilteredIndexed_ComprehensiveEquivalence is claude's
// counterpart to codex's test of the same name: discovery using a
// persisted-and-reloaded SessionCache + CWDIndex must match cold discovery
// across the brief's required fixture-tree scenarios: unchanged files, a
// truncated/replaced file with the same mtime but a different size, a
// truncated/replaced file with a different mtime, a deleted file, a
// corrupted persisted cache/index JSON, and a wrong-formatVersion persisted
// cache/index JSON. (The "appended file changes the answer" scenario is
// codex-specific per the brief -- claude's headCWD scans forward from the
// start of the file, so an append can never change what it finds; see
// shouldParseForCWD's doc comment. Truncation/replacement is what actually
// exercises claude's invalidate-on-change rule.)
func TestDiscoverSessionsFilteredIndexed_ComprehensiveEquivalence(t *testing.T) {
	configDir, projectsDir := newTestProjectsRoot(t)
	match := func(cwd string) bool { return cwd == "/tmp/match" }

	paths := map[string]string{
		"unchanged_match":    writeClaudeSession(t, projectsDir, "dir-unchanged-match", "sess-unchanged-match", []string{cwdLine("/tmp/match")}),
		"unchanged_nonmatch": writeClaudeSession(t, projectsDir, "dir-unchanged-nonmatch", "sess-unchanged-nonmatch", []string{cwdLine("/tmp/other")}),
		// The original cwd here is deliberately a different length than
		// "/tmp/match" (below), so the mutation genuinely changes the
		// file's size, not just its bytes -- a same-length replacement
		// would accidentally collide on size and fail to exercise the
		// "same mtime, different size" invalidation path at all.
		"truncated_same_mtime": writeClaudeSession(t, projectsDir, "dir-trunc-same", "sess-trunc-same", []string{cwdLine("/tmp/some-other-much-longer-directory-name")}),
		"truncated_diff_mtime": writeClaudeSession(t, projectsDir, "dir-trunc-diff", "sess-trunc-diff", []string{cwdLine("/tmp/some-other-much-longer-directory-name")}),
		"deleted":              writeClaudeSession(t, projectsDir, "dir-deleted", "sess-deleted", []string{cwdLine("/tmp/other")}),
	}

	cache := model.NewSessionCache()
	cwdIdx := NewCWDIndex()
	if _, err := DiscoverSessionsFilteredIndexed(cache, NewDesktopCache(), []string{configDir}, match, cwdIdx); err != nil {
		t.Fatalf("run1: %v", err)
	}
	cacheDir := t.TempDir()
	discoveryPath := filepath.Join(cacheDir, "discovery-claude.json")
	cwdIndexPath := filepath.Join(cacheDir, "cwdindex-claude.json")
	if err := cache.SaveTo(discoveryPath); err != nil {
		t.Fatalf("SaveTo cache: %v", err)
	}
	if err := cwdIdx.SaveTo(cwdIndexPath); err != nil {
		t.Fatalf("SaveTo cwdIdx: %v", err)
	}

	infoSame, err := os.Stat(paths["truncated_same_mtime"])
	if err != nil {
		t.Fatal(err)
	}
	origMtime := infoSame.ModTime()
	if err := os.WriteFile(paths["truncated_same_mtime"], []byte(cwdLine("/tmp/match")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(paths["truncated_same_mtime"], origMtime, origMtime); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(paths["truncated_diff_mtime"], []byte(cwdLine("/tmp/match")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(paths["deleted"]); err != nil {
		t.Fatal(err)
	}

	cold, err := DiscoverSessionsFilteredIndexed(model.NewSessionCache(), NewDesktopCache(), []string{configDir}, match, nil)
	if err != nil {
		t.Fatalf("cold run: %v", err)
	}

	reloadedCache := model.NewSessionCache()
	if err := reloadedCache.LoadFrom(discoveryPath); err != nil {
		t.Fatalf("LoadFrom cache: %v", err)
	}
	reloadedIdx := NewCWDIndex()
	if err := reloadedIdx.LoadFrom(cwdIndexPath); err != nil {
		t.Fatalf("LoadFrom cwdIdx: %v", err)
	}
	warm, err := DiscoverSessionsFilteredIndexed(reloadedCache, NewDesktopCache(), []string{configDir}, match, reloadedIdx)
	if err != nil {
		t.Fatalf("warm run: %v", err)
	}

	claudeAssertSameSessionSet(t, "warm-vs-cold after mutations", cold, warm)

	warmPaths := claudeSessionPaths(warm)
	if !warmPaths[paths["truncated_same_mtime"]] {
		t.Errorf("truncated (same mtime, different size) file missing from warm results")
	}
	if !warmPaths[paths["truncated_diff_mtime"]] {
		t.Errorf("truncated (different mtime) file missing from warm results")
	}
	if warmPaths[paths["deleted"]] {
		t.Errorf("deleted file unexpectedly present in warm results")
	}

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
			got, err := DiscoverSessionsFilteredIndexed(c, NewDesktopCache(), []string{configDir}, match, idx)
			if err != nil {
				t.Fatalf("discovery: %v", err)
			}
			claudeAssertSameSessionSet(t, tc.name, cold, got)
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
			got, err := DiscoverSessionsFilteredIndexed(c, NewDesktopCache(), []string{configDir}, match, idx)
			if err != nil {
				t.Fatalf("discovery: %v", err)
			}
			claudeAssertSameSessionSet(t, tc.name, cold, got)
		})
	}
}
