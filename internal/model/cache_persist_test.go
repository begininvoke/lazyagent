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
