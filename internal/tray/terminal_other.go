//go:build !notray && !darwin && !linux

package tray

import "os/exec"

// Terminal launching has not yet been implemented for this platform.
func terminalCommand(_ string, _ string, _ []string) []string { return nil }

func terminalStarted(c *exec.Cmd, _ string) {
	go func() { _ = c.Wait() }()
}
