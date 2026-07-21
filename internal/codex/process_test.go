package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
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

	sessions, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), nil, nil)
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

	sessions, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), nil, nil)
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
	// Malformed first line (headCWD can't determine a cwd from it), but a
	// real turn_context line further down establishes the session's actual
	// cwd as project-a. This exercises the "unreadable head -> conservative
	// full parse" path and confirms the file is genuinely parsed (not
	// blanket-included) and correctly included because its *real* parsed
	// cwd matches — not because the head was unreadable.
	pathMalformed := writeRolloutFileRaw(t, dir, 3, "sess-c-0000-0000-0000-000000000000", []string{
		`not a json line at all`,
		`{"timestamp":"2026-03-03T11:00:01.000Z","type":"turn_context","payload":{"cwd":"/tmp/project-a","model":"gpt-5.2-codex","git":{"branch":"main"}}}`,
	})

	matchA := func(cwd string) bool { return cwd == "/tmp/project-a" }

	sessions, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), matchA, nil)
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
		t.Errorf("expected malformed-first-line file %s to be included: unreadable head must fall through to a full parse, and its real cwd (from turn_context) matches", pathMalformed)
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

	sessions, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("got %d sessions, want 3 (nil matcher must not filter anything)", len(sessions))
	}
}

// TestDiscoverSessionsFiltered_FullCacheHitUsesCachedCWDWithoutRereadingFile
// asserts the full-cache-hit fast path actually relies on cached.CWD instead
// of touching the file: it corrupts pathA's on-disk content after priming
// the cache (while preserving its exact mtime, so GetIncremental still
// reports a full hit) and confirms the filtered result is still correct —
// which is only possible if the cached CWD, not a re-read of the now-garbage
// file, is what got checked.
func TestDiscoverSessionsFiltered_FullCacheHitUsesCachedCWDWithoutRereadingFile(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "session_index.jsonl")

	pathA := writeRolloutFile(t, dir, 1, "sess-a-0000-0000-0000-000000000000", "/tmp/project-a")
	pathB := writeRolloutFile(t, dir, 2, "sess-b-0000-0000-0000-000000000000", "/tmp/project-b")

	cache := model.NewSessionCache()
	// Prime the cache with a full parse (nil matcher) so both files are cached.
	if _, err := discoverSessionsFromDir(dir, indexPath, cache, nil, nil); err != nil {
		t.Fatalf("priming pass: unexpected error: %v", err)
	}

	infoA, err := os.Stat(pathA)
	if err != nil {
		t.Fatal(err)
	}
	mtimeA := infoA.ModTime()

	// Corrupt pathA on disk (a re-read would find no valid cwd at all), but
	// restore its exact mtime so the cache still treats it as unchanged.
	if err := os.WriteFile(pathA, []byte("garbage, not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(pathA, mtimeA, mtimeA); err != nil {
		t.Fatal(err)
	}

	matchA := func(cwd string) bool { return cwd == "/tmp/project-a" }
	sessions, err := discoverSessionsFromDir(dir, indexPath, cache, matchA, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gotPaths := make(map[string]bool)
	for _, s := range sessions {
		gotPaths[s.JSONLPath] = true
	}
	if !gotPaths[pathA] {
		t.Errorf("expected cached matching-cwd file %s to be included via cached.CWD despite corrupted on-disk content", pathA)
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

	sessions, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
}

// writeRolloutFileRaw writes a rollout file with exactly the given raw JSONL
// lines, for tests that need to control turn_context entries precisely
// (e.g. cwd drift scenarios that writeRolloutFile can't express).
func writeRolloutFileRaw(t *testing.T, root string, day int, sessionID string, lines []string) string {
	t.Helper()
	dayDir := filepath.Join(root, "2026", "03", fmt.Sprintf("%02d", day))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dayDir, fmt.Sprintf("rollout-2026-03-%02dT11-00-00-%s.jsonl", day, sessionID))
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// --- tailFinalCWD ---

func TestTailFinalCWD_FindsLastNonEmptyCWD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	content := `{"timestamp":"2026-03-01T11:00:00.000Z","type":"session_meta","payload":{"id":"s1","cwd":"/tmp/a"}}
{"timestamp":"2026-03-01T11:00:01.000Z","type":"turn_context","payload":{"cwd":"/tmp/b"}}
{"timestamp":"2026-03-01T11:00:02.000Z","type":"turn_context","payload":{}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, found := tailFinalCWD(path)
	if !found {
		t.Fatal("tailFinalCWD found = false, want true")
	}
	if cwd != "/tmp/b" {
		t.Fatalf("cwd = %q, want /tmp/b (the empty-cwd turn_context after it must not win)", cwd)
	}
}

func TestTailFinalCWD_NotFoundWhenNoTurnContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	content := `{"timestamp":"2026-03-01T11:00:00.000Z","type":"session_meta","payload":{"id":"s1","cwd":"/tmp/a"}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, found := tailFinalCWD(path); found {
		t.Fatal("tailFinalCWD found = true, want false")
	}
}

func TestTailFinalCWD_OutOfWindowTurnContextNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")

	var b strings.Builder
	b.WriteString(`{"timestamp":"2026-03-01T11:00:00.000Z","type":"turn_context","payload":{"cwd":"/tmp/early"}}` + "\n")
	// Pad well past the tail scan window with filler lines that aren't turn_context.
	padLine := `{"timestamp":"2026-03-01T11:00:00.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"` + strings.Repeat("x", 200) + `"}]}}` + "\n"
	for b.Len() < maxTailScanSize+4096 {
		b.WriteString(padLine)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, found := tailFinalCWD(path); found {
		t.Fatal("tailFinalCWD found = true, want false (the only turn_context lies outside the tail window)")
	}
}

func TestTailFinalCWD_MissingFile(t *testing.T) {
	if _, found := tailFinalCWD(filepath.Join(t.TempDir(), "missing.jsonl")); found {
		t.Fatal("tailFinalCWD found = true, want false for missing file")
	}
}

// --- shouldParseForCWD ---

func TestShouldParseForCWD_HeadMatches(t *testing.T) {
	dir := t.TempDir()
	path := writeRolloutFile(t, dir, 1, "sess-a-0000-0000-0000-000000000000", "/tmp/project-a")
	match := func(cwd string) bool { return cwd == "/tmp/project-a" }
	if !shouldParseForCWD(path, match) {
		t.Fatal("shouldParseForCWD = false, want true (head matches)")
	}
}

func TestShouldParseForCWD_TailConclusivelyRulesOut(t *testing.T) {
	dir := t.TempDir()
	path := writeRolloutFileRaw(t, dir, 1, "sess-drift-0000-0000-0000-000000000000", []string{
		`{"timestamp":"2026-03-01T11:00:00.000Z","type":"session_meta","payload":{"id":"s1","cwd":"/tmp/project-a"}}`,
		`{"timestamp":"2026-03-01T11:00:01.000Z","type":"turn_context","payload":{"cwd":"/tmp/project-c"}}`,
	})
	match := func(cwd string) bool { return cwd == "/tmp/project-b" }
	if shouldParseForCWD(path, match) {
		t.Fatal("shouldParseForCWD = true, want false (tail conclusively shows a different final cwd)")
	}
}

func TestShouldParseForCWD_TailInconclusiveFallsBackToParse(t *testing.T) {
	dir := t.TempDir()
	// writeRolloutFile emits session_meta + a response_item — no turn_context
	// at all, so the tail scan can't determine a final cwd.
	path := writeRolloutFile(t, dir, 1, "sess-only-meta-0000-0000-0000-000000000000", "/tmp/project-a")
	match := func(cwd string) bool { return cwd == "/tmp/project-b" }
	if !shouldParseForCWD(path, match) {
		t.Fatal("shouldParseForCWD = false, want true (no turn_context in tail window — conservative parse)")
	}
}

func TestShouldParseForCWD_HeadUnreadableFallsBackToParse(t *testing.T) {
	dir := t.TempDir()
	path := writeRolloutFile(t, dir, 1, "sess-c-0000-0000-0000-000000000000", "") // malformed head
	match := func(cwd string) bool { return false }
	if !shouldParseForCWD(path, match) {
		t.Fatal("shouldParseForCWD = false, want true (head unreadable — conservative parse)")
	}
}

// --- CWD drift end-to-end (the case the head-only prefilter used to get wrong) ---

func driftFixtureLines(finalCWD string, trailingEmptyTurnContext bool) []string {
	lines := []string{
		`{"timestamp":"2026-03-01T11:00:00.000Z","type":"session_meta","payload":{"id":"sess-drift","cwd":"/tmp/project-a","cli_version":"0.116.0","source":"cli"}}`,
		fmt.Sprintf(`{"timestamp":"2026-03-01T11:00:01.000Z","type":"turn_context","payload":{"cwd":"%s","model":"gpt-5.2-codex","git":{"branch":"main"}}}`, finalCWD),
	}
	if trailingEmptyTurnContext {
		lines = append(lines, `{"timestamp":"2026-03-01T11:00:02.000Z","type":"turn_context","payload":{"model":"gpt-5.2-codex","git":{"branch":"main"}}}`)
	} else {
		lines = append(lines, `{"timestamp":"2026-03-01T11:00:02.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`)
	}
	return lines
}

func TestDiscoverSessionsFiltered_CWDDriftMatchesFinalCWDViaTailScan(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "session_index.jsonl")

	path := writeRolloutFileRaw(t, dir, 1, "sess-drift-0000-0000-0000-000000000000", driftFixtureLines("/tmp/project-b", false))

	matchB := func(cwd string) bool { return cwd == "/tmp/project-b" }
	sessions, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), matchB, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 || sessions[0].JSONLPath != path {
		t.Fatalf("sessions = %#v, want the drifted session included (final cwd is project-b, even though session_meta said project-a)", sessions)
	}
	if sessions[0].CWD != "/tmp/project-b" {
		t.Fatalf("CWD = %q, want /tmp/project-b", sessions[0].CWD)
	}
}

