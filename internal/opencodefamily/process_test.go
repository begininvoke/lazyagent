package opencodefamily

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
	_ "modernc.org/sqlite"
)

func TestDiscoverSessionsFor_OpenCodeCompatibleSource(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEST_KILO_DATA_DIR", dir)

	source := Source{
		Agent:      "kilo",
		EnvVar:     "TEST_KILO_DATA_DIR",
		DataSubdir: "kilo",
		DBFile:     "kilo.db",
	}
	writeCompatDB(t, filepath.Join(dir, "kilo.db"))

	sessions, err := DiscoverSessionsFor(source, NewSessionCache())
	if err != nil {
		t.Fatalf("DiscoverSessionsFor() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}

	got := sessions[0]
	if got.Agent != "kilo" {
		t.Fatalf("Agent = %q, want kilo", got.Agent)
	}
	if got.SessionID != "ses_test" {
		t.Errorf("SessionID = %q, want ses_test", got.SessionID)
	}
	if got.CWD != "/tmp/project" {
		t.Errorf("CWD = %q, want /tmp/project", got.CWD)
	}
	if got.Name != "Test session" {
		t.Errorf("Name = %q, want Test session", got.Name)
	}
	if got.Version != "7.3.12" {
		t.Errorf("Version = %q, want 7.3.12", got.Version)
	}
	if got.Model != "grok-4.3" {
		t.Errorf("Model = %q, want grok-4.3", got.Model)
	}
	if got.Status != model.StatusExecutingTool {
		t.Errorf("Status = %v, want %v", got.Status, model.StatusExecutingTool)
	}
	if got.CurrentTool != "Write" {
		t.Errorf("CurrentTool = %q, want Write", got.CurrentTool)
	}
	if got.LastFileWrite != "/tmp/project/main.go" {
		t.Errorf("LastFileWrite = %q, want /tmp/project/main.go", got.LastFileWrite)
	}
	if got.UserMessages != 1 || got.AssistantMessages != 1 || got.TotalMessages != 2 {
		t.Errorf("message counts = user %d assistant %d total %d, want 1/1/2", got.UserMessages, got.AssistantMessages, got.TotalMessages)
	}
	if got.InputTokens != 10 || got.OutputTokens != 20 || got.CacheReadTokens != 3 || got.CacheCreationTokens != 4 {
		t.Errorf("tokens = in %d out %d read %d write %d, want 10/20/3/4", got.InputTokens, got.OutputTokens, got.CacheReadTokens, got.CacheCreationTokens)
	}
	if got.CostUSD != 0.5 {
		t.Errorf("CostUSD = %v, want 0.5", got.CostUSD)
	}
	if len(got.RecentMessages) != 1 || got.RecentMessages[0].Text != "hello" {
		t.Fatalf("RecentMessages = %#v, want one user text", got.RecentMessages)
	}
	if len(got.RecentTools) != 1 || got.RecentTools[0].Name != "Write" {
		t.Fatalf("RecentTools = %#v, want one Write tool", got.RecentTools)
	}

	wantToolTime := time.UnixMilli(1700000001000)
	if !got.LastFileWriteAt.Equal(wantToolTime) {
		t.Errorf("LastFileWriteAt = %v, want %v", got.LastFileWriteAt, wantToolTime)
	}
	if !got.LastActivity.Equal(time.UnixMilli(1700000002000)) {
		t.Errorf("LastActivity = %v, want session time_updated", got.LastActivity)
	}
}

func TestDataDirFor_UsesEnvOverride(t *testing.T) {
	t.Setenv("TEST_AGENT_DATA_DIR", "/tmp/test-agent")
	got := DataDirFor(Source{Agent: "test-agent", EnvVar: "TEST_AGENT_DATA_DIR"})
	if got != "/tmp/test-agent" {
		t.Fatalf("DataDirFor() = %q, want /tmp/test-agent", got)
	}
}

func writeCompatDB(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE session (
			id text PRIMARY KEY,
			project_id text NOT NULL,
			parent_id text,
			directory text NOT NULL,
			title text NOT NULL,
			version text NOT NULL,
			time_created integer NOT NULL,
			time_updated integer NOT NULL,
			time_compacting integer,
			time_archived integer
		)`,
		`CREATE TABLE message (
			id text PRIMARY KEY,
			session_id text NOT NULL,
			time_created integer NOT NULL,
			time_updated integer NOT NULL,
			data text NOT NULL
		)`,
		`CREATE TABLE part (
			id text PRIMARY KEY,
			message_id text NOT NULL,
			session_id text NOT NULL,
			time_created integer NOT NULL,
			time_updated integer NOT NULL,
			data text NOT NULL
		)`,
		`INSERT INTO session (id, project_id, parent_id, directory, title, version, time_created, time_updated, time_compacting, time_archived)
		 VALUES ('ses_test', 'proj_test', NULL, '/tmp/project', 'Test session', '7.3.12', 1700000000000, 1700000002000, NULL, NULL)`,
		`INSERT INTO message (id, session_id, time_created, time_updated, data)
		 VALUES ('msg_user', 'ses_test', 1700000000000, 1700000000000, '{"role":"user","time":{"created":1700000000000}}')`,
		`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
		 VALUES ('part_user_text', 'msg_user', 'ses_test', 1700000000000, 1700000000000, '{"type":"text","text":"hello"}')`,
		`INSERT INTO message (id, session_id, time_created, time_updated, data)
		 VALUES ('msg_assistant', 'ses_test', 1700000001000, 1700000001000, '{"role":"assistant","modelID":"grok-4.3","providerID":"xai","cost":0.5,"tokens":{"input":10,"output":20,"cache":{"read":3,"write":4}},"finish":"tool-calls","time":{"created":1700000001000}}')`,
		`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
		 VALUES ('part_tool', 'msg_assistant', 'ses_test', 1700000001000, 1700000001000, '{"type":"tool","tool":"write","state":{"input":{"file_path":"/tmp/project/main.go"}}}')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
}

// filterFixtureSource is the Source used by the DiscoverSessionsForFiltered
// tests below; it's independent of the OpenCode/Kilo defaults so these tests
// don't collide with real OPENCODE_DATA_DIR/KILO_DATA_DIR env state.
var filterFixtureSource = Source{
	Agent:      "testfilter",
	EnvVar:     "TEST_FILTER_DATA_DIR",
	DataSubdir: "testfilter",
	DBFile:     "testfilter.db",
}

// writeFilterFixtureDB creates a session table with three sessions across
// two directories:
//   - ses_a1, ses_a2 live in /tmp/project-a, each with one well-formed
//     user message/part.
//   - ses_b1 lives in /tmp/project-b and carries a message row whose data
//     column is malformed JSON — a payload that would fail parsing loudly
//     if buildSession ever attempted to load it. Combined with the
//     cache-population check the tests below perform (see
//     TestDiscoverSessionsForFiltered_SkipsMessageLoadForNonMatchingSessions),
//     this proves the skipped session's messages were never read: the
//     malformed payload documents intent, and the cache assertion is the
//     actual mechanism proving buildSession (and its message query) was
//     never invoked for ses_b1.
func writeFilterFixtureDB(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE session (
			id text PRIMARY KEY,
			project_id text NOT NULL,
			parent_id text,
			directory text NOT NULL,
			title text NOT NULL,
			version text NOT NULL,
			time_created integer NOT NULL,
			time_updated integer NOT NULL,
			time_compacting integer,
			time_archived integer
		)`,
		`CREATE TABLE message (
			id text PRIMARY KEY,
			session_id text NOT NULL,
			time_created integer NOT NULL,
			time_updated integer NOT NULL,
			data text NOT NULL
		)`,
		`CREATE TABLE part (
			id text PRIMARY KEY,
			message_id text NOT NULL,
			session_id text NOT NULL,
			time_created integer NOT NULL,
			time_updated integer NOT NULL,
			data text NOT NULL
		)`,
		`INSERT INTO session (id, project_id, parent_id, directory, title, version, time_created, time_updated, time_compacting, time_archived)
		 VALUES ('ses_a1', 'proj_a', NULL, '/tmp/project-a', 'A1', '1.0.0', 1700000000000, 1700000001000, NULL, NULL)`,
		`INSERT INTO session (id, project_id, parent_id, directory, title, version, time_created, time_updated, time_compacting, time_archived)
		 VALUES ('ses_a2', 'proj_a', NULL, '/tmp/project-a', 'A2', '1.0.0', 1700000000000, 1700000002000, NULL, NULL)`,
		`INSERT INTO session (id, project_id, parent_id, directory, title, version, time_created, time_updated, time_compacting, time_archived)
		 VALUES ('ses_b1', 'proj_b', NULL, '/tmp/project-b', 'B1', '1.0.0', 1700000000000, 1700000003000, NULL, NULL)`,
		`INSERT INTO message (id, session_id, time_created, time_updated, data)
		 VALUES ('msg_a1', 'ses_a1', 1700000000000, 1700000000000, '{"role":"user","time":{"created":1700000000000}}')`,
		`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
		 VALUES ('part_a1', 'msg_a1', 'ses_a1', 1700000000000, 1700000000000, '{"type":"text","text":"hello from a1"}')`,
		`INSERT INTO message (id, session_id, time_created, time_updated, data)
		 VALUES ('msg_a2', 'ses_a2', 1700000000000, 1700000000000, '{"role":"user","time":{"created":1700000000000}}')`,
		`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
		 VALUES ('part_a2', 'msg_a2', 'ses_a2', 1700000000000, 1700000000000, '{"type":"text","text":"hello from a2"}')`,
		// Malformed JSON on purpose — see doc comment above.
		`INSERT INTO message (id, session_id, time_created, time_updated, data)
		 VALUES ('msg_b1', 'ses_b1', 1700000000000, 1700000000000, '{"role":"user", this is not valid json')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
}

func TestDiscoverSessionsForFiltered_NilMatcherReturnsAll(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filterFixtureSource.EnvVar, dir)
	writeFilterFixtureDB(t, DBPathFor(filterFixtureSource))

	got, err := DiscoverSessionsForFiltered(filterFixtureSource, NewSessionCache(), nil)
	if err != nil {
		t.Fatalf("DiscoverSessionsForFiltered() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(sessions) = %d, want 3", len(got))
	}

	want, err := DiscoverSessionsFor(filterFixtureSource, NewSessionCache())
	if err != nil {
		t.Fatalf("DiscoverSessionsFor() error = %v", err)
	}
	if len(want) != len(got) {
		t.Fatalf("nil-matcher filtered discovery returned %d sessions, DiscoverSessionsFor returned %d", len(got), len(want))
	}
}

func TestDiscoverSessionsForFiltered_ScopesToMatchingDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filterFixtureSource.EnvVar, dir)
	writeFilterFixtureDB(t, DBPathFor(filterFixtureSource))

	matchA := func(cwd string) bool { return cwd == "/tmp/project-a" }
	sessions, err := DiscoverSessionsForFiltered(filterFixtureSource, NewSessionCache(), matchA)
	if err != nil {
		t.Fatalf("DiscoverSessionsForFiltered() error = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len(sessions) = %d, want 2", len(sessions))
	}
	for _, s := range sessions {
		if s.CWD != "/tmp/project-a" {
			t.Errorf("session %s CWD = %q, want /tmp/project-a", s.SessionID, s.CWD)
		}
		if s.SessionID == "ses_b1" {
			t.Error("ses_b1 (directory /tmp/project-b) must not be returned when matching /tmp/project-a")
		}
	}
}

