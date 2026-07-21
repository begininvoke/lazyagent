package model

import (
	"fmt"
	"os"
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
//
// Each entry is also checked, right now, for the one specific way a
// persisted-and-reloaded entry can be unsafe in a way an in-process one
// never is: if the file it names currently has the SAME mtime as recorded
// but a DIFFERENT size, the entry is dropped rather than merged, so a fresh
// parse happens for that file this run instead of silently serving stale
// content. GetIncremental's own full-hit check trusts mtime alone (by
// design -- some providers that always fully re-parse rather than resume
// incrementally deliberately store a placeholder size and rely on exactly
// that trust, see e.g. internal/grok), which is safe within a single
// process: nothing can rewrite a file between two GetIncremental calls with
// its mtime coincidentally restored to its old value in between. That
// assumption doesn't hold across separate CLI invocations: a tool that
// rewrites a file while deliberately preserving its mtime (cp -p, some
// sync/backup tools) between two runs would otherwise load in an entry
// whose mtime matches but whose content doesn't, and GetIncremental would
// then serve it forever as a "full hit" without ever re-reading the file.
// Every other combination -- mtime differs (whether the file grew, for a
// legitimate incremental resume, or shrank), or the file is gone entirely
// -- is left for GetIncremental's existing logic to handle exactly as it
// would for an in-process entry; only this one specific, provably-stale
// combination is filtered here, precisely where persistence introduces it,
// without touching GetIncremental's contract that other callers depend on.
//
// The check only applies when the recorded size is non-zero. A stored size
// of 0 is grok's (and any similar always-fully-reparse provider's)
// deliberate placeholder -- see internal/grok's cache.Put call -- used
// purely to force a full re-parse on the NEXT mtime change; on an unchanged
// mtime it relies on GetIncremental's mtime-only trust exactly like this
// check does for any other provider, so treating "size 0 vs real file size"
// as evidence of staleness here would wrongly drop every one of that
// provider's entries and silently defeat its persistence, even though
// nothing has actually changed.
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
		if e.Size != 0 {
			if info, err := os.Stat(k); err == nil && info.ModTime().Equal(e.MTime) && info.Size() != e.Size {
				continue
			}
		}
		c.entries[k] = sessionCacheEntry{mtime: e.MTime, size: e.Size, session: e.Session}
	}
	return nil
}
