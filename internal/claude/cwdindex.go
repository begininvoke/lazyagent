package claude

import (
	"fmt"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/diskcache"
)

// cwdIndexFormatVersion is bumped whenever the persisted shape changes in a
// backward-incompatible way.
const cwdIndexFormatVersion = 1

// CWDIndex memoizes headCWD results per claude session JSONL file, keyed by
// path with an (mtime, size) validity check, so that once a file's
// head-scan outcome is known for its CURRENT content, repeated discovery
// calls — including calls from a later process, if the index is persisted
// and reloaded — never re-read the file to answer the same question again.
//
// headCWD is a pure function of a file's content (it takes no matcher), so
// memoizing it is always sound to reuse for ANY future cwdMatch, regardless
// of which matcher originally triggered the scan. Unlike codex, claude's
// session.CWD is first-cwd-wins (see scanEntries in jsonl.go) and therefore
// stable across appends — but entries are still invalidated on ANY mtime or
// size change, for uniformity with codex's index and as a defensive
// safety net (see shouldParseForCWD's doc comment).
//
// Only files whose cwd conclusively does NOT match a query ever populate
// this index — see discoverInDir's cwdMatch != nil && cached == nil guard,
// the only place it's consulted. Files that get parsed end up in
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
}

// NewCWDIndex creates an empty CWDIndex.
func NewCWDIndex() *CWDIndex {
	return &CWDIndex{entries: make(map[string]cwdIndexEntry)}
}

// headCWDIndexed returns the same (cwd, ok) headCWD(path) would for path's
// current content, using a cached result when the index already holds a
// valid (mtime/size-matching) entry, and computing + storing it fresh
// otherwise.
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

// persistedCWDEntry is the on-disk shape of one CWDIndex entry.
type persistedCWDEntry struct {
	MTime   time.Time `json:"mtime"`
	Size    int64     `json:"size"`
	HeadCWD string    `json:"headCwd"`
	HeadOK  bool      `json:"headOk"`
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
		snapshot.Entries[k] = persistedCWDEntry{MTime: e.mtime, Size: e.size, HeadCWD: e.headCWD, HeadOK: e.headOK}
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
		idx.entries[k] = cwdIndexEntry{mtime: e.MTime, size: e.Size, headCWD: e.HeadCWD, headOK: e.HeadOK}
	}
	return nil
}
