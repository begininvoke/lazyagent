---
title: "Session history for a directory"
description: "Print a table of every recorded session for the current directory, across all agents."
sidebar:
  order: 4
---

`lazyagent history` prints a table of every session whose working directory
is the current directory (or a subdirectory of it), across all supported
agents — oldest at the top, most recent at the bottom as row `#1`, right
above the prompt. In a terminal it then offers to resume one by row number. It is the flat sibling of [`lazyagent sessions`](sessions.md): same
discovery, directory filter, and resume behavior, but a plain table plus a
one-shot prompt instead of an interactive picker.

By default only the **20 most recent** sessions are shown; pass `--all` to
see every one.

## Synopsis

```
lazyagent history [--all] [--agent NAME] [--dir PATH]
```

## Output

```
  #   AGENT    SESSION                                   BRANCH            MSGS   LAST ACTIVITY
  3   grok     docs limits                               -                 12     2026-08-27 18:03
  2   codex    webhook config models                     feat/webhooks     31     2026-08-30 09:10
  1   claude   fix build embed placeholder               main              84     2026-08-31 14:52

3 session(s) in ~/projects/foo.

Resume a session? Enter row #, or press Enter to quit:
```

Each row shows the agent, a title (your custom session name when set,
otherwise the agent-provided name or a preview of the first user message),
the git branch recorded for the session (`-` when unknown), the message
count, and the local last-activity timestamp. Row `#1` is always the most
recent session — the numbers count down from the top so the newest entry
ends up next to the prompt, like shell history. When more sessions exist
than the 20 shown, the footer says so and points at `--all`. To skip the
table and jump straight into the newest session, use
[`lazyagent latest`](latest.md).

## Resuming

When stdin and stdout are terminals, the table is followed by a prompt:
entering a row number resumes that session with the originating agent's CLI
(e.g. `claude --resume <id>`), run from the session's own working directory
when it still exists. Pressing Enter on an empty line — or `ctrl+c` —
quits without resuming. Piped or redirected output skips the prompt
entirely, so `lazyagent history | cat` stays non-interactive.

Agents lazyagent can exec directly: Claude Code, Codex, Amp, pi, Grok, and Kimi.
Other agents print a "no resume command" notice instead. For scripted
output, `lazyagent sessions --json` emits the same list as JSON.

## Flags

| Flag | Type | Default | Summary |
|------|------|---------|---------|
| `--all` | bool | `false` | Show every session instead of the 20 most recent |
| `--agent NAME` | string | `all` | Restrict the listing to one agent |
| `--dir PATH` | string | current dir | Show history for another directory |

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Normal exit (including "no sessions found" and quitting the prompt) |
| `1` | Discovery or resume error — details printed to stderr |
| `2` | Bad invocation (unknown flag or `--agent` value, `--dir` not a directory) or an invalid row selection |