func TestDiscoverSessionsFiltered_CWDDriftExcludesOriginalCWDViaPostParseFilter(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "session_index.jsonl")

	writeRolloutFileRaw(t, dir, 1, "sess-drift-0000-0000-0000-000000000000", driftFixtureLines("/tmp/project-b", false))

	// Head (session_meta) says project-a, so this matcher would have
	// wrongly included the session under the old head-only prefilter. The
	// session's *final* cwd (after the turn_context) is project-b, so it
	// must be excluded now that the post-parse filter is authoritative.
	matchA := func(cwd string) bool { return cwd == "/tmp/project-a" }
	sessions, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), matchA, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions = %#v, want empty: final cwd is project-b, not project-a, despite session_meta saying project-a", sessions)
	}
}

func TestDiscoverSessionsFiltered_EmptyTurnContextCWDDoesNotOverwriteFinal(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "session_index.jsonl")

	writeRolloutFileRaw(t, dir, 1, "sess-drift-0000-0000-0000-000000000000", driftFixtureLines("/tmp/project-b", true))

	matchB := func(cwd string) bool { return cwd == "/tmp/project-b" }
	sessions, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), matchB, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %#v, want 1 (final cwd should still be project-b — the trailing turn_context has no cwd and must not overwrite it)", sessions)
	}
	if sessions[0].CWD != "/tmp/project-b" {
		t.Fatalf("CWD = %q, want /tmp/project-b", sessions[0].CWD)
	}
}

