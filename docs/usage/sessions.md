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

## Performance

### Progressive picker

When you run `lazyagent sessions`, the picker opens immediately and results stream in as each agent's discovery completes. A footer displays `loading agents… (done/total)` during discovery. Once all agents finish, the footer switches to the normal keybinding hint. If discovery finishes with zero sessions, the command prints "No sessions found in …" and exits.

### Discovery cache

The sessions command maintains persistent discovery caches under your system cache directory:

- **macOS**: `~/Library/Caches/lazyagent/`
- **Linux**: `~/.cache/lazyagent/`
- **Other**: per-platform defaults from `$XDG_CACHE_HOME` or equivalent

Cache files follow the pattern `discovery-<agent>.json` and `cwdindex-<agent>.json` (for example, `discovery-claude.json` and `cwdindex-claude.json` for Claude Code). Files are created with permission `0600`; the directory has `0700`.

Cache contents are **advisory**: deleting the cache directory is always safe and won't break anything. On the next run, `lazyagent sessions` simply re-scans the session data and rebuilds the cache.

Repeat runs typically complete in tens of milliseconds when caches are warm; only sessions from files that changed on disk are re-read.

**Privacy note**: cache files may contain short transcript snippets (for example, the first message text for session preview). If you delete your entire cache directory, you also remove these cached snippets from disk.

### Directory-scoped optimization

The listing is optimized to discover sessions for the target directory without reading other directories' data, which speeds up results when your codebase spans multiple large directory hierarchies.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success — including quitting the picker or an empty listing |
| `1` | Runtime failure (discovery, resume exec, clipboard) |
| `2` | Usage error (unknown `--agent`, `--dir` not a directory, no TTY without `--json`) |
