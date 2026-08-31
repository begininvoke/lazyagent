//go:build !notray && darwin

package tray

import (
	"reflect"
	"strings"
	"testing"
)

func TestTerminalCommand_DirectLauncher(t *testing.T) {
	cwd := "/tmp/proj"
	argv := []string{"codex", "resume", "abc-123"}

	got := terminalCommand("kitty", cwd, argv)
	wantTail := []string{"--single-instance", "--instance-group", "lazyagent", "--directory", cwd, "codex", "resume", "abc-123"}
	if len(got) < 1 || !strings.HasSuffix(got[0], "kitty") {
		t.Errorf("kitty argv[0] = %v, want a kitty binary path", got)
	} else if !reflect.DeepEqual(got[1:], wantTail) {
		t.Errorf("kitty args = %v, want %v", got[1:], wantTail)
	}
}

func TestTerminalCommand_AppleScriptLauncher(t *testing.T) {
	cwd := "/tmp/pro j"
	argv := []string{"claude", "--resume", "id-1"}

	got := terminalCommand("terminal", cwd, argv)
	if len(got) < 3 || got[0] != "osascript" || got[1] != "-e" {
		t.Fatalf("terminalCommand() = %v, want osascript -e <script>", got)
	}
	script := got[2]
	for _, expected := range []string{`"Terminal"`, "'/tmp/pro j'", "'claude' '--resume' 'id-1'"} {
		if !strings.Contains(script, expected) {
			t.Errorf("script missing %s: %s", expected, script)
		}
	}
}