// TestDiscoverSessionsFiltered_EquivalentToManualFilterOfFullDiscovery is an
// equivalence-property test: for a tree covering a plain match, a plain
// non-match, a malformed head, a cwd-drift file, and a no-turn_context file
// (the "tail scan inconclusive" branch), DiscoverSessionsFiltered's result
// must equal filtering a full (unfiltered) DiscoverSessions pass by the same
// matcher — proving the fast path never silently drops a session the slow
// path would have returned.
func TestDiscoverSessionsFiltered_EquivalentToManualFilterOfFullDiscovery(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "session_index.jsonl")

	writeRolloutFile(t, dir, 1, "sess-a-0000-0000-0000-000000000000", "/tmp/project-a")
	writeRolloutFile(t, dir, 2, "sess-b-0000-0000-0000-000000000000", "/tmp/project-b")
	writeRolloutFile(t, dir, 3, "sess-c-0000-0000-0000-000000000000", "") // malformed head
	writeRolloutFileRaw(t, dir, 4, "sess-drift-0000-0000-0000-000000000000", driftFixtureLines("/tmp/project-b", false))
	writeRolloutFileRaw(t, dir, 5, "sess-only-meta-0000-0000-0000-000000000000", []string{
		`{"timestamp":"2026-03-05T11:00:00.000Z","type":"session_meta","payload":{"id":"sess-only-meta","cwd":"/tmp/project-c","cli_version":"0.116.0","source":"cli"}}`,
	})

	matchers := map[string]func(string) bool{
		"project-a":      func(cwd string) bool { return cwd == "/tmp/project-a" },
		"project-b":      func(cwd string) bool { return cwd == "/tmp/project-b" },
		"nothing-at-all": func(cwd string) bool { return cwd == "/tmp/does-not-exist" },
	}

	for name, matcher := range matchers {
		t.Run(name, func(t *testing.T) {
			full, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), nil, nil)
			if err != nil {
				t.Fatalf("full discovery: unexpected error: %v", err)
			}
			var want []string
			for _, s := range full {
				if matcher(s.CWD) {
					want = append(want, s.JSONLPath)
				}
			}
			sort.Strings(want)

			got, err := discoverSessionsFromDir(dir, indexPath, model.NewSessionCache(), matcher, nil)
			if err != nil {
				t.Fatalf("filtered discovery: unexpected error: %v", err)
			}
			var gotPaths []string
			for _, s := range got {
				gotPaths = append(gotPaths, s.JSONLPath)
			}
			sort.Strings(gotPaths)

			if !slices.Equal(want, gotPaths) {
				t.Fatalf("filtered discovery = %v, want %v (must equal manual filter of full discovery)", gotPaths, want)
			}
		})
	}
}

