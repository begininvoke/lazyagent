package sessions

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/illegalstudio/lazyagent/internal/chatops"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
	"github.com/mattn/go-isatty"
)

var validAgents = map[string]bool{
	"claude": true, "pi": true, "opencode": true, "kilo": true, "cursor": true,
	"codex": true, "amp": true, "grok": true, "kimi": true, "all": true,
}

// Run implements `lazyagent sessions`. It lists the sessions recorded for
// the current (or --dir) directory across agents and reopens the chosen one.
func Run(args []string) int {
	fs := flag.NewFlagSet("sessions", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	agent := fs.String("agent", "all", "Agent to list: claude, pi, opencode, kilo, cursor, codex, amp, grok, kimi, all")
	jsonOut := fs.Bool("json", false, "Print the session list as JSON and exit")
	dirFlag := fs.String("dir", "", "List sessions for this directory instead of the current one")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `lazyagent sessions — list sessions for a directory and reopen one

Lists every recorded session whose working directory is the current
directory (or --dir) or a subdirectory of it, across all agents.
Selecting a session resumes it with the originating agent's CLI.

Usage:
  lazyagent sessions
  lazyagent sessions --agent claude
  lazyagent sessions --json
  lazyagent sessions --dir ~/projects/foo

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if !validAgents[*agent] {
		fmt.Fprintf(os.Stderr, "Error: unknown --agent value %q (use claude, pi, opencode, kilo, cursor, codex, amp, grok, kimi, or all)\n", *agent)
		return 2
	}

	dir := *dirFlag
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot determine working directory: %v\n", err)
			return 2
		}
		dir = wd
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: %q is not a directory\n", dir)
		return 2
	}

	cfg := core.LoadConfig()
	provider := core.BuildProvider(*agent, cfg)
	all, err := provider.DiscoverSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	filtered, err := FilterByDir(all, dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	names := core.NewSessionNames()
	nameFor := func(s *model.Session) string {
		if alias := names.Get(s.SessionID); alias != "" {
			return alias
		}
		return s.Name
	}

	if *jsonOut {
		if err := writeJSON(os.Stdout, filtered, nameFor); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		return 0
	}

	if len(filtered) == 0 {
		fmt.Fprintf(os.Stderr, "No sessions found in %s.\n", abbreviateHome(dir))
		return 0
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stderr.Fd()) {
		fmt.Fprintln(os.Stderr, "Error: the interactive picker needs a terminal (use --json for scripted output)")
		return 2
	}

	titles := make([]string, len(filtered))
	for i, s := range filtered {
		titles[i] = titleFor(s, names.Get(s.SessionID))
	}
	chosen, action, err := runPicker(filtered, titles, abbreviateHome(dir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	switch action {
	case actionOpen:
		return openSession(chosen)
	case actionCopy:
		cmdStr := core.ResumeCommand(chosen.Agent, chosen.SessionID)
		if err := core.CopyToClipboard(cmdStr); err != nil {
			fmt.Fprintf(os.Stderr, "Copy failed: %v\nCommand: %s\n", err, cmdStr)
			return 1
		}
		fmt.Fprintf(os.Stderr, "Copied to clipboard: %s\n", cmdStr)
	}
	return 0
}

// openSession execs the agent's resume command in the current terminal,
// running from the session's own CWD when it still exists (claude --resume
// locates sessions by project directory).
func openSession(s *model.Session) int {
	argv := core.ResumeArgv(s.Agent, s.SessionID)
	if argv == nil {
		fmt.Fprintf(os.Stderr, "No resume command available for %s sessions.\n", s.Agent)
		return 1
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	if s.CWD != "" {
		if info, err := os.Stat(s.CWD); err == nil && info.IsDir() {
			cmd.Dir = s.CWD
		}
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Fprintf(os.Stderr, "%s %s\n", chatops.StyleMuted.Render("Opening:"), core.ResumeCommand(s.Agent, s.SessionID))
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

// abbreviateHome shortens a path with the user's home directory to ~/...
func abbreviateHome(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" && (path == home || strings.HasPrefix(path, home+"/")) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
