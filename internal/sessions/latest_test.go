package sessions

import (
	"strings"
	"testing"
)

func TestRunLatestRejectsUnknownAgent(t *testing.T) {
	if code := RunLatest([]string{"--agent", "nope"}); code != 2 {
		t.Errorf("unknown agent: exit = %d, want 2", code)
	}
}

func TestRunLatestRejectsMissingDir(t *testing.T) {
	if code := RunLatest([]string{"--dir", "/nonexistent-lazyagent-test-dir"}); code != 2 {
		t.Errorf("missing dir: exit = %d, want 2", code)
	}
}

func TestRunLatestHelp(t *testing.T) {
	if code := RunLatest([]string{"--help"}); code != 0 {
		t.Errorf("--help: exit = %d, want 0", code)
	}
}

func TestRunLatestRejectsUnknownFlag(t *testing.T) {
	if code := RunLatest([]string{"--bogus"}); code != 2 {
		t.Errorf("unknown flag: exit = %d, want 2", code)
	}
}

func TestStripControl(t *testing.T) {
	got := stripControl("~/pro\x1b[2Jj\x07\u009b")
	for _, r := range []rune{0x1b, 0x07, 0x9b} {
		if strings.ContainsRune(got, r) {
			t.Fatalf("stripControl() retained control rune %#x: %q", r, got)
		}
	}
	if got != "~/pro[2Jj" {
		t.Errorf("stripControl() = %q, want %q", got, "~/pro[2Jj")
	}
}
