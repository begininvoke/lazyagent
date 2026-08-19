//go:build !notray && !darwin

package tray

// setDesktopActivation is a no-op off macOS: activation policies and the
// Dock are AppKit concepts.
func setDesktopActivation(regular bool) {}

// activateProcess is a no-op off macOS.
func activateProcess(pid int) {}
