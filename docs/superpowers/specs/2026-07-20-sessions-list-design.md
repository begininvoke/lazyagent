# `lazyagent sessions` — Design

**Date:** 2026-07-20
**Branch:** `feat/sessions-list`

## Goal

A new subcommand that lists every session that happened in the current
directory — across all supported coding agents — and lets the user reopen one:

```
lazyagent sessions [--agent <name>] [--json] [--dir <path>]
```

The default invocation opens an interactive TUI picker; selecting a session
resumes it in the current terminal with the originating agent's own CLI.

## Decisions (confirmed)

1. **Interactive picker** (bubbletea), not a numbered-prompt list and not a
   flag-only list.
2. **Directory matching: exact CWD + subdirectories.** A session belongs to the
   listing if its recorded `CWD` equals the target directory or lives beneath
   it. No whole-git-repo / linked-worktree expansion.
3. **Show all agents, disable open where unavailable.** Sessions from agents
   without an executable resume command still appear (it is a historical
   listing); pressing enter on them shows a footer message and falls back to
   copying the resume command to the clipboard when one exists.
4. **Flags from day one:** `--agent`, `--json`, `--dir`.

## Command surface

- **Default:** TUI picker over sessions whose CWD matches the current
  directory (or `--dir`), all agents, sorted by last activity descending.
  Sidechain (sub-agent) sessions are excluded — they are noise in a
  historical listing.
- **`--agent claude|pi|opencode|kilo|cursor|codex|amp|grok|kimi`** — restrict
  to one agent. Same validation and error message style as `main.go`'s
  `--agent` flag. Default: all agents.
- **`--dir <path>`** — list sessions for that directory instead of
  `os.Getwd()`.
- **`--json`** — print the filtered list as JSON on stdout and exit; no
  picker. Per-session fields: `agent`, `session_id`, `name`, `cwd`,
  `last_activity`, `messages`, `resume_command`. Empty result → `[]`.

### Directory matching

The target path is normalized with `filepath.Abs` + `filepath.Clean`; a
symlink-resolved variant (`filepath.EvalSymlinks`) is also computed so that
e.g. `/tmp` matches sessions recorded under `/private/tmp` on macOS. A session
matches when its cleaned `CWD` equals either variant or starts with
`variant + "/"` (prefix on path boundary — `/foo/bar` must not match
`/foo/barbaz`).

### Picker UX

```
┌─ Sessions in ~/lazyagent (12) ──────────────────────────────┐
│ ▸ claude  2h ago      84  fix build embed placeholder       │
│   codex   yesterday   31  webhook config models             │
│   grok    3d ago      12  docs limits   (no resume)         │
└─────────────────────────────────────────────────────────────┘
  ↑/↓ move · enter open · c copy resume cmd · q quit
```

- Row = agent, relative last-activity time, message count, title (custom
  session name when set, otherwise a truncated preview of the first user
  message).
- **enter** on an openable session (claude, codex, amp, pi, kimi — the set
  `search` already executes): quit the picker, then run the resume command in
  the current terminal with stdin/stdout/stderr attached and `cmd.Dir` set to
  the session's CWD when that directory still exists (required by e.g.
  `claude --resume`, which locates sessions by project dir).
- **enter** on a non-openable session: footer message. If a resume command
  string exists anyway (opencode, kilo, cursor) it is copied to the clipboard
  via the existing `core` clipboard helper; for grok: "no resume available".
- **c** copies the resume command to the clipboard for any row that has one.
- **↑/↓** and **j/k** move; **q/esc/ctrl+c** quit.
- No incremental filter/search inside the picker for now (YAGNI).

## Architecture

New package `internal/sessions`, following the existing subcommand pattern
(`internal/<pkg>.Run(args []string) int` registered in `main.go`'s switch):

```
internal/sessions/
  sessions.go   Run(): flag parsing (flag.NewFlagSet), discovery, dispatch
  filter.go     target-path normalization, CWD matching, sidechain
                exclusion, LastActivity-desc sorting — pure functions
  picker.go     bubbletea model; returns (chosen session, action) —
                open/copy/quit. Exec happens OUTSIDE the tea program,
                after terminal restore.
  json.go       JSON serialization of the filtered list
```

**Data flow:** `Run` → `core.LoadConfig()` → `core.BuildProvider(agent, cfg)`
→ `DiscoverSessions()` → filter/sort → picker or JSON → on open: exec the
resume command. Discovery has no recency cutoff, so the listing is the full
on-disk history.

**Targeted refactor (dedup, not gratuitous):** `search/run.go` currently
duplicates the agent→resume-command mapping of `core.ResumeCommand` in its own
switch. Add `core.ResumeArgv(agent, sessionID) []string` and make both
`search` and `sessions` build their `exec.Cmd` from it. Semantics:

- `ResumeArgv` returns argv **only for the executable set** (claude, codex,
  amp, pi, kimi) — nil otherwise. "Openable" ⇔ `ResumeArgv != nil`.
- `ResumeCommand` (display string) stays as-is, covering all agents that have
  one. "Copyable" ⇔ `ResumeCommand != ""`.

One source of truth for "how a session is reopened".

**Registration:** new `case "sessions":` in `main.go`'s subcommand switch plus
a line in the usage text.

**No impact** on GUI/tray/API surfaces.

## Error handling

- Nonexistent `--dir` or unknown `--agent` → message on stderr, exit 2.
- No sessions found → friendly message, exit 0 (`--json`: `[]`, exit 0).
- Agent binary missing at exec time → error message (as `search` does),
  exit 1.
- Quitting the picker without opening → exit 0.

## Testing

- `filter_test.go` — CWD matching (exact, subdirectory, false prefix,
  symlinked target), sidechain exclusion, sort order.
- `json_test.go` — output shape, empty list.
- `core/resume_test.go` — `ResumeArgv` coverage and argv ↔ display-string
  consistency with `ResumeCommand`.
- Picker stays thin: the selection→action mapping is a pure function with
  tests; rendering is not tested.

## Documentation

- New dedicated page `docs/usage/sessions.md`, linked from the docs index.
- Update the subcommand list in `README.md` and in `main.go`'s usage text.
