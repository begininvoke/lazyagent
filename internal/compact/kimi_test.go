package compact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeKimiSessionForCompact(t *testing.T, big string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "kimi-work", "sess-1")
	mainDir := filepath.Join(dir, "agents", "main")
	subDir := filepath.Join(dir, "agents", "sub-1")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	args := jsonString(`{"path":"README.md","extra":"` + big + `"}`)
	wire := strings.Join([]string{
		`{"type":"metadata","protocol_version":"1.3","created_at":1700000000000}`,
		`{"type":"turn.prompt","input":[{"type":"text","text":` + jsonString(big) + `}],"origin":{"kind":"user"},"time":1700000001000}`,
		`{"type":"context.append_message","message":{"role":"user","content":[{"type":"text","text":` + jsonString(big) + `}],"toolCalls":[]}}`,
		`{"type":"context.append_message","message":{"role":"assistant","content":[{"type":"think","think":` + jsonString(big) + `},{"type":"text","text":"assistant text"}],"toolCalls":[{"type":"function","id":"call-1","function":{"name":"ReadFile","arguments":` + args + `}}]}}`,
		`{"type":"context.append_loop_event","event":{"type":"tool.call","toolCallId":"call-1","name":"ReadFile","args":{"path":"README.md","extra":` + jsonString(big) + `}},"time":1700000002000}`,
		`{"type":"context.append_loop_event","event":{"type":"tool.result","toolCallId":"call-1","result":{"output":` + jsonString(big) + `,"nested":["ok",` + jsonString(big) + `]}},"time":1700000003000}`,
		`{"type":"context.append_loop_event","event":{"type":"content.part","part":{"type":"text","text":"assistant text"}},"time":1700000004000}`,
	}, "\n") + "\n"

	files := map[string]string{
		filepath.Join(mainDir, "wire.jsonl"):             wire,
		filepath.Join(dir, "state.json"):                 `{"title":"Kimi session"}`,
		filepath.Join(subDir, "wire.jsonl"):              strings.ReplaceAll(wire, "call-1", "sub-call-1"),
		filepath.Join(subDir, "output"):                  big,
		filepath.Join(subDir, "prompt.txt"):              "prompt-stays",
		filepath.Join(subDir, "meta.json"):               `{"id":"sub-1"}`,
		filepath.Join(subDir, "unrelated.compact.tmp"):   "ignored",
		filepath.Join(subDir, "previous-output.log.bak"): "ignored",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCompactKimiSession(t *testing.T) {
	big := strings.Repeat("K", 50*1024)
	dir := writeKimiSessionForCompact(t, big)

	before := kimiLiveDirBytes(dir)
	newSize, err := compactKimiSession(dir, 10*1024, true)
	if err != nil {
		t.Fatalf("compactKimiSession: %v", err)
	}
	if newSize >= before {
		t.Errorf("size did not shrink: before=%d after=%d", before, newSize)
	}

	mainWire := filepath.Join(dir, "agents", "main", "wire.jsonl")
	subWire := filepath.Join(dir, "agents", "sub-1", "wire.jsonl")
	if n := countLines(t, mainWire); n != 7 {
		t.Errorf("main wire.jsonl line count = %d, want 7", n)
	}
	if n := countLines(t, subWire); n != 7 {
		t.Errorf("subagent wire.jsonl line count = %d, want 7", n)
	}

	data, _ := os.ReadFile(mainWire)
	if !strings.Contains(string(data), `"toolCallId":"call-1"`) {
		t.Error("toolCallId linkage was lost")
	}
	if !strings.Contains(string(data), `"id":"call-1"`) {
		t.Error("tool call id was lost")
	}
	if !strings.Contains(string(data), `"name":"ReadFile"`) {
		t.Error("tool name was lost")
	}

	for _, path := range []string{
		mainWire + ".bak",
		subWire + ".bak",
		filepath.Join(dir, "agents", "sub-1", "output.bak"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected backup %s", path)
		}
	}

	outputInfo, err := os.Stat(filepath.Join(dir, "agents", "sub-1", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if outputInfo.Size() >= int64(len(big)) {
		t.Errorf("subagent output did not shrink: size=%d, was %d", outputInfo.Size(), len(big))
	}
	if _, err := os.Stat(filepath.Join(dir, "agents", "sub-1", "prompt.txt.bak")); !os.IsNotExist(err) {
		t.Error("prompt.txt should not be compacted or backed up")
	}
}

func TestEstimateKimiSession(t *testing.T) {
	big := strings.Repeat("M", 50*1024)
	dir := writeKimiSessionForCompact(t, big)
	before := kimiLiveDirBytes(dir)

	after, err := estimateKimiSession(dir, 10*1024)
	if err != nil {
		t.Fatalf("estimateKimiSession: %v", err)
	}
	if after >= before {
		t.Errorf("estimate did not shrink: before=%d after=%d", before, after)
	}
	if kimiLiveDirBytes(dir) != before {
		t.Error("estimateKimiSession modified the session on disk")
	}
	if _, err := os.Stat(filepath.Join(dir, "agents", "main", "wire.jsonl.bak")); !os.IsNotExist(err) {
		t.Error("estimateKimiSession wrote a backup")
	}
}
