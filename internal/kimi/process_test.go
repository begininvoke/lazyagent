package kimi

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// TestParseImportedSession covers sessions migrated from the legacy kimi-cli
// into ~/.kimi-code. Their whole conversation is materialized as
// context.append_message events (old tool names, no token records).
func TestParseImportedSession(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "ses_1")
	writeImportedSession(t, sessionDir)

	session, _, err := ParseSession(sessionDir, "/tmp/project")
	if err != nil {
		t.Fatalf("ParseSession() error = %v", err)
	}

	if session.Agent != "kimi" {
		t.Fatalf("Agent = %q, want kimi", session.Agent)
	}
	if session.SessionID != "ses_1" {
		t.Fatalf("SessionID = %q, want ses_1", session.SessionID)
	}
	if session.CWD != "/tmp/project" {
		t.Fatalf("CWD = %q, want /tmp/project", session.CWD)
	}
	if session.Name != "Custom title" {
		t.Fatalf("Name = %q, want Custom title", session.Name)
	}
	if session.Version != "1.10" {
		t.Fatalf("Version = %q, want 1.10", session.Version)
	}
	if session.Status != model.StatusWaitingForUser {
		t.Fatalf("Status = %v, want waiting", session.Status)
	}
	if session.UserMessages != 1 || session.AssistantMessages != 1 || session.TotalMessages != 2 {
		t.Fatalf("message counts = %d/%d/%d, want 1/1/2", session.UserMessages, session.AssistantMessages, session.TotalMessages)
	}
	if len(session.RecentMessages) != 2 {
		t.Fatalf("RecentMessages len = %d, want 2", len(session.RecentMessages))
	}
	if got := session.RecentMessages[0].Text; got != "please edit" {
		t.Fatalf("first message = %q, want please edit", got)
	}
	if got := session.RecentMessages[1].Text; got != "done" {
		t.Fatalf("second message = %q, want done", got)
	}
	if len(session.RecentTools) != 1 || session.RecentTools[0].Name != "WriteFile" {
		t.Fatalf("RecentTools = %+v, want WriteFile", session.RecentTools)
	}
	if session.LastFileWrite != "/tmp/project/out.txt" {
		t.Fatalf("LastFileWrite = %q, want /tmp/project/out.txt", session.LastFileWrite)
	}
	if session.InputTokens != 0 || session.OutputTokens != 0 {
		t.Fatalf("tokens = input %d output %d, want 0/0 (imported has no usage)", session.InputTokens, session.OutputTokens)
	}
	wantLast := time.Unix(1700000000, 0)
	if !session.LastActivity.Equal(wantLast) {
		t.Fatalf("LastActivity = %v, want %v", session.LastActivity, wantLast)
	}
}

// TestParseNativeSession covers sessions created by the new kimi-code binary.
// User turns are context.append_message; assistant content, tools and tokens
// stream through context.append_loop_event / usage.record events.
func TestParseNativeSession(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "ses_2")
	writeNativeSession(t, sessionDir)

	session, _, err := ParseSession(sessionDir, "/tmp/project")
	if err != nil {
		t.Fatalf("ParseSession() error = %v", err)
	}

	if session.Version != "1.3" {
		t.Fatalf("Version = %q, want 1.3", session.Version)
	}
	if session.Model != "kimi-code/kimi-for-coding" {
		t.Fatalf("Model = %q, want kimi-code/kimi-for-coding", session.Model)
	}
	if session.Name != "native title" {
		t.Fatalf("Name = %q, want native title", session.Name)
	}
	// turn.prompt and context.append_message both carry the user input; only
	// one must be counted.
	if session.UserMessages != 1 || session.AssistantMessages != 1 || session.TotalMessages != 2 {
		t.Fatalf("message counts = %d/%d/%d, want 1/1/2", session.UserMessages, session.AssistantMessages, session.TotalMessages)
	}
	if len(session.RecentMessages) != 2 {
		t.Fatalf("RecentMessages len = %d, want 2", len(session.RecentMessages))
	}
	if session.RecentMessages[0].Text != "hi there" || session.RecentMessages[1].Text != "all done" {
		t.Fatalf("RecentMessages = %+v, want [hi there, all done]", session.RecentMessages)
	}
	if len(session.RecentTools) != 1 || session.RecentTools[0].Name != "Edit" {
		t.Fatalf("RecentTools = %+v, want Edit", session.RecentTools)
	}
	if session.LastFileWrite != "/tmp/project/main.go" {
		t.Fatalf("LastFileWrite = %q, want /tmp/project/main.go", session.LastFileWrite)
	}
	if session.InputTokens != 15 || session.OutputTokens != 7 || session.CacheReadTokens != 3 || session.CacheCreationTokens != 2 {
		t.Fatalf("tokens = input %d output %d cacheRead %d cacheCreate %d, want 15/7/3/2",
			session.InputTokens, session.OutputTokens, session.CacheReadTokens, session.CacheCreationTokens)
	}
	if session.Status != model.StatusWaitingForUser {
		t.Fatalf("Status = %v, want waiting", session.Status)
	}
	wantLast := time.Unix(1700000008, 0)
	if !session.LastActivity.Equal(wantLast) {
		t.Fatalf("LastActivity = %v, want %v", session.LastActivity, wantLast)
	}
}

