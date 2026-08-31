//go:build !notray && darwin

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

// terminalCommand returns the argv that opens argv inside the selected macOS
// terminal. term must already be normalized by core.NormalizeTerminal.
func terminalCommand(term, cwd string, argv []string) []string {
	switch term {
	case "kitty":
		// Direct binary, never `open`: kitty's launch-services command line may
		// replace `open --args`. A dedicated instance group also lets us raise
		// the correct kitty process after spawning it.
		return append([]string{kittyBinary(), "--single-instance", "--instance-group", "lazyagent", "--directory", cwd}, argv...)
	default: // "terminal" — macOS Terminal.app
		return []string{"osascript", "-e", fmt.Sprintf(`tell application "Terminal"
	activate
	do script "cd %s && %s"
end tell`, shellQuote(cwd), quotedJoin(argv))}
	}
}

func terminalStarted(c *exec.Cmd, term string) {
	if term == "kitty" {
		go activateSpawnedKitty(c)
		return
	}
	go func() { _ = c.Wait() }()
}

// activateSpawnedKitty raises the kitty window lazyagent just spawned. The
// process either remains as the lazyagent instance-group primary or forwards
// the window to an existing primary and exits; handle both cases and reap it.
func activateSpawnedKitty(c *exec.Cmd) {
	pid := c.Process.Pid
	done := make(chan struct{})
	go func() { _ = c.Wait(); close(done) }()
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

// kittyBinary resolves the executable from PATH first, then the standard app
// bundle locations used by native and Homebrew installations.
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
