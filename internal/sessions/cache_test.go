package sessions

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// --- ResolveCacheDir ---

func TestResolveCacheDir_Success(t *testing.T) {
	base := t.TempDir()
	orig := userCacheDir
	userCacheDir = func() (string, error) { return base, nil }
	defer func() { userCacheDir = orig }()

	dir, ok := ResolveCacheDir()
	if !ok {
		t.Fatal("ResolveCacheDir ok = false, want true")
	}
	want := filepath.Join(base, "lazyagent")
	if dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
}

func TestResolveCacheDir_UserCacheDirFailure(t *testing.T) {
	orig := userCacheDir
	userCacheDir = func() (string, error) { return "", errors.New("no cache dir on this platform") }
	defer func() { userCacheDir = orig }()

	dir, ok := ResolveCacheDir()
	if ok {
		t.Fatalf("ResolveCacheDir ok = true, want false; dir = %q", dir)
	}
	if dir != "" {
		t.Fatalf("dir = %q, want empty", dir)
	}
}

func TestResolveCacheDir_EmptyStringIsFailure(t *testing.T) {
	orig := userCacheDir
	userCacheDir = func() (string, error) { return "", nil }
	defer func() { userCacheDir = orig }()

	if _, ok := ResolveCacheDir(); ok {
		t.Fatal("ResolveCacheDir ok = true, want false for an empty (but non-error) UserCacheDir result")
	}
}

// --- writeFixtureCodexSession / fixture helpers ---

