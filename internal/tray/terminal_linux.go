//go:build !notray && linux

package tray

import "os/exec"

type terminalPathLookup func(string) (string, error)

// terminalCommand opens argv in the user's selected Linux terminal. The
// freedesktop xdg-terminal-exec launcher wins when available because it
// respects the desktop's configured default; explicit fallbacks cover the
// common terminals on distributions that do not ship it yet.
func terminalCommand(term, cwd string, argv []string) []string {
	return linuxTerminalCommand(term, cwd, argv, exec.LookPath)
}

func linuxTerminalCommand(term, cwd string, argv []string, lookup terminalPathLookup) []string {
	if len(argv) == 0 {
		return nil
	}

	find := func(name string) string {
		path, err := lookup(name)
		if err != nil {
			return ""
		}
		return path
	}

	if term == "kitty" {
		if path := find("kitty"); path != "" {
			return append([]string{path, "--directory", cwd}, argv...)
		}
	}

	if path := find("xdg-terminal-exec"); path != "" {
		return append([]string{path, "--app-id=lazyagent", "--title=Lazyagent", "--dir=" + cwd, "--"}, argv...)
	}

	// Debian's alternatives system exposes the user's preferred emulator
	// under this name. It has no portable cwd flag, so run a quoted shell
	// command after opening the terminal.
	if path := find("x-terminal-emulator"); path != "" {
		return shellTerminalCommand(path, []string{"-e"}, cwd, argv)
	}

	if path := find("kitty"); path != "" {
		return append([]string{path, "--directory", cwd}, argv...)
	}
	if path := find("ghostty"); path != "" {
		return append([]string{path, "--working-directory=" + cwd, "-e"}, argv...)
	}
	if path := find("wezterm"); path != "" {
		return append([]string{path, "start", "--cwd", cwd, "--"}, argv...)
	}
	if path := find("alacritty"); path != "" {
		return append([]string{path, "--working-directory", cwd, "-e"}, argv...)
	}
	if path := find("gnome-terminal"); path != "" {
		return append([]string{path, "--working-directory=" + cwd, "--"}, argv...)
	}
	if path := find("kgx"); path != "" {
		return append([]string{path, "--working-directory=" + cwd, "--"}, argv...)
	}
	if path := find("konsole"); path != "" {
		return append([]string{path, "--workdir", cwd, "-e"}, argv...)
	}
	if path := find("xfce4-terminal"); path != "" {
		return []string{path, "--working-directory=" + cwd, "--command=" + quotedJoin(argv)}
	}
	if path := find("xterm"); path != "" {
		return shellTerminalCommand(path, []string{"-e"}, cwd, argv)
	}

	return nil
}

func terminalStarted(c *exec.Cmd, _ string) {
	go func() { _ = c.Wait() }()
}

func shellTerminalCommand(path string, prefix []string, cwd string, argv []string) []string {
	command := "cd " + shellQuote(cwd) + " && exec " + quotedJoin(argv)
	result := append([]string{path}, prefix...)
	return append(result, "sh", "-lc", command)
}
