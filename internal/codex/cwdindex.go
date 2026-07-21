package codex

import (
	"fmt"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/diskcache"
)

// cwdIndexFormatVersion is bumped whenever the persisted shape changes in a
// backward-incompatible way.
const cwdIndexFormatVersion = 1

// CWDIndex memoizes headCWD and tailFinalCWD results per rollout file,
// keyed by path with an (mtime, size) validity check, so that once a file's
// head/tail scan outcome is known for its CURRENT content, repeated
// discovery calls — including calls from a later process, if the index is
// persisted and reloaded — never re-read the file to answer the same
// question again.
//
// Both headCWD and tailFinalCWD are pure functions of a file's content (they
// take no matcher), so memoizing either is always sound to reuse for ANY
// future cwdMatch, regardless of which matcher originally triggered the
// scan — unlike shouldParseForCWD's overall boolean decision, which does
// depend on the matcher passed to that particular call and so is never
// itself cached (see shouldParseForCWDIndexed, which replays the exact same
// decision tree as shouldParseForCWD using these two memoized primitives).
//
// Entries are invalidated (recomputed from scratch) whenever a file's mtime
// or size changes at all: an appended turn_context can change a rollout
// file's final cwd, so any change must not be trusted, even though most
// appends don't touch cwd.
//
// Only files whose cwd conclusively does NOT match a query ever populate
// this index — see discoverSessionsFromDir's cwdMatch != nil && cached ==
// nil guard, the only place it's consulted. Files that get parsed end up in
// model.SessionCache instead, whose persisted full-hit fast path already
// avoids re-reading them.
type CWDIndex struct {
	mu      sync.Mutex
	entries map[string]cwdIndexEntry
}

type cwdIndexEntry struct {
	mtime time.Time
	size  int64

	headCWD string
	headOK  bool

	tailScanned bool
	tailCWD     string
	tailFound   bool
}

// NewCWDIndex creates an empty CWDIndex.
func NewCWDIndex() *CWDIndex {
	return &CWDIndex{entries: make(map[string]cwdIndexEntry)}
}

// headCWDIndexed returns the same (cwd, ok) headCWD(path) would for path's
// current content, using a cached result when the index already holds a
// valid (mtime/size-matching) entry, and computing + storing it fresh
// otherwise. mtime and size must be the caller's already-obtained current
// stat of path, shared with any accompanying tailFinalCWDIndexed call for
// the same path so both agree on what "current" means and no redundant stat
// calls or TOCTOU gap is introduced.
func (idx *CWDIndex) headCWDIndexed(path string, mtime time.Time, size int64) (cwd string, ok bool) {
	idx.mu.Lock()
	if e, found := idx.entries[path]; found && e.mtime.Equal(mtime) && e.size == size {
		idx.mu.Unlock()
		return e.headCWD, e.headOK
	}
	idx.mu.Unlock()

	cwd, ok = headCWD(path)

	idx.mu.Lock()
	idx.entries[path] = cwdIndexEntry{mtime: mtime, size: size, headCWD: cwd, headOK: ok}
	idx.mu.Unlock()
	return cwd, ok
}

// tailFinalCWDIndexed is headCWDIndexed's counterpart for tailFinalCWD. When
// called after headCWDIndexed for the same (path, mtime, size), it shares
// that call's entry (adding the tail fields to it); called on its own, it
// creates a fresh entry with only the tail fields populated.
func (idx *CWDIndex) tailFinalCWDIndexed(path string, mtime time.Time, size int64) (cwd string, found bool) {
	idx.mu.Lock()
	if e, ok := idx.entries[path]; ok && e.mtime.Equal(mtime) && e.size == size && e.tailScanned {
		idx.mu.Unlock()
		return e.tailCWD, e.tailFound
	}
	idx.mu.Unlock()

	cwd, found = tailFinalCWD(path)

	idx.mu.Lock()
	e, ok := idx.entries[path]
	if !ok || !e.mtime.Equal(mtime) || e.size != size {
		e = cwdIndexEntry{mtime: mtime, size: size}
	}
	e.tailScanned = true
	e.tailCWD = cwd
	e.tailFound = found
	idx.entries[path] = e
	idx.mu.Unlock()
	return cwd, found
}

// persistedCWDEntry is the on-disk shape of one CWDIndex entry.
type persistedCWDEntry struct {
	MTime       time.Time `json:"mtime"`
	Size        int64     `json:"size"`
	HeadCWD     string    `json:"headCwd"`
	HeadOK      bool      `json:"headOk"`
	TailScanned bool      `json:"tailScanned"`
	TailCWD     string    `json:"tailCwd"`
	TailFound   bool      `json:"tailFound"`
}

type persistedCWDIndex struct {
	FormatVersion int                          `json:"formatVersion"`
	Entries       map[string]persistedCWDEntry `json:"entries"`
}

// SaveTo atomically writes the index to path (temp file + rename), 0600
// permissions.
func (idx *CWDIndex) SaveTo(path string) error {
	idx.mu.Lock()
	snapshot := persistedCWDIndex{
		FormatVersion: cwdIndexFormatVersion,
		Entries:       make(map[string]persistedCWDEntry, len(idx.entries)),
	}
	for k, e := range idx.entries {
		snapshot.Entries[k] = persistedCWDEntry{
			MTime: e.mtime, Size: e.size,
			HeadCWD: e.headCWD, HeadOK: e.headOK,
			TailScanned: e.tailScanned, TailCWD: e.tailCWD, TailFound: e.tailFound,
		}
	}
	idx.mu.Unlock()

	return diskcache.AtomicWriteJSON(path, snapshot)
}

// LoadFrom best-effort loads a persisted snapshot from path and merges its
// entries into the index. Any file-level problem (missing/unreadable file,
// malformed JSON, format version mismatch) is reported via the returned
// error and leaves the index completely untouched — callers on the
// discovery path are expected to ignore the error and continue with a cold
// (but valid) index.
func (idx *CWDIndex) LoadFrom(path string) error {
	var snapshot persistedCWDIndex
	if err := diskcache.ReadJSON(path, &snapshot); err != nil {
		return err
	}
	if snapshot.FormatVersion != cwdIndexFormatVersion {
		return fmt.Errorf("cwd index %s: format version %d, want %d", path, snapshot.FormatVersion, cwdIndexFormatVersion)
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()
	for k, e := range snapshot.Entries {
		idx.entries[k] = cwdIndexEntry{
			mtime: e.MTime, size: e.Size,
			headCWD: e.HeadCWD, headOK: e.HeadOK,
			tailScanned: e.TailScanned, tailCWD: e.TailCWD, tailFound: e.TailFound,
		}
	}
	return nil
}