// writeFixtureCodexHome creates a fake $HOME with a single codex rollout
// session file whose cwd is cwd, and returns that HOME path. It also
// creates an empty ~/.claude and ~/.codex/session_index.jsonl so the other
// providers BuildProvider might touch (when agent is "all") don't error.
func writeFixtureCodexHome(t *testing.T, cwd string) string {
	t.Helper()
	home := t.TempDir()
	dayDir := filepath.Join(home, ".codex", "sessions", "2026", "03", "01")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionID := "019d3431-8669-7603-be71-7079fa555f4a"
	content := `{"timestamp":"2026-03-01T11:00:00.000Z","type":"session_meta","payload":{"id":"` + sessionID + `","cwd":"` + cwd + `","cli_version":"0.1.0","source":"cli"}}
{"timestamp":"2026-03-01T11:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}
`
	path := filepath.Join(dayDir, "rollout-2026-03-01T11-00-00-"+sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

// withStubbedCacheDir points userCacheDir at base for the duration of the
// test and restores it afterward.
func withStubbedCacheDir(t *testing.T, base string) {
	t.Helper()
	orig := userCacheDir
	userCacheDir = func() (string, error) { return base, nil }
	t.Cleanup(func() { userCacheDir = orig })
}

// captureStdout runs f with os.Stdout redirected to an in-memory pipe and
// returns whatever f wrote to it. Run() writes --json output directly to
// os.Stdout with no injectable writer, so this is the only way to capture
// it from an in-process test.
func captureStdout(t *testing.T, f func()) []byte {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	f()
	os.Stdout = orig
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// --- Run: cache wiring ---

func TestRun_SavesCacheAfterSuccessfulDiscovery(t *testing.T) {
	dir := t.TempDir() // the --dir target; also used as the fixture session's cwd
	home := writeFixtureCodexHome(t, dir)
	t.Setenv("HOME", home)

	cacheBase := t.TempDir()
	withStubbedCacheDir(t, cacheBase)

	// No TTY in the test process, so the interactive picker path errors
	// out with exit 2 -- irrelevant here, since Save happens right after
	// discovery, before the picker is ever reached.
	_ = Run([]string{"--agent", "codex", "--dir", dir})

	cacheDir := filepath.Join(cacheBase, "lazyagent")
	discoveryPath := filepath.Join(cacheDir, "discovery-codex.json")
	data, err := os.ReadFile(discoveryPath)
	if err != nil {
		t.Fatalf("expected %s to exist after Run: %v", discoveryPath, err)
	}
	if !bytes.Contains(data, []byte("019d3431-8669-7603-be71-7079fa555f4a")) {
		t.Fatalf("discovery-codex.json does not contain the fixture session ID -- content: %s", data)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "cwdindex-codex.json")); err != nil {
		t.Fatalf("expected cwdindex-codex.json to exist after Run: %v", err)
	}

	info, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("cache dir perms = %o, want 0700", perm)
	}
}

// TestRun_SavesCacheAfterSuccessfulDiscoveryWithZeroResults is the
// zero-results counterpart to the test above: a discovery that succeeds but
// matches nothing (the fixture session's cwd is a different directory than
// --dir queries for) must still save the caches -- per the brief, "Save
// even when the run found zero sessions." This also exercises the
// cwd-index specifically, since the one fixture file gets prefiltered
// (its non-matching cwd populates cwdindex-codex.json) rather than parsed.
func TestRun_SavesCacheAfterSuccessfulDiscoveryWithZeroResults(t *testing.T) {
	home := writeFixtureCodexHome(t, "/tmp/some-completely-different-project")
	t.Setenv("HOME", home)

	cacheBase := t.TempDir()
	withStubbedCacheDir(t, cacheBase)

	dir := t.TempDir() // does not match the fixture session's cwd
	code := Run([]string{"--agent", "codex", "--dir", dir})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (successful discovery, zero results -> \"no sessions found\")", code)
	}

	cacheDir := filepath.Join(cacheBase, "lazyagent")
	if _, err := os.Stat(filepath.Join(cacheDir, "discovery-codex.json")); err != nil {
		t.Fatalf("expected discovery-codex.json to exist after a zero-result run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "cwdindex-codex.json")); err != nil {
		t.Fatalf("expected cwdindex-codex.json to exist after a zero-result run: %v", err)
	}
}

func TestRun_DoesNotSaveCacheAfterDiscoveryError(t *testing.T) {
	// An empty HOME makes codex.SessionsDir() return "", which
	// discoverSessionsFromDir turns into a genuine discovery error.
	t.Setenv("HOME", "")

	cacheBase := t.TempDir()
	withStubbedCacheDir(t, cacheBase)

	dir := t.TempDir()
	code := Run([]string{"--agent", "codex", "--dir", dir})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (discovery error)", code)
	}

	if _, err := os.Stat(filepath.Join(cacheBase, "lazyagent")); !os.IsNotExist(err) {
		t.Fatalf("expected no cache dir to be created after a discovery error, stat err = %v", err)
	}
}

// TestRun_JSONOutputIdenticalColdAndWarm is the sessions.Run-level
// equivalence property test the brief calls for: running the exact same
// query twice -- once cold (no persisted cache yet), once warm (reusing the
// first run's persisted discovery cache and cwd index) -- must produce
// byte-identical --json output.
func TestRun_JSONOutputIdenticalColdAndWarm(t *testing.T) {
	dir := t.TempDir()
	home := writeFixtureCodexHome(t, dir)
	t.Setenv("HOME", home)

	cacheBase := t.TempDir()
	withStubbedCacheDir(t, cacheBase)

	args := []string{"--agent", "codex", "--json", "--dir", dir}

	var coldCode, warmCode int
	cold := captureStdout(t, func() { coldCode = Run(args) })
	warm := captureStdout(t, func() { warmCode = Run(args) })

	if coldCode != 0 || warmCode != 0 {
		t.Fatalf("exit codes = (cold=%d, warm=%d), want (0, 0)", coldCode, warmCode)
	}
	if len(cold) == 0 {
		t.Fatal("cold run produced no output")
	}
	if !bytes.Equal(cold, warm) {
		t.Fatalf("cold and warm --json output differ:\ncold: %s\nwarm: %s", cold, warm)
	}
}
