package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestSessionsDir(t *testing.T) {
	dir := SessionsDir()
	if dir == "" {
		t.Fatal("SessionsDir() returned empty string")
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".codex", "sessions")
	if dir != want {
		t.Fatalf("SessionsDir() = %q, want %q", dir, want)
	}
}

func TestDiscoverSessions_FromSyntheticDir(t *testing.T) {
	dir := t.TempDir()
	dayDir := filepath.Join(dir, "2026", "03", "28")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionID := "019d3431-8669-7603-be71-7079fa555f4a"
	indexPath := filepath.Join(t.TempDir(), "session_index.jsonl")
	index := `{"id":"` + sessionID + `","thread_name":"Add Codex support"}`
	if err := os.WriteFile(indexPath, []byte(index+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `{"timestamp":"2026-03-28T11:26:17.785Z","type":"session_meta","payload":{"id":"` + sessionID + `","cwd":"/tmp/project","cli_version":"0.116.0","source":"cli"}}
{"timestamp":"2026-03-28T11:26:17.900Z","type":"turn_context","payload":{"cwd":"/tmp/project","model":"gpt-5.2-codex","git":{"branch":"main"}}}
{"timestamp":"2026-03-28T11:26:18.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"please add codex"}]}}
{"timestamp":"2026-03-28T11:26:19.000Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"rg codex\"}"}}
{"timestamp":"2026-03-28T11:26:20.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"ok"}}
{"timestamp":"2026-03-28T11:26:21.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1200,"cached_input_tokens":300,"output_tokens":500,"reasoning_output_tokens":100}}}}
{"timestamp":"2026-03-28T11:26:22.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"implemented"}]}}
`
	path := filepath.Join(dayDir, "rollout-2026-03-28T11-25-54-"+sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	sessions, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}

	got := sessions[0]
	if got.SessionID != sessionID {
		t.Fatalf("SessionID = %q, want %q", got.SessionID, sessionID)
	}
	if got.Name != "Add Codex support" {
		t.Fatalf("Name = %q, want %q", got.Name, "Add Codex support")
	}
	if got.Agent != "codex" {
		t.Fatalf("Agent = %q, want codex", got.Agent)
	}
	if got.Model != "gpt-5.2-codex" {
		t.Fatalf("Model = %q, want gpt-5.2-codex", got.Model)
	}
	if got.GitBranch != "main" {
		t.Fatalf("GitBranch = %q, want main", got.GitBranch)
	}
	if got.UserMessages != 1 || got.AssistantMessages != 1 || got.TotalMessages != 2 {
		t.Fatalf("message counts = (%d,%d,%d), want (1,1,2)", got.UserMessages, got.AssistantMessages, got.TotalMessages)
	}
	if got.Status != model.StatusWaitingForUser {
		t.Fatalf("Status = %v, want waiting", got.Status)
	}
	if len(got.RecentTools) != 1 || got.RecentTools[0].Name != "exec_command" {
		t.Fatalf("RecentTools = %#v, want exec_command", got.RecentTools)
	}
	if got.InputTokens != 1200 || got.CacheReadTokens != 300 || got.OutputTokens != 600 {
		t.Fatalf("tokens = (%d,%d,%d), want (1200,300,600)", got.InputTokens, got.CacheReadTokens, got.OutputTokens)
	}
}

func TestParseJSONLIncremental(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	initial := `{"timestamp":"2026-03-28T11:26:17.785Z","type":"session_meta","payload":{"id":"s1","cwd":"/tmp/project","cli_version":"0.116.0","source":"cli"}}
{"timestamp":"2026-03-28T11:26:18.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}
{"timestamp":"2026-03-28T11:26:19.000Z","type":"response_item","payload":{"type":"function_call","name":"apply_patch","arguments":"*** Begin Patch"}}
`
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	base, offset, err := ParseJSONL(path)
	if err != nil {
		t.Fatalf("ParseJSONL error: %v", err)
	}
	if base.Status != model.StatusExecutingTool || base.CurrentTool != "apply_patch" {
		t.Fatalf("base status/tool = (%v,%q), want executing/apply_patch", base.Status, base.CurrentTool)
	}

	more := `{"timestamp":"2026-03-28T11:26:20.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"ok"}}
{"timestamp":"2026-03-28T11:26:21.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}
`
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(more); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	got, _, err := ParseJSONLIncremental(path, offset, base)
	if err != nil {
		t.Fatalf("ParseJSONLIncremental error: %v", err)
	}
	if got.Status != model.StatusWaitingForUser {
		t.Fatalf("Status = %v, want waiting", got.Status)
	}
	if got.CurrentTool != "" {
		t.Fatalf("CurrentTool = %q, want empty", got.CurrentTool)
	}
	if got.LastFileWrite != "apply_patch" {
		t.Fatalf("LastFileWrite = %q, want apply_patch", got.LastFileWrite)
	}
	if got.TotalMessages != 2 {
		t.Fatalf("TotalMessages = %d, want 2", got.TotalMessages)
	}
}

func TestDiscoverSessions_ParallelMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "session_index.jsonl")

	// Create 5 session files across different date dirs to exercise parallel parsing.
	var indexLines string
	for i := 0; i < 5; i++ {
		dayDir := filepath.Join(dir, "2026", "03", fmt.Sprintf("%02d", i+1))
		if err := os.MkdirAll(dayDir, 0o755); err != nil {
			t.Fatal(err)
		}
		sid := fmt.Sprintf("sess-%04d-0000-0000-0000-000000000000", i)
		indexLines += fmt.Sprintf(`{"id":"%s","thread_name":"Session %d"}`+"\n", sid, i)

		content := fmt.Sprintf(`{"timestamp":"2026-03-%02dT11:00:00.000Z","type":"session_meta","payload":{"id":"%s","cwd":"/tmp/project%d","cli_version":"0.116.0","source":"cli"}}
{"timestamp":"2026-03-%02dT11:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello %d"}]}}
{"timestamp":"2026-03-%02dT11:00:02.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi %d"}]}}
`, i+1, sid, i, i+1, i, i+1, i)
		fname := filepath.Join(dayDir, fmt.Sprintf("rollout-2026-03-%02dT11-00-00-%s.jsonl", i+1, sid))
		if err := os.WriteFile(fname, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(indexPath, []byte(indexLines), 0o644); err != nil {
		t.Fatal(err)
	}

	sessions, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 5 {
		t.Fatalf("got %d sessions, want 5", len(sessions))
	}

	ids := make(map[string]bool)
	for _, s := range sessions {
		ids[s.SessionID] = true
		if s.Agent != "codex" {
			t.Errorf("session %s: Agent = %q, want codex", s.SessionID, s.Agent)
		}
		if s.Name == "" {
			t.Errorf("session %s: Name should be set from index", s.SessionID)
		}
		if s.UserMessages != 1 || s.AssistantMessages != 1 {
			t.Errorf("session %s: messages = (%d,%d), want (1,1)", s.SessionID, s.UserMessages, s.AssistantMessages)
		}
	}
	if len(ids) != 5 {
		t.Errorf("got %d unique session IDs, want 5", len(ids))
	}
}

func TestHeadCWD_ValidSessionMeta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	content := `{"timestamp":"2026-03-28T11:26:17.785Z","type":"session_meta","payload":{"id":"s1","cwd":"/tmp/project-a","cli_version":"0.116.0","source":"cli"}}
{"timestamp":"2026-03-28T11:26:18.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, ok := headCWD(path)
	if !ok {
		t.Fatal("headCWD ok = false, want true")
	}
	if cwd != "/tmp/project-a" {
		t.Fatalf("headCWD cwd = %q, want /tmp/project-a", cwd)
	}
}

func TestHeadCWD_GarbageFirstLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	content := "not valid json at all\n{\"timestamp\":\"2026-03-28T11:26:18.000Z\",\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/tmp/project-a\"}}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, ok := headCWD(path); ok {
		t.Fatal("headCWD ok = true, want false for garbage first line")
	}
}

func TestHeadCWD_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, ok := headCWD(path); ok {
		t.Fatal("headCWD ok = true, want false for empty file")
	}
}

func TestHeadCWD_NonexistentFile(t *testing.T) {
	if _, ok := headCWD(filepath.Join(t.TempDir(), "missing.jsonl")); ok {
		t.Fatal("headCWD ok = true, want false for missing file")
	}
}

// writeRolloutFile is a small helper for the DiscoverSessionsFiltered tests:
// it writes a minimal rollout file with the given cwd (or, if cwd is empty,
// a garbage first line that headCWD cannot parse) under dir/2026/03/<day>.
func writeRolloutFile(t *testing.T, root string, day int, sessionID, cwd string) string {
	t.Helper()
	dayDir := filepath.Join(root, "2026", "03", fmt.Sprintf("%02d", day))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dayDir, fmt.Sprintf("rollout-2026-03-%02dT11-00-00-%s.jsonl", day, sessionID))

	var firstLine string
	if cwd == "" {
		firstLine = `not a json line at all`
	} else {
		firstLine = fmt.Sprintf(`{"timestamp":"2026-03-%02dT11:00:00.000Z","type":"session_meta","payload":{"id":"%s","cwd":"%s","cli_version":"0.116.0","source":"cli"}}`, day, sessionID, cwd)
	}
	content := firstLine + "\n" + fmt.Sprintf(`{"timestamp":"2026-03-%02dT11:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}`, day) + "\n"

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoverSessionsFiltered_ScopesToMatchingCWD(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "session_index.jsonl")

	pathA := writeRolloutFile(t, dir, 1, "sess-a-0000-0000-0000-000000000000", "/tmp/project-a")
	pathB := writeRolloutFile(t, dir, 2, "sess-b-0000-0000-0000-000000000000", "/tmp/project-b")
	pathMalformed := writeRolloutFile(t, dir, 3, "sess-c-0000-0000-0000-000000000000", "")

	matchA := func(cwd string) bool { return cwd == "/tmp/project-a" }

	sessions, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), matchA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gotPaths := make(map[string]bool)
	for _, s := range sessions {
		gotPaths[s.JSONLPath] = true
	}

	if !gotPaths[pathA] {
		t.Errorf("expected matching cwd file %s to be included", pathA)
	}
	if !gotPaths[pathMalformed] {
		t.Errorf("expected malformed-first-line file %s to be included (conservative fallback)", pathMalformed)
	}
	if gotPaths[pathB] {
		t.Errorf("did not expect non-matching cwd file %s to be included", pathB)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
}

func TestDiscoverSessionsFiltered_NilMatcherReturnsAll(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "session_index.jsonl")

	writeRolloutFile(t, dir, 1, "sess-a-0000-0000-0000-000000000000", "/tmp/project-a")
	writeRolloutFile(t, dir, 2, "sess-b-0000-0000-0000-000000000000", "/tmp/project-b")
	writeRolloutFile(t, dir, 3, "sess-c-0000-0000-0000-000000000000", "")

	sessions, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("got %d sessions, want 3 (nil matcher must not filter anything)", len(sessions))
	}
}

