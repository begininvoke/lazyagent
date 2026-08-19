//go:build !notray

package tray

import (
	"reflect"
	"strings"
	"testing"
)

func TestTerminalCommand_DirectLaunchers(t *testing.T) {
	cwd := "/tmp/proj"
	argv := []string{"codex", "resume", "abc-123"}

	// kitty: direct binary (never `open` — kitty's launch-services cmdline
	// file, if the user has one, replaces `open --args` arguments) in a
	// dedicated lazyagent instance group. The first window creates the
	// group's instance, later ones join it. Path is resolved, so assert
	// suffix + tail.
	kittyGot := terminalCommand("kitty", cwd, argv)
	kittyTail := []string{"--single-instance", "--instance-group", "lazyagent", "--directory", cwd, "codex", "resume", "abc-123"}
	if len(kittyGot) < 1 || !strings.HasSuffix(kittyGot[0], "kitty") {
		t.Errorf("kitty argv[0] = %v, want a kitty binary path", kittyGot)
	} else if !reflect.DeepEqual(kittyGot[1:], kittyTail) {
		t.Errorf("kitty args = %v, want %v", kittyGot[1:], kittyTail)
	}

	// ghostty/wezterm/alacritty expectations are parked with their
	// implementations — restore them here when the presets return.
}

func TestTerminalCommand_AppleScriptLaunchers(t *testing.T) {
	cwd := "/tmp/pro j"
	argv := []string{"claude", "--resume", "id-1"}

	for term, marker := range map[string]string{"terminal": `"Terminal"`} {
		got := terminalCommand(term, cwd, argv)
		if len(got) < 3 || got[0] != "osascript" || got[1] != "-e" {
			t.Fatalf("terminalCommand(%q) = %v, want osascript -e <script>", term, got)
		}
		script := got[2]
		if !strings.Contains(script, marker) {
			t.Errorf("%s script missing %s: %s", term, marker, script)
		}
		// cwd with a space must be shell-quoted; argv elements quoted too.
		if !strings.Contains(script, "'/tmp/pro j'") {
			t.Errorf("%s script missing quoted cwd: %s", term, script)
		}
		if !strings.Contains(script, "'claude' '--resume' 'id-1'") {
			t.Errorf("%s script missing quoted command: %s", term, script)
		}
	}
}
