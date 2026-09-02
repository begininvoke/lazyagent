package sessions

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/illegalstudio/lazyagent/internal/chatops"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
	"github.com/mattn/go-isatty"
)

// historyDefaultLimit is how many sessions `lazyagent history` shows unless
// --all is given.
const historyDefaultLimit = 20

// RunHistory implements `lazyagent history`. It prints a table of the
// sessions recorded for the current (or --dir) directory across agents,
// most recent first, then — in a terminal — offers to resume one by row
// number. Sibling of Run (the `sessions` picker): same discovery, same
// directory filter, but a plain table plus a one-shot prompt instead of an
// interactive picker.
func RunHistory(args []string) int {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	agent := fs.String("agent", "all", "Agent to list: claude, pi, opencode, kilo, cursor, codex, amp, grok, kimi, all")
	showAll := fs.Bool("all", false, fmt.Sprintf("Show every session instead of the %d most recent", historyDefaultLimit))
	dirFlag := fs.String("dir", "", "Show history for this directory instead of the current one")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `lazyagent history — show past sessions for a directory

Prints a table of every recorded session whose working directory is the
current directory (or --dir) or a subdirectory of it, across all agents —
oldest at the top, most recent at the bottom as row #1. Only the %d most
recent are shown unless --all is given. In a terminal, entering a row
number then resumes that session with the originating agent's CLI; press
Enter or ctrl+c to quit instead.

Usage:
  lazyagent history
  lazyagent history --all
  lazyagent history --agent claude
  lazyagent history --dir ~/projects/foo

Flags:
`, historyDefaultLimit)
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

	dir, err := resolveTargetDir(*dirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}

	filtered, code := discoverDirSessions(*agent, dir)
	if code != 0 {
		return code
	}

	if len(filtered) == 0 {
		fmt.Fprintf(os.Stderr, "No sessions found in %s.\n", abbreviateHome(dir))
		return 0
	}

	names := core.NewSessionNames()
	nameFor := func(s *model.Session) string {
		return titleFor(s, names.Get(s.SessionID))
	}
	limit := historyDefaultLimit
	if *showAll {
		limit = 0
	}
	shown := renderHistory(os.Stdout, filtered, nameFor, abbreviateHome(dir), limit)
	return promptResumeSession(shown)
}

// promptResumeSession asks for a row number and resumes the chosen session.
// Skipped entirely (exit 0) when stdin/stdout are not terminals, so piped
// output stays non-interactive — same rule as search's promptOpenResult.
// Empty input, EOF, and ctrl+c (SIGINT's default handling) all quit without
// resuming.
func promptResumeSession(shown []*model.Session) int {
	if len(shown) == 0 || !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) {
		return 0
	}
	fmt.Printf("\n%s ", chatops.StyleMuted.Render("Resume a session? Enter row #, or press Enter to quit:"))
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println()
		return 0
	}
	answer := strings.TrimSpace(line)
	if answer == "" {
		return 0
	}
	n, err := strconv.Atoi(answer)
	if err != nil || n < 1 || n > len(shown) {
		fmt.Fprintf(os.Stderr, "Invalid selection %q.\n", answer)
		return 2
	}
	return openSession(shown[n-1])
}

// renderHistory prints the history table plus a footer line and returns the
// slice of sessions actually rendered, newest first — the prompt's row
// numbers index into it (#1 = shown[0]). Rows are printed oldest at the
// top, newest at the bottom with row number 1, so the most recent session
// sits right above the resume prompt. limit <= 0 means "show everything";
// otherwise only the first limit sessions are rendered and the footer
// points at --all. sessions must already be sorted most recent first
// (FilterByDir's order).
func renderHistory(w io.Writer, sessions []*model.Session, nameFor func(*model.Session) string, dirLabel string, limit int) []*model.Session {
	total := len(sessions)
	shown := sessions
	if limit > 0 && total > limit {
		shown = sessions[:limit]
	}

	t := chatops.NewTable().Headers("#", "AGENT", "SESSION", "BRANCH", "MSGS", "LAST ACTIVITY")
	for i := len(shown) - 1; i >= 0; i-- {
		s := shown[i]
		t.Row(
			chatops.StyleMuted.Render(strconv.Itoa(i+1)),
			chatops.StyleAgent.Render(s.Agent),
			truncate(stripControl(nameFor(s)), 60),
			branchLabel(stripControl(s.GitBranch)),
			chatops.StyleCount.Render(strconv.Itoa(s.TotalMessages)),
			chatops.StyleMuted.Render(historyFormatWhen(s.LastActivity)),
		)
	}
	fmt.Fprintln(w, t)

	if len(shown) < total {
		fmt.Fprintln(w, chatops.StyleFooter.Render(fmt.Sprintf(
			"Showing %d of %d session(s) in %s — use --all to see every session.",
			len(shown), total, dirLabel,
		)))
		return shown
	}
	fmt.Fprintln(w, chatops.StyleFooter.Render(fmt.Sprintf(
		"%d session(s) in %s.", total, dirLabel,
	)))
	return shown
}

// stripControl removes control runes — C0 (including ESC and BEL), DEL, and
// C1 (including the CSI and OSC introducers) — so session titles and branch
// names, which come from persisted transcript content an agent's
// counterparty can influence, cannot inject terminal escape sequences
// through the table. Printable text passes through untouched; truncate's
// whitespace collapsing handles tabs and newlines separately.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}

func branchLabel(branch string) string {
	if branch == "" {
		return chatops.StyleMuted.Render("-")
	}
	return branch
}

func historyFormatWhen(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}
