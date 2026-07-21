package model

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionCache_SaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discovery-claude.json")

	// GetIncremental stats the session file itself (to compare mtimes), so
	// the fixture needs a real file on disk, not just a cache entry.
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	mtime := info.ModTime()

	c := NewSessionCache()
	session := &Session{
		SessionID:       "s1",
		CWD:             "/tmp/project-a",
		Agent:           "claude",
		TotalMessages:   2,
		RecentTools:     []ToolCall{{Name: "Read", Timestamp: mtime}},
		RecentMessages:  []ConversationMessage{{Role: "user", Text: "hi", Timestamp: mtime}},
		EntryTimestamps: []time.Time{mtime},
	}
	c.Put(sessionPath, mtime, 10, session)

	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	loaded := NewSessionCache()
	if err := loaded.LoadFrom(path); err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	// A full hit (mtime unchanged) must return the identical session data,
	// with offset 0 -- exactly as GetIncremental would for an entry that
	// was Put in-process, never touched by persistence at all.
	got, offset, gotMtime := loaded.GetIncremental(sessionPath)
	if got == nil {
		t.Fatal("GetIncremental after LoadFrom: got nil session, want a full hit")
	}
	if offset != 0 {
		t.Fatalf("offset = %d, want 0 (full hit)", offset)
	}
	if !gotMtime.Equal(mtime) {
		t.Fatalf("mtime = %v, want %v", gotMtime, mtime)
	}
	if got.SessionID != "s1" || got.CWD != "/tmp/project-a" || got.TotalMessages != 2 {
		t.Fatalf("got = %+v, want SessionID=s1 CWD=/tmp/project-a TotalMessages=2", got)
	}
	if len(got.RecentTools) != 1 || got.RecentTools[0].Name != "Read" {
		t.Fatalf("RecentTools = %+v, want [{Read ...}]", got.RecentTools)
	}
	if len(got.RecentMessages) != 1 || got.RecentMessages[0].Text != "hi" {
		t.Fatalf("RecentMessages = %+v, want [{user hi ...}]", got.RecentMessages)
	}
}

// TestSessionCache_LoadFrom_DropsEntryWhenSameMtimeButDifferentSize covers
// the gap the comprehensive discovery-level equivalence test (in
// internal/codex) surfaced: a tool that rewrites a file while deliberately
// preserving its mtime (cp -p, some sync/backup tools) between two separate
// `sessions` CLI invocations must not be loaded back in as if unchanged.
// GetIncremental's own full-hit check trusts mtime alone by design (see its
// doc comment -- some providers, e.g. grok, deliberately store a
// placeholder size and rely on exactly that trust for their own full-hit
// semantics), which is safe in-process since nothing can rewrite a file
// between two calls with its mtime coincidentally restored -- but that
// assumption doesn't hold across process runs. LoadFrom must catch this one
// specific, provably-stale combination (same mtime, different size, right
// now) itself, rather than loosening GetIncremental's contract that other
// callers depend on.
func TestSessionCache_LoadFrom_DropsEntryWhenSameMtimeButDifferentSize(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "discovery-codex.json")
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("0123456789ABCDEF"), 0o644); err != nil { // 16 bytes
		t.Fatal(err)
	}
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	mtime := info.ModTime()

	c := NewSessionCache()
	c.Put(sessionPath, mtime, 16, &Session{SessionID: "stale", CWD: "/tmp/old"})
	if err := c.SaveTo(cachePath); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	// Replace with shorter content, forcibly preserving the exact same
	// mtime -- the pathological case LoadFrom must not trust.
	if err := os.WriteFile(sessionPath, []byte("short"), 0o644); err != nil { // 5 bytes
		t.Fatal(err)
	}
	if err := os.Chtimes(sessionPath, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	loaded := NewSessionCache()
	if err := loaded.LoadFrom(cachePath); err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	got, offset, gotMtime := loaded.GetIncremental(sessionPath)
	if got != nil {
		t.Fatalf("GetIncremental = %+v, want nil (full miss forcing re-parse) -- LoadFrom must have dropped the stale entry, not merged it", got)
	}
	if offset != 0 {
		t.Fatalf("offset = %d, want 0 (full miss)", offset)
	}
	if !gotMtime.Equal(mtime) {
		t.Fatalf("mtime = %v, want %v", gotMtime, mtime)
	}
}

