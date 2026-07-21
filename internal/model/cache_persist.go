package model

import (
	"fmt"
	"time"

	"github.com/illegalstudio/lazyagent/internal/diskcache"
)

// sessionCacheFormatVersion is bumped whenever the persisted shape changes
// in a backward-incompatible way. LoadFrom refuses to merge a snapshot
// whose version doesn't match, so an old or future binary reading a cache
// file it doesn't understand starts cold instead of misinterpreting it.
const sessionCacheFormatVersion = 1

// persistedSessionCache is the on-disk shape of a SessionCache snapshot.
type persistedSessionCache struct {
	FormatVersion int                              `json:"formatVersion"`
	Entries       map[string]persistedSessionEntry `json:"entries"`
}

// persistedSessionEntry is the on-disk shape of one SessionCache entry.
// Session is a plain, fully-exported data struct (see Session in types.go)
// with no unexported fields or custom marshaling needs, so it round-trips
// through encoding/json byte-for-byte.
type persistedSessionEntry struct {
	MTime   time.Time `json:"mtime"`
	Size    int64     `json:"size"`
	Session *Session  `json:"session"`
}

// SaveTo atomically writes the cache's current contents to path (temp file
// in path's directory, then rename), with 0600 permissions -- the cache can
// contain transcript snippets. Safe to call while other goroutines read/
// write the cache; the snapshot is taken under the same lock GetIncremental/
// Put/Prune use.
func (c *SessionCache) SaveTo(path string) error {
	c.mu.Lock()
	snapshot := persistedSessionCache{
		FormatVersion: sessionCacheFormatVersion,
		Entries:       make(map[string]persistedSessionEntry, len(c.entries)),
	}
	for k, e := range c.entries {
		snapshot.Entries[k] = persistedSessionEntry{MTime: e.mtime, Size: e.size, Session: e.session}
	}
	c.mu.Unlock()

	return diskcache.AtomicWriteJSON(path, snapshot)
}

// LoadFrom best-effort loads a persisted snapshot from path and merges its
// entries into the cache, so a subsequent GetIncremental(path) behaves
// exactly as if the entry had been Put by an earlier call in this same
// process: a full hit (mtime unchanged) returns the loaded *Session
// directly, and an incremental hit (file grew) returns Clone() of it plus
// the persisted byte offset -- Clone()'s deep-copy semantics apply
// identically to a loaded entry, since json.Unmarshal always allocates
// fresh slices, so no loaded entry can ever alias another's backing arrays.
//
// Any file-level problem -- the file is missing or unreadable, the JSON is
// malformed, or the format version doesn't match -- is reported via the
// returned error and leaves the cache completely untouched (nothing is
// merged), so the caller can safely ignore the error and continue with a
// cold-but-valid cache; discovery is never failed by a bad cache file. An
// individual entry whose "session" field decoded as null (a partial/corrupt
// per-entry decode) is silently skipped rather than merged as a nil
// session, which would otherwise crash the very first GetIncremental/Clone
// call that reached it.
func (c *SessionCache) LoadFrom(path string) error {
	var snapshot persistedSessionCache
	if err := diskcache.ReadJSON(path, &snapshot); err != nil {
		return err
	}
	if snapshot.FormatVersion != sessionCacheFormatVersion {
		return fmt.Errorf("session cache %s: format version %d, want %d", path, snapshot.FormatVersion, sessionCacheFormatVersion)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range snapshot.Entries {
		if e.Session == nil {
			continue
		}
		c.entries[k] = sessionCacheEntry{mtime: e.MTime, size: e.Size, session: e.Session}
	}
	return nil
}