// TestDiscoverSessionsForFiltered_SkipsMessageLoadForNonMatchingSessions
// proves the row-level directory prefilter skips the expensive message/part
// load entirely for non-matching sessions, rather than merely dropping them
// from the result after loading. The sentinel mechanism: buildSession only
// ever populates cache.entries for a session ID after successfully running
// its message/part query (see DiscoverSessionsForFiltered's cache.Put
// call), so a filtered call that never adds an entry for ses_b1 (directory
// /tmp/project-b, excluded by matchA) proves buildSession — and therefore
// the message query, including the malformed-JSON row seeded by
// writeFilterFixtureDB — was never invoked for it. This test lives in
// package opencodefamily specifically so it can reach the unexported
// cache.entries map.
func TestDiscoverSessionsForFiltered_SkipsMessageLoadForNonMatchingSessions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filterFixtureSource.EnvVar, dir)
	writeFilterFixtureDB(t, DBPathFor(filterFixtureSource))

	matchA := func(cwd string) bool { return cwd == "/tmp/project-a" }
	cache := NewSessionCache()
	if _, err := DiscoverSessionsForFiltered(filterFixtureSource, cache, matchA); err != nil {
		t.Fatalf("DiscoverSessionsForFiltered() error = %v", err)
	}

	cache.mu.Lock()
	_, built := cache.entries["ses_b1"]
	_, builtA1 := cache.entries["ses_a1"]
	_, builtA2 := cache.entries["ses_a2"]
	cache.mu.Unlock()

	if built {
		t.Error("ses_b1 was built (cached) despite not matching the directory filter — its messages should never have been loaded")
	}
	if !builtA1 || !builtA2 {
		t.Error("expected ses_a1 and ses_a2 (matching sessions) to be built and cached")
	}
}

