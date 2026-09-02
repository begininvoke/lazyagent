package sessions

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestRunHistoryRejectsUnknownAgent(t *testing.T) {
	if code := RunHistory([]string{"--agent", "nope"}); code != 2 {
		t.Errorf("unknown agent: exit = %d, want 2", code)
	}
}

func TestRunHistoryRejectsMissingDir(t *testing.T) {
	if code := RunHistory([]string{"--dir", "/nonexistent-lazyagent-test-dir"}); code != 2 {
		t.Errorf("missing dir: exit = %d, want 2", code)
	}
}

func TestRunHistoryHelp(t *testing.T) {
	if code := RunHistory([]string{"--help"}); code != 0 {
		t.Errorf("--help: exit = %d, want 0", code)
	}
}

func TestRunHistoryRejectsUnknownFlag(t *testing.T) {
	if code := RunHistory([]string{"--bogus"}); code != 2 {
		t.Errorf("unknown flag: exit = %d, want 2", code)
	}
}

// historyFixture builds n sessions, most recent first, named s1..sn.
func historyFixture(n int) []*model.Session {
	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	out := make([]*model.Session, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, &model.Session{
			SessionID:     fmt.Sprintf("id-%02d", i+1),
			Agent:         "claude",
			Name:          fmt.Sprintf("s%d", i+1),
			TotalMessages: i + 1,
			LastActivity:  base.Add(-time.Duration(i) * time.Hour),
		})
	}
	return out
}

func nameByField(s *model.Session) string { return s.Name }

func TestRenderHistoryTruncatesToLimit(t *testing.T) {
	sessions := historyFixture(25)
	var buf bytes.Buffer
	renderHistory(&buf, sessions, nameByField, "~/proj", historyDefaultLimit)
	got := buf.String()

	if !strings.Contains(got, "s20") {
		t.Errorf("output missing 20th session; got:\n%s", got)
	}
	if strings.Contains(got, "s21") {
		t.Errorf("output should not include the 21st session; got:\n%s", got)
	}
	if !strings.Contains(got, "Showing 20 of 25 session(s) in ~/proj") {
		t.Errorf("footer missing truncation notice; got:\n%s", got)
	}
	if !strings.Contains(got, "--all") {
		t.Errorf("footer should point at --all; got:\n%s", got)
	}
}

func TestRenderHistoryNoLimitShowsEverything(t *testing.T) {
	sessions := historyFixture(25)
	var buf bytes.Buffer
	renderHistory(&buf, sessions, nameByField, "~/proj", 0)
	got := buf.String()

	if !strings.Contains(got, "s25") {
		t.Errorf("output missing last session; got:\n%s", got)
	}
	if !strings.Contains(got, "25 session(s) in ~/proj.") {
		t.Errorf("footer missing full count; got:\n%s", got)
	}
	if strings.Contains(got, "--all") {
		t.Errorf("footer should not mention --all when nothing is hidden; got:\n%s", got)
	}
}

// historyRowRE captures a data row's number cell and fixture name (sN):
// leading whitespace, row number, agent, name.
var historyRowRE = regexp.MustCompile(`^\s*(\d+)\s+claude\s+(s\d+)\b`)

func TestRenderHistoryOldestOnTopNewestAsRowOne(t *testing.T) {
	sessions := historyFixture(5)
	var buf bytes.Buffer
	renderHistory(&buf, sessions, nameByField, "~/proj", 0)

	var numbers, names []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if m := historyRowRE.FindStringSubmatch(line); m != nil {
			numbers = append(numbers, m[1])
			names = append(names, m[2])
		}
	}
	wantNames := []string{"s5", "s4", "s3", "s2", "s1"} // oldest first
	wantNumbers := []string{"5", "4", "3", "2", "1"}    // countdown to the newest
	if fmt.Sprint(names) != fmt.Sprint(wantNames) {
		t.Errorf("row order = %v, want %v", names, wantNames)
	}
	if fmt.Sprint(numbers) != fmt.Sprint(wantNumbers) {
		t.Errorf("row numbers = %v, want %v", numbers, wantNumbers)
	}
}

func TestRenderHistoryReturnsShownRows(t *testing.T) {
	sessions := historyFixture(25)
	var buf bytes.Buffer
	shown := renderHistory(&buf, sessions, nameByField, "~/proj", historyDefaultLimit)
	if len(shown) != historyDefaultLimit {
		t.Fatalf("returned %d rows, want %d", len(shown), historyDefaultLimit)
	}
	if shown[0].SessionID != "id-01" || shown[19].SessionID != "id-20" {
		t.Errorf("returned rows out of order: first %q, last %q", shown[0].SessionID, shown[19].SessionID)
	}
}

func TestRenderHistoryStripsControlSequences(t *testing.T) {
	sessions := []*model.Session{{
		SessionID:    "id-01",
		Agent:        "claude",
		Name:         "evil\x1b]0;pwned\x07title\x9b31mred",
		GitBranch:    "feat\x1b[2Jbranch",
		LastActivity: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	}}
	var buf bytes.Buffer
	renderHistory(&buf, sessions, nameByField, "~/proj", 0)
	got := buf.String()

	for _, r := range []rune{0x1b, 0x07, 0x9b} {
		if strings.ContainsRune(got, r) {
			t.Errorf("output contains control rune %#x; got:\n%q", r, got)
		}
	}
	// The introducer bytes are gone; what trailed them survives as inert
	// printable text.
	if !strings.Contains(got, "evil]0;pwnedtitle") {
		t.Errorf("title not neutralized in place; got:\n%q", got)
	}
	if !strings.Contains(got, "feat[2Jbranch") {
		t.Errorf("branch not neutralized in place; got:\n%q", got)
	}
}

func TestRenderHistoryUnderLimitOmitsHint(t *testing.T) {
	sessions := historyFixture(3)
	var buf bytes.Buffer
	renderHistory(&buf, sessions, nameByField, "~/proj", historyDefaultLimit)
	got := buf.String()

	if !strings.Contains(got, "3 session(s) in ~/proj.") {
		t.Errorf("footer missing full count; got:\n%s", got)
	}
	if strings.Contains(got, "--all") {
		t.Errorf("footer should not mention --all when nothing is hidden; got:\n%s", got)
	}
}