// TestSessionCache_LoadFrom_PlaceholderZeroSizeEntrySurvivesUnchangedFile
// covers a provider like grok that never does incremental parsing and
// deliberately Puts a placeholder size of 0 for every entry (see
// internal/grok's cache.Put call and its "size 0 forces a full re-parse on
// any future mtime change" comment). On an UNCHANGED file (same mtime),
// that entry must still be loaded and still produce a full hit, exactly as
// it would in-process -- the staleness check added for the "same mtime,
// different size" case must not treat a real file's nonzero size as
// evidence of staleness against a deliberately-placeholder 0, or it would
// silently defeat persistence for this whole class of provider without
// anything having actually changed.
func TestSessionCache_LoadFrom_PlaceholderZeroSizeEntrySurvivesUnchangedFile(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "discovery-grok.json")
	sessionPath := filepath.Join(dir, "chat_history.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"some":"real content, well over zero bytes"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	mtime := info.ModTime()

	c := NewSessionCache()
	c.Put(sessionPath, mtime, 0, &Session{SessionID: "s1", Agent: "grok"}) // grok's placeholder size
	if err := c.SaveTo(cachePath); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	// File is NOT touched at all between save and load.
	loaded := NewSessionCache()
	if err := loaded.LoadFrom(cachePath); err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	got, offset, gotMtime := loaded.GetIncremental(sessionPath)
	if got == nil {
		t.Fatal("GetIncremental after LoadFrom: got nil, want a full hit (placeholder-size entry on an unchanged file must survive persistence)")
	}
	if offset != 0 {
		t.Fatalf("offset = %d, want 0 (full hit)", offset)
	}
	if !gotMtime.Equal(mtime) {
		t.Fatalf("mtime = %v, want %v", gotMtime, mtime)
	}
	if got.SessionID != "s1" {
		t.Fatalf("SessionID = %q, want s1", got.SessionID)
	}
}

// TestSessionCache_LoadFrom_StillLoadsIncrementalEntryWhenFileGrew is the
// companion/contrast test to the one above: it proves the size-vs-mtime
// staleness check is narrowly scoped to the "same mtime" case only. A file
// that grew since the entry was saved (the ordinary, expected incremental-
// append scenario) has a DIFFERENT mtime and a LARGER size -- that entry
// must still be loaded and still drive a proper incremental resume
// (Clone() of the base session, offset at the old size), exactly like
// TestSessionCache_LoadFrom_PreservesIncrementalOffset already covers
// end-to-end; this test isolates the LoadFrom step itself to guard against
// a future, overly-broad tightening of the staleness check swallowing this
// legitimate case (an earlier draft of this fix did exactly that, by
// requiring size to match GetIncremental-side for every entry regardless of
// whether mtime changed, which broke grok's placeholder-size full-reparse
// design and any legitimate incremental-append entry).
func TestSessionCache_LoadFrom_StillLoadsIncrementalEntryWhenFileGrew(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "discovery-codex.json")
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("0123456789"), 0o644); err != nil { // 10 bytes
		t.Fatal(err)
	}
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	oldMtime := info.ModTime()

	c := NewSessionCache()
	c.Put(sessionPath, oldMtime, 10, &Session{SessionID: "base", CWD: "/tmp/a"})
	if err := c.SaveTo(cachePath); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	newMtime := oldMtime.Add(time.Second)
	if err := os.WriteFile(sessionPath, []byte("0123456789ABCDEF"), 0o644); err != nil { // 16 bytes
		t.Fatal(err)
	}
	if err := os.Chtimes(sessionPath, newMtime, newMtime); err != nil {
		t.Fatal(err)
	}

	loaded := NewSessionCache()
	if err := loaded.LoadFrom(cachePath); err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	base, offset, mtime := loaded.GetIncremental(sessionPath)
	if base == nil {
		t.Fatal("GetIncremental: got nil base, want the loaded entry to still drive an incremental resume")
	}
	if offset != 10 {
		t.Fatalf("offset = %d, want 10", offset)
	}
	if !mtime.Equal(newMtime) {
		t.Fatalf("mtime = %v, want %v", mtime, newMtime)
	}
	if base.SessionID != "base" {
		t.Fatalf("base.SessionID = %q, want base", base.SessionID)
	}
}

