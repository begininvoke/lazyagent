package opencode

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDBPath_UsesOpenCodeDataDir(t *testing.T) {
	t.Setenv("OPENCODE_DATA_DIR", "/tmp/opencode-data")
	got := DBPath()
	want := filepath.Join("/tmp/opencode-data", "opencode.db")
	if got != want {
		t.Fatalf("DBPath() = %q, want %q", got, want)
	}
}

// TestDiscoverSessionsFiltered_WiringReachesOpenCodeFamilyAndFilters is a
// wiring test: it proves DiscoverSessionsFiltered is reachable through the
// opencode package's thin Source declaration and that it actually filters
// by directory, not just that it compiles. The heavy-lifting (row-level
// prefilter exactness, skip-optimization, equivalence property) is tested
// once, at the shared opencodefamily engine level.
func TestDiscoverSessionsFiltered_WiringReachesOpenCodeFamilyAndFilters(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_DATA_DIR", dir)
	writeTwoDirFixtureDB(t, DBPath())

	matchA := func(cwd string) bool { return cwd == "/tmp/opencode-project-a" }
	sessions, err := DiscoverSessionsFiltered(NewSessionCache(), matchA)
	if err != nil {
		t.Fatalf("DiscoverSessionsFiltered() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}
	if got := sessions[0]; got.SessionID != "ses_oc_a" || got.Agent != "opencode" || got.CWD != "/tmp/opencode-project-a" {
		t.Errorf("session = %+v, want ses_oc_a in /tmp/opencode-project-a agent opencode", got)
	}

	all, err := DiscoverSessionsFiltered(NewSessionCache(), nil)
	if err != nil {
		t.Fatalf("DiscoverSessionsFiltered(nil) error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2 (nil matcher must return everything)", len(all))
	}
}

// writeTwoDirFixtureDB creates two sessions in two different directories,
// each with a single well-formed message, using the minimal OpenCode-family
// schema (see opencodefamily's writeCompatDB for the source of this shape).
func writeTwoDirFixtureDB(t *testing.T, path string) {
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
		 VALUES ('ses_oc_a', 'proj_a', NULL, '/tmp/opencode-project-a', 'A', '0.1.0', 1700000000000, 1700000001000, NULL, NULL)`,
		`INSERT INTO session (id, project_id, parent_id, directory, title, version, time_created, time_updated, time_compacting, time_archived)
		 VALUES ('ses_oc_b', 'proj_b', NULL, '/tmp/opencode-project-b', 'B', '0.1.0', 1700000000000, 1700000002000, NULL, NULL)`,
		`INSERT INTO message (id, session_id, time_created, time_updated, data)
		 VALUES ('msg_oc_a', 'ses_oc_a', 1700000000000, 1700000000000, '{"role":"user","time":{"created":1700000000000}}')`,
		`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
		 VALUES ('part_oc_a', 'msg_oc_a', 'ses_oc_a', 1700000000000, 1700000000000, '{"type":"text","text":"hi from a"}')`,
		`INSERT INTO message (id, session_id, time_created, time_updated, data)
		 VALUES ('msg_oc_b', 'ses_oc_b', 1700000000000, 1700000000000, '{"role":"user","time":{"created":1700000000000}}')`,
		`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
		 VALUES ('part_oc_b', 'msg_oc_b', 'ses_oc_b', 1700000000000, 1700000000000, '{"type":"text","text":"hi from b"}')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
}
