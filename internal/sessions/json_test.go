package sessions

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestWriteJSONFields(t *testing.T) {
	last := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	list := []*model.Session{{
		Agent: "claude", SessionID: "abc", CWD: "/proj",
		LastActivity: last, TotalMessages: 5,
	}}
	var buf bytes.Buffer
	if err := writeJSON(&buf, list, func(*model.Session) string { return "my-alias" }); err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 entry, got %d", len(out))
	}
	e := out[0]
	if e["agent"] != "claude" || e["session_id"] != "abc" || e["cwd"] != "/proj" {
		t.Errorf("identity fields wrong: %v", e)
	}
	if e["name"] != "my-alias" {
		t.Errorf("name = %v, want my-alias", e["name"])
	}
	if e["messages"] != float64(5) {
		t.Errorf("messages = %v, want 5", e["messages"])
	}
	if e["resume_command"] != "claude --resume abc" {
		t.Errorf("resume_command = %v", e["resume_command"])
	}
	if _, ok := e["last_activity"]; !ok {
		t.Error("missing last_activity")
	}
}

func TestWriteJSONEmptyList(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, nil, func(*model.Session) string { return "" }); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "[]" {
		t.Errorf("empty list must encode as [], got %q", got)
	}
}
