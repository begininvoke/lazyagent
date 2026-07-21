package sessions

import "testing"

func TestRunRejectsUnknownAgent(t *testing.T) {
	if code := Run([]string{"--agent", "nope"}); code != 2 {
		t.Errorf("unknown agent: exit = %d, want 2", code)
	}
}

func TestRunRejectsMissingDir(t *testing.T) {
	if code := Run([]string{"--dir", "/nonexistent-lazyagent-test-dir"}); code != 2 {
		t.Errorf("missing dir: exit = %d, want 2", code)
	}
}

func TestRunHelp(t *testing.T) {
	if code := Run([]string{"--help"}); code != 0 {
		t.Errorf("--help: exit = %d, want 0", code)
	}
}
