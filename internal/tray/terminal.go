//go:build !notray

package tray

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// terminalCommand returns the argv to execute so that argv runs inside cwd
// in the user's chosen terminal emulator. term must already be normalized
// (core.NormalizeTerminal). Terminal.app and iTerm2 go through AppleScript
// and leave the shell open when the command exits; the others launch a new
// window via `open -n` whose lifetime is the command's.
func terminalCommand(term, cwd string, argv []string) []string {
	switch term {
	case "kitty":
		// Direct binary, never `open`: kitty's macos-launch-services-cmdline
		// file (if the user has one) REPLACES `open --args` arguments, so
		// LaunchServices launches can silently drop the command. A dedicated
		// --instance-group keeps all lazyagent windows in one extra kitty
		// instance regardless of how the user's own kitty runs; the spawned
		// instance cannot raise itself, so launchCommandInTerminal activates
		// it after the spawn (see activateSpawnedTerminal).
		return append([]string{kittyBinary(), "--single-instance", "--instance-group", "lazyagent", "--directory", cwd}, argv...)
	// Parked until verified on real setups (they also need re-enabling in
	// core.validTerminals and the Settings panel; ghostty/wezterm/alacritty
	// may share kitty's second-instance focus problem and need the same
	// direct-binary + activation treatment):
	// case "ghostty":
	// 	return append([]string{"open", "-na", "Ghostty", "--args", "--working-directory=" + cwd, "-e"}, argv...)
	// case "wezterm":
	// 	return append([]string{"open", "-na", "WezTerm", "--args", "start", "--cwd", cwd, "--"}, argv...)
	// case "alacritty":
	// 	return append([]string{"open", "-na", "Alacritty", "--args", "--working-directory", cwd, "-e"}, argv...)
	// case "iterm2":
	// 	return []string{"osascript", "-e", fmt.Sprintf(`tell application "iTerm"
	// create window with default profile
	// tell current session of current window
	// 	write text "cd %s && %s"
	// end tell
	// end tell`, shellQuote(cwd), quotedJoin(argv))}
	default: // "terminal" — macOS Terminal.app
		return []string{"osascript", "-e", fmt.Sprintf(`tell application "Terminal"
	activate
	do script "cd %s && %s"
end tell`, shellQuote(cwd), quotedJoin(argv))}
	}
}

// activateSpawnedKitty raises the kitty window lazyagent just spawned: a
// bare-exec'd kitty instance cannot bring itself to the foreground on
// macOS. Two outcomes of the spawn: the process became the lazyagent-group
// primary (still alive after the grace period → activate its own pid), or
// it forwarded the window to the existing group primary and exited (find
// that primary via pgrep). Activation repeats once because the OS window
// can lag the process start.
func activateSpawnedKitty(c *exec.Cmd) {
	pid := c.Process.Pid
	done := make(chan struct{})
	go func() { _ = c.Wait(); close(done) }() // reap either way
	select {
	case <-done:
		out, err := exec.Command("pgrep", "-f", "--instance-group lazyagent").Output()
		if err != nil {
			return
		}
		fields := strings.Fields(string(out))
		if len(fields) == 0 {
			return
		}
		if p, err := strconv.Atoi(fields[0]); err == nil {
			activateProcess(p)
			time.Sleep(600 * time.Millisecond)
			activateProcess(p)
		}
	case <-time.After(700 * time.Millisecond):
		activateProcess(pid)
		time.Sleep(1200 * time.Millisecond)
		activateProcess(pid)
	}
}

// kittyBinary resolves the kitty executable: PATH first (brew links it),
// then the standard app bundle locations.
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
