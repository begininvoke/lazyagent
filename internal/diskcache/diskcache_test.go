package diskcache

import (
	"os"
	"path/filepath"
	"testing"
)

type sample struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestEnsureDir_CreatesAt0700(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lazyagent")
	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("perms = %o, want 0700", perm)
	}
}

// TestEnsureDir_TightensPreExisting0755Dir covers the real scenario this
// function exists for: another lazyagent feature sharing the same cache
// root (e.g. internal/search's index, which creates os.UserCacheDir()/
// lazyagent at 0755) may have already created the directory at looser
// permissions before a discovery cache is ever saved. Plain os.MkdirAll
// would silently leave those loose permissions in place, since it only
// applies the given mode when it actually creates the directory. EnsureDir
// must tighten it regardless of who created it first.
func TestEnsureDir_TightensPreExisting0755Dir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lazyagent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Fatalf("fixture setup: dir perms = %o, want 0755 before EnsureDir runs", perm)
	}

	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	info, err = os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("perms after EnsureDir = %o, want 0700 (tightened from the pre-existing 0755)", perm)
	}
}

func TestAtomicWriteJSON_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	want := sample{Name: "hello", Count: 3}
	if err := AtomicWriteJSON(path, want); err != nil {
		t.Fatalf("AtomicWriteJSON: %v", err)
	}

	var got sample
	if err := ReadJSON(path, &got); err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestAtomicWriteJSON_FilePermissions0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	if err := AtomicWriteJSON(path, sample{Name: "x"}); err != nil {
		t.Fatalf("AtomicWriteJSON: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file perms = %o, want 0600", perm)
	}
}

// TestAtomicWriteJSON_NoTempFileLeftBehind confirms a successful write
// leaves exactly the target file in the directory -- no stray temp file --
// proving the temp-file-then-rename dance cleans up after itself.
func TestAtomicWriteJSON_NoTempFileLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	if err := AtomicWriteJSON(path, sample{Name: "x"}); err != nil {
		t.Fatalf("AtomicWriteJSON: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "cache.json" {
		t.Fatalf("dir entries = %v, want exactly [cache.json]", entries)
	}
}

// TestAtomicWriteJSON_LeavesOldFileIntactOnMarshalError simulates a failed
// write (an unmarshalable value) and confirms the previously-written
// complete file is left untouched -- the atomic-write contract: a reader
// only ever sees the old complete file or the new complete file, never a
// torn one, and a failed write must not corrupt the old file.
func TestAtomicWriteJSON_LeavesOldFileIntactOnMarshalError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	if err := AtomicWriteJSON(path, sample{Name: "original"}); err != nil {
		t.Fatalf("AtomicWriteJSON (first write): %v", err)
	}

	// A Go channel cannot be marshaled to JSON -- forces json.Marshal to
	// fail before any temp file is even created.
	if err := AtomicWriteJSON(path, make(chan int)); err == nil {
		t.Fatal("AtomicWriteJSON with unmarshalable value: want error, got nil")
	}

	var got sample
	if err := ReadJSON(path, &got); err != nil {
		t.Fatalf("ReadJSON after failed write: %v", err)
	}
	if got.Name != "original" {
		t.Fatalf("file after failed write = %+v, want original untouched", got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir entries after failed write = %v, want exactly [cache.json] (no leftover temp file)", entries)
	}
}

func TestReadJSON_MissingFile(t *testing.T) {
	dir := t.TempDir()
	var got sample
	if err := ReadJSON(filepath.Join(dir, "missing.json"), &got); err == nil {
		t.Fatal("ReadJSON on missing file: want error, got nil")
	}
}

func TestReadJSON_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got sample
	if err := ReadJSON(path, &got); err == nil {
		t.Fatal("ReadJSON on corrupt JSON: want error, got nil")
	}
}