func TestDiscoverSessionsFiltered_IncrementalCacheHitUsesPostParseFilterNotStaleCachedCWD(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "session_index.jsonl")

	path := writeRolloutFile(t, dir, 1, "sess-a-0000-0000-0000-000000000000", "/tmp/project-a")

	cache := model.NewSessionCache()
	// Prime the cache with a full parse — cached.CWD is now /tmp/project-a.
	if _, err := discoverSessionsFromDir(dir, indexPath, cache, nil, nil); err != nil {
		t.Fatalf("priming pass: unexpected error: %v", err)
	}

	// Append a turn_context that moves the session's cwd to project-b. This
	// grows the file, so the next GetIncremental returns (cached, offset>0)
	// rather than a full hit — cached.CWD (project-a) is now stale.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"timestamp":"2026-03-01T11:00:05.000Z","type":"turn_context","payload":{"cwd":"/tmp/project-b","model":"gpt-5.2-codex","git":{"branch":"main"}}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	matchB := func(cwd string) bool { return cwd == "/tmp/project-b" }
	sessions, err := discoverSessionsFromDir(dir, indexPath, cache, matchB, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 || sessions[0].CWD != "/tmp/project-b" {
		t.Fatalf("sessions = %#v, want the incrementally-updated session included with final cwd project-b (stale cached.CWD project-a must not be used to filter it out)", sessions)
	}
}
