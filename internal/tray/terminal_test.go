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

	cases := []struct {
		term string
		want []string
	}{
		// --single-instance forwards the window into the running kitty
		// (which must own the single-instance socket — see the kitty
		// launch-services note in docs); macOS kitty does not support a
		// functional second instance, so plain `open -n` is wrong.
		{"kitty", []string{"open", "-na", "kitty", "--args", "--single-instance", "--directory", cwd, "codex", "resume", "abc-123"}},
		{"ghostty", []string{"open", "-na", "Ghostty", "--args", "--working-directory=" + cwd, "-e", "codex", "resume", "abc-123"}},
		{"wezterm", []string{"open", "-na", "WezTerm", "--args", "start", "--cwd", cwd, "--", "codex", "resume", "abc-123"}},
		{"alacritty", []string{"open", "-na", "Alacritty", "--args", "--working-directory", cwd, "-e", "codex", "resume", "abc-123"}},
	}
	for _, c := range cases {
		if got := terminalCommand(c.term, cwd, argv); !reflect.DeepEqual(got, c.want) {
			t.Errorf("terminalCommand(%q) = %v, want %v", c.term, got, c.want)
		}
	}
}

func TestTerminalCommand_AppleScriptLaunchers(t *testing.T) {
	cwd := "/tmp/pro j"
	argv := []string{"claude", "--resume", "id-1"}

	for term, marker := range map[string]string{"terminal": `"Terminal"`, "iterm2": `"iTerm"`} {
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
