//go:build !notray

package tray

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// terminalCommand returns the argv to execute so that argv runs inside cwd
// in the user's chosen terminal emulator. term must already be normalized
// (core.NormalizeTerminal). Terminal.app and iTerm2 go through AppleScript
// and leave the shell open when the command exits; the others launch a new
// window via `open -n` whose lifetime is the command's.
func terminalCommand(term, cwd string, argv []string) []string {
	switch term {
	case "kitty":
		// Direct binary + --single-instance: the window opens inside the
		// already-running kitty. A second instance via `open -n` gets its
		// own Cmd-Tab icon and broken keyboard focus (kitty issue #385).
		return append([]string{kittyBinary(), "--single-instance", "--directory", cwd}, argv...)
	case "ghostty":
		return append([]string{"open", "-na", "Ghostty", "--args", "--working-directory=" + cwd, "-e"}, argv...)
	case "wezterm":
		return append([]string{"open", "-na", "WezTerm", "--args", "start", "--cwd", cwd, "--"}, argv...)
	case "alacritty":
		return append([]string{"open", "-na", "Alacritty", "--args", "--working-directory", cwd, "-e"}, argv...)
	case "iterm2":
		return []string{"osascript", "-e", fmt.Sprintf(`tell application "iTerm"
	create window with default profile
	tell current session of current window
		write text "cd %s && %s"
	end tell
end tell`, shellQuote(cwd), quotedJoin(argv))}
	default: // "terminal" — macOS Terminal.app
		return []string{"osascript", "-e", fmt.Sprintf(`tell application "Terminal"
	activate
	do script "cd %s && %s"
end tell`, shellQuote(cwd), quotedJoin(argv))}
	}
}

// kittyBinary resolves the kitty executable: PATH first (brew installs a
// symlink), then the standard app bundle locations.
func kittyBinary() string {
	if p, err := exec.LookPath("kitty"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, "Applications/kitty.app/Contents/MacOS/kitty")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "/Applications/kitty.app/Contents/MacOS/kitty"
}

// quotedJoin shell-quotes every argv element and joins them with spaces.
func quotedJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = shellQuote(a)
	}
	return strings.Join(parts, " ")
}