func TestDiscoverSessionsFiltered_CacheHitFilteredWithoutFileRead(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "session_index.jsonl")

	pathA := writeRolloutFile(t, dir, 1, "sess-a-0000-0000-0000-000000000000", "/tmp/project-a")
	pathB := writeRolloutFile(t, dir, 2, "sess-b-0000-0000-0000-000000000000", "/tmp/project-b")

	cache := model.NewSessionCache()
	// Prime the cache with a full parse (nil matcher) so both files are cached.
	if _, err := discoverSessionsFromDir(dir, indexPath, cache, nil); err != nil {
		t.Fatalf("priming pass: unexpected error: %v", err)
	}

	matchA := func(cwd string) bool { return cwd == "/tmp/project-a" }
	sessions, err := discoverSessionsFromDir(dir, indexPath, cache, matchA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gotPaths := make(map[string]bool)
	for _, s := range sessions {
		gotPaths[s.JSONLPath] = true
	}
	if !gotPaths[pathA] {
		t.Errorf("expected cached matching-cwd file %s to be included", pathA)
	}
	if gotPaths[pathB] {
		t.Errorf("did not expect cached non-matching-cwd file %s to be included", pathB)
	}
}

func TestDiscoverSessions_StillReturnsEverything(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "session_index.jsonl")

	writeRolloutFile(t, dir, 1, "sess-a-0000-0000-0000-000000000000", "/tmp/project-a")
	writeRolloutFile(t, dir, 2, "sess-b-0000-0000-0000-000000000000", "/tmp/project-b")

	sessions, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
}