// TestDiscoverSessionsForFiltered_EquivalentToManualFilterOfFullDiscovery is
// an equivalence-property test: for a fixture spanning two directories,
// DiscoverSessionsForFiltered's result must equal filtering a full
// (unfiltered) DiscoverSessionsFor pass by the same matcher, for several
// matchers — proving the fast path never silently drops a session the slow
// path would have returned, nor includes one it wouldn't.
func TestDiscoverSessionsForFiltered_EquivalentToManualFilterOfFullDiscovery(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filterFixtureSource.EnvVar, dir)
	writeFilterFixtureDB(t, DBPathFor(filterFixtureSource))

	matchers := map[string]func(string) bool{
		"project-a":      func(cwd string) bool { return cwd == "/tmp/project-a" },
		"project-b":      func(cwd string) bool { return cwd == "/tmp/project-b" },
		"nothing-at-all": func(cwd string) bool { return cwd == "/tmp/does-not-exist" },
		"everything":     func(string) bool { return true },
	}

	for name, matcher := range matchers {
		t.Run(name, func(t *testing.T) {
			full, err := DiscoverSessionsFor(filterFixtureSource, NewSessionCache())
			if err != nil {
				t.Fatalf("full discovery: unexpected error: %v", err)
			}
			var want []string
			for _, s := range full {
				if matcher(s.CWD) {
					want = append(want, s.SessionID)
				}
			}
			sort.Strings(want)

			got, err := DiscoverSessionsForFiltered(filterFixtureSource, NewSessionCache(), matcher)
			if err != nil {
				t.Fatalf("filtered discovery: unexpected error: %v", err)
			}
			var gotIDs []string
			for _, s := range got {
				gotIDs = append(gotIDs, s.SessionID)
			}
			sort.Strings(gotIDs)

			if len(want) != len(gotIDs) {
				t.Fatalf("filtered discovery = %v, want %v (must equal manual filter of full discovery)", gotIDs, want)
			}
			for i := range want {
				if want[i] != gotIDs[i] {
					t.Fatalf("filtered discovery = %v, want %v (must equal manual filter of full discovery)", gotIDs, want)
				}
			}
		})
	}
}

// TestDiscoverSessionsForFiltered_CacheHitRespectsMatcher confirms that a
// session served from cache (time_updated unchanged since the prior call)
// is still checked against cwdMatch before being returned.
func TestDiscoverSessionsForFiltered_CacheHitRespectsMatcher(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filterFixtureSource.EnvVar, dir)
	writeFilterFixtureDB(t, DBPathFor(filterFixtureSource))

	cache := NewSessionCache()
	// Prime the cache with an unfiltered pass.
	if _, err := DiscoverSessionsFor(filterFixtureSource, cache); err != nil {
		t.Fatalf("priming pass: unexpected error: %v", err)
	}

	matchA := func(cwd string) bool { return cwd == "/tmp/project-a" }
	sessions, err := DiscoverSessionsForFiltered(filterFixtureSource, cache, matchA)
	if err != nil {
		t.Fatalf("DiscoverSessionsForFiltered() error = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len(sessions) = %d, want 2 (cache hits must still respect the matcher)", len(sessions))
	}
	for _, s := range sessions {
		if s.CWD != "/tmp/project-a" {
			t.Errorf("session %s CWD = %q, want /tmp/project-a", s.SessionID, s.CWD)
		}
	}
}