// TestSessionCache_LoadFrom_PreservesIncrementalOffset confirms the
// persisted entry's byte offset (the "size" field: how much of the file was
// consumed by the parse this entry reflects) survives the round-trip, so a
// file that grew since the snapshot was saved is still resumed
// incrementally rather than fully re-parsed.
func TestSessionCache_LoadFrom_PreservesIncrementalOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discovery-codex.json")
	filePath := filepath.Join(dir, "session.jsonl")

	if err := os.WriteFile(filePath, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	oldMtime := info.ModTime()

	c := NewSessionCache()
	c.Put(filePath, oldMtime, 5, &Session{SessionID: "s1", CWD: "/tmp/a"})
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	// Grow the file (simulating an append between runs) and give it a new
	// mtime, so the loaded entry's offset (5) should trigger an incremental
	// resume, not a full hit and not a full re-parse.
	newMtime := oldMtime.Add(time.Second)
	if err := os.WriteFile(filePath, []byte("0123456789ABCDEF"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filePath, newMtime, newMtime); err != nil {
		t.Fatal(err)
	}

	loaded := NewSessionCache()
	if err := loaded.LoadFrom(path); err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	base, offset, mtime := loaded.GetIncremental(filePath)
	if base == nil {
		t.Fatal("GetIncremental: got nil base, want the loaded entry as an incremental base")
	}
	if offset != 5 {
		t.Fatalf("offset = %d, want 5 (the persisted entry's byte offset)", offset)
	}
	if !mtime.Equal(newMtime) {
		t.Fatalf("mtime = %v, want the file's current (new) mtime %v", mtime, newMtime)
	}
	if base.SessionID != "s1" {
		t.Fatalf("base.SessionID = %q, want s1", base.SessionID)
	}
}

// TestSessionCache_LoadFrom_CloneIsIndependent confirms a loaded entry's
// Clone() (as used internally by GetIncremental's incremental-resume path)
// produces a deep copy that doesn't alias the cached entry's slices --
// exactly the same guarantee an in-process-populated entry provides.
func TestSessionCache_LoadFrom_CloneIsIndependent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discovery-claude.json")

	mtime := time.Now().Truncate(time.Second)
	c := NewSessionCache()
	c.Put("/tmp/session.jsonl", mtime, 10, &Session{
		SessionID:   "s1",
		RecentTools: []ToolCall{{Name: "Read"}},
	})
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	loaded := NewSessionCache()
	if err := loaded.LoadFrom(path); err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	entry, ok := loaded.entries["/tmp/session.jsonl"]
	if !ok {
		t.Fatal("loaded entry missing")
	}
	clone := entry.session.Clone()
	clone.RecentTools[0].Name = "Mutated"
	if entry.session.RecentTools[0].Name != "Read" {
		t.Fatalf("mutating the clone changed the cached entry: %q, want unaffected (Read)", entry.session.RecentTools[0].Name)
	}
}

func TestSessionCache_SaveTo_FilePermissions0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discovery-claude.json")

	c := NewSessionCache()
	c.Put("/tmp/session.jsonl", time.Now(), 10, &Session{SessionID: "s1"})
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("perms = %o, want 0600", perm)
	}
}

// TestSessionCache_SaveTo_AtomicNoTempFileLeftBehind is the SessionCache-
// level instance of the atomic-write behavior test the brief calls for:
// after a successful Save, the directory contains only the target file --
// no leftover temp file -- confirming the write really did go through the
// temp-file-then-rename path rather than writing path directly (which could
// leave a torn file on a crash mid-write).
func TestSessionCache_SaveTo_AtomicNoTempFileLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discovery-claude.json")

	c := NewSessionCache()
	c.Put("/tmp/session.jsonl", time.Now(), 10, &Session{SessionID: "s1"})
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "discovery-claude.json" {
		t.Fatalf("dir entries = %v, want exactly [discovery-claude.json]", entries)
	}
}

func TestSessionCache_LoadFrom_MissingFile(t *testing.T) {
	c := NewSessionCache()
	if err := c.LoadFrom(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("LoadFrom missing file: want error, got nil")
	}
}

func TestSessionCache_LoadFrom_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discovery-claude.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := NewSessionCache()
	if err := c.LoadFrom(path); err == nil {
		t.Fatal("LoadFrom corrupt JSON: want error, got nil")
	}
	// Cache must be left usable (cold), not poisoned.
	got, _, _ := c.GetIncremental("/tmp/anything.jsonl")
	if got != nil {
		t.Fatalf("cache after corrupt load = %+v, want nil (cold, untouched)", got)
	}
}

func TestSessionCache_LoadFrom_WrongFormatVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discovery-claude.json")
	if err := os.WriteFile(path, []byte(`{"formatVersion":999999,"entries":{"/tmp/x.jsonl":{"mtime":"2026-01-01T00:00:00Z","size":10,"session":{"SessionID":"s1"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	c := NewSessionCache()
	if err := c.LoadFrom(path); err == nil {
		t.Fatal("LoadFrom wrong format version: want error, got nil")
	}
	got, _, _ := c.GetIncremental("/tmp/x.jsonl")
	if got != nil {
		t.Fatalf("cache after version-mismatch load = %+v, want nil (cold, untouched)", got)
	}
}

// TestSessionCache_LoadFrom_SkipsNilSessionEntry covers a partially decoded
// entry (valid JSON, but a null "session" field) -- it must be skipped, not
// crash or poison the cache with a nil *Session that GetIncremental/Clone
// would panic on.
func TestSessionCache_LoadFrom_SkipsNilSessionEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discovery-claude.json")
	if err := os.WriteFile(path, []byte(`{"formatVersion":1,"entries":{"/tmp/x.jsonl":{"mtime":"2026-01-01T00:00:00Z","size":10,"session":null}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	c := NewSessionCache()
	if err := c.LoadFrom(path); err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	got, _, _ := c.GetIncremental("/tmp/x.jsonl")
	if got != nil {
		t.Fatalf("cache after nil-session entry load = %+v, want nil (entry skipped, not a hit)", got)
	}
}

func TestSessionCache_SaveTo_EmptyCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discovery-claude.json")

	c := NewSessionCache()
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo empty cache: %v", err)
	}

	loaded := NewSessionCache()
	if err := loaded.LoadFrom(path); err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
}
