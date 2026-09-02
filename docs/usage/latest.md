---
title: "Resume the latest session"
description: "Resume the most recent session recorded for the current directory, across all agents."
sidebar:
  order: 5
---

`lazyagent latest` finds the most recently active session whose working
directory is the current directory (or a subdirectory of it), across all
supported agents, and resumes it immediately with the originating agent's
CLI — no table, no prompt. It opens exactly the session
[`lazyagent history`](history.md) shows as row `#1`.

## Synopsis

```
lazyagent latest [--agent NAME] [--dir PATH]
```

```
$ lazyagent latest
Opening: claude --resume 3f2a…
```

The resume command runs with this terminal attached, from the session's own
working directory when it still exists. Agents lazyagent can exec directly:
Claude Code, Codex, Amp, pi, Grok, and Kimi; for other agents a "no resume
command" notice is printed instead. "Most recent" means latest activity in
the transcript (lazyagent does not record a separate creation time).

## Flags

| Flag | Type | Default | Summary |
|------|------|---------|---------|
| `--agent NAME` | string | `all` | Consider only one agent's sessions |
| `--dir PATH` | string | current dir | Resume the latest session for another directory |

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Session resumed and exited normally |
| `1` | No session found for the directory, discovery error, or resume error |
| `2` | Bad invocation (unknown flag or `--agent` value, `--dir` not a directory) |
