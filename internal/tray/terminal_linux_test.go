//go:build !notray && linux

package tray

import (
	"errors"
	"reflect"
	"testing"
)

func lookupOnly(names ...string) terminalPathLookup {
	available := make(map[string]bool, len(names))
	for _, name := range names {
		available[name] = true
	}
	return func(name string) (string, error) {
		if available[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func TestLinuxTerminalCommandPrefersDesktopDefault(t *testing.T) {
	got := linuxTerminalCommand("terminal", "/tmp/pro j", []string{"codex", "resume", "abc"}, lookupOnly("xdg-terminal-exec", "kitty"))
	want := []string{"/usr/bin/xdg-terminal-exec", "--app-id=lazyagent", "--title=Lazyagent", "--dir=/tmp/pro j", "--", "codex", "resume", "abc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("linuxTerminalCommand() = %v, want %v", got, want)
	}
}

func TestLinuxTerminalCommandExplicitKitty(t *testing.T) {
	got := linuxTerminalCommand("kitty", "/tmp/proj", []string{"claude", "--resume", "id"}, lookupOnly("xdg-terminal-exec", "kitty"))
	want := []string{"/usr/bin/kitty", "--directory", "/tmp/proj", "claude", "--resume", "id"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("linuxTerminalCommand() = %v, want %v", got, want)
	}
}

func TestLinuxTerminalCommandMissingKittyUsesDesktopDefault(t *testing.T) {
	got := linuxTerminalCommand("kitty", "/tmp/proj", []string{"claude"}, lookupOnly("xdg-terminal-exec"))
	want := []string{"/usr/bin/xdg-terminal-exec", "--app-id=lazyagent", "--title=Lazyagent", "--dir=/tmp/proj", "--", "claude"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("linuxTerminalCommand() = %v, want %v", got, want)
	}
}

func TestLinuxTerminalCommandFallbacks(t *testing.T) {
	argv := []string{"my editor", "a'b"}
	got := linuxTerminalCommand("terminal", "/tmp/pro j", argv, lookupOnly("x-terminal-emulator"))
	want := []string{"/usr/bin/x-terminal-emulator", "-e", "sh", "-lc", "cd '/tmp/pro j' && exec 'my editor' 'a'\\''b'"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("linuxTerminalCommand() = %v, want %v", got, want)
	}

	if got := linuxTerminalCommand("terminal", "/tmp/proj", argv, lookupOnly()); got != nil {
		t.Fatalf("no emulator: got %v, want nil", got)
	}
}
