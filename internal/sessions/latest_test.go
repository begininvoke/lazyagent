package sessions

import "testing"

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
