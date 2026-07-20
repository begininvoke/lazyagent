---
title: "Sessions for a directory"
description: "List every recorded session for the current directory — across all agents — and reopen one."
sidebar:
  order: 3
---

`lazyagent sessions` lists every session whose working directory is the
current directory (or a subdirectory of it), across all supported agents,
newest first. Selecting a session resumes it with the originating agent's
own CLI.

## Synopsis

```
lazyagent sessions [--agent NAME] [--json] [--dir PATH]
```

## The picker

```
┌─ Sessions in ~/projects/foo (12) ───────────────────────────┐
│ ▸ claude  2h ago      84  fix build embed placeholder       │
│   codex   yesterday   31  webhook config models             │
│   grok    3d ago      12  docs limits   (no resume)         │
└─────────────────────────────────────────────────────────────┘
  ↑/↓ move · enter open · c copy resume cmd · q quit
```

Each row shows the agent, relative last-activity time, message count, and a
title (your custom session name when set, otherwise a preview of the first
user message).

| Key | Action |
|-----|--------|
| `↑`/`k`, `↓`/`j` | Move the cursor |
| `enter` | Reopen the session in this terminal |
| `c` | Copy the resume command to the clipboard |
| `q` / `esc` / `ctrl+c` | Quit without opening |

**Opening** runs the agent's resume command (e.g. `claude --resume <id>`)
with this terminal attached, from the session's own working directory when
it still exists. Agents lazyagent can exec directly: Claude Code, Codex,
Amp, pi, and Kimi. For OpenCode, Kilo, and Cursor the resume command is
copied to the clipboard instead; Grok has no resume command.

## Flags

| Flag | Type | Default | Summary |
|------|------|---------|---------|
| `--agent NAME` | string | `all` | Restrict the listing to one agent |
| `--json` | bool | `false` | Print the list as JSON on stdout and exit (no picker) |
| `--dir PATH` | string | current dir | List sessions for another directory |

## JSON output

`--json` emits an array (possibly `[]`), one object per session:

```json
[
  {
    "agent": "claude",
    "session_id": "abc123",
    "name": "fix-build",
    "cwd": "/Users/me/projects/foo",
    "last_activity": "2026-07-20T09:12:33Z",
    "messages": 84,
    "resume_command": "claude --resume abc123"
  }
]
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success — including quitting the picker or an empty listing |
| `1` | Runtime failure (discovery, resume exec, clipboard) |
| `2` | Usage error (unknown `--agent`, `--dir` not a directory, no TTY without `--json`) |