func TestDiscoverSessionsResolvesWorkDirFromIndex(t *testing.T) {
	root := t.TempDir()
	sessionsRoot := filepath.Join(root, "sessions")
	workDir := "/tmp/kimi-project"
	sessionDir := filepath.Join(sessionsRoot, "wd_project_abc123", "ses_1")
	writeImportedSession(t, sessionDir)

	indexPath := filepath.Join(root, "session_index.jsonl")
	line := fmt.Sprintf(`{"sessionId":"ses_1","sessionDir":%q,"workDir":%q}`+"\n", sessionDir, workDir)
	if err := os.WriteFile(indexPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	sessions, err := discoverSessionsFromDir(sessionsRoot, indexPath, model.NewSessionCache())
	if err != nil {
		t.Fatalf("discoverSessionsFromDir() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].CWD != workDir {
		t.Fatalf("CWD = %q, want %q", sessions[0].CWD, workDir)
	}
	if sessions[0].SessionID != "ses_1" {
		t.Fatalf("SessionID = %q, want ses_1", sessions[0].SessionID)
	}
}

func TestExtractContextChunksImported(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "ses_1")
	writeImportedSession(t, sessionDir)

	chunks, err := ExtractContextChunks(sessionDir, "/tmp/project")
	if err != nil {
		t.Fatalf("ExtractContextChunks() error = %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0].Role != "user" || chunks[0].Text != "please edit" {
		t.Fatalf("first chunk = %+v, want user please edit", chunks[0])
	}
	if chunks[1].Role != "assistant" || chunks[1].Text != "done" {
		t.Fatalf("second chunk = %+v, want assistant done", chunks[1])
	}
	if chunks[0].Name != "Custom title" {
		t.Fatalf("Name = %q, want Custom title", chunks[0].Name)
	}
}

func TestExtractContextChunksNative(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "ses_2")
	writeNativeSession(t, sessionDir)

	chunks, err := ExtractContextChunks(sessionDir, "/tmp/project")
	if err != nil {
		t.Fatalf("ExtractContextChunks() error = %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2: %+v", len(chunks), chunks)
	}
	if chunks[0].Role != "user" || chunks[0].Text != "hi there" {
		t.Fatalf("first chunk = %+v, want user hi there", chunks[0])
	}
	if chunks[1].Role != "assistant" || chunks[1].Text != "all done" {
		t.Fatalf("second chunk = %+v, want assistant all done", chunks[1])
	}
}

func writeImportedSession(t *testing.T, sessionDir string) {
	t.Helper()
	wire := `{"type":"metadata","protocol_version":"1.10","created_at":1700000000000}
{"type":"context.append_message","message":{"role":"user","content":[{"type":"text","text":"please edit"}],"toolCalls":[]}}
{"type":"context.append_message","message":{"role":"assistant","content":[{"type":"think","think":"hidden"}],"toolCalls":[{"type":"function","id":"tool-1","function":{"name":"WriteFile","arguments":"{\"path\":\"out.txt\",\"content\":\"hello\"}"}}]}}
{"type":"context.append_message","message":{"role":"tool","content":[{"type":"text","text":"noisy output"}],"toolCallId":"tool-1"}}
{"type":"context.append_message","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"toolCalls":[]}}
`
	writeWire(t, sessionDir, wire)
	writeState(t, sessionDir, `{"title":"Custom title","isCustomTitle":true}`)
}

func writeNativeSession(t *testing.T, sessionDir string) {
	t.Helper()
	wire := `{"type":"metadata","protocol_version":"1.3","created_at":1700000000000,"app_version":"0.11.0"}
{"type":"config.update","modelAlias":"kimi-code/kimi-for-coding","thinkingLevel":"high","time":1700000001000}
{"type":"turn.prompt","input":[{"type":"text","text":"hi there"}],"origin":{"kind":"user"},"time":1700000002000}
{"type":"context.append_message","message":{"role":"user","content":[{"type":"text","text":"hi there"}],"toolCalls":[]}}
{"type":"context.append_loop_event","event":{"type":"step.begin","turnId":"0","step":1},"time":1700000003000}
{"type":"context.append_loop_event","event":{"type":"content.part","part":{"type":"think","think":"reasoning"}},"time":1700000004000}
{"type":"context.append_loop_event","event":{"type":"tool.call","toolCallId":"t1","name":"Edit","args":{"file_path":"main.go"}},"time":1700000005000}
{"type":"context.append_loop_event","event":{"type":"tool.result","toolCallId":"t1","result":{"output":"ok"}},"time":1700000006000}
{"type":"context.append_loop_event","event":{"type":"content.part","part":{"type":"text","text":"all done"}},"time":1700000007000}
{"type":"context.append_loop_event","event":{"type":"step.end","step":1,"usage":{"inputOther":15,"output":7,"inputCacheRead":3,"inputCacheCreation":2},"finishReason":"stop"},"time":1700000008000}
{"type":"usage.record","model":"kimi-code/kimi-for-coding","usage":{"inputOther":15,"output":7,"inputCacheRead":3,"inputCacheCreation":2},"usageScope":"turn","time":1700000008000}
`
	writeWire(t, sessionDir, wire)
	writeState(t, sessionDir, `{"title":"native title","isCustomTitle":true}`)
}

func writeWire(t *testing.T, sessionDir, wire string) {
	t.Helper()
	wireDir := filepath.Join(sessionDir, "agents", "main")
	if err := os.MkdirAll(wireDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wireDir, "wire.jsonl"), []byte(wire), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeState(t *testing.T, sessionDir, state string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(sessionDir, "state.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
}
