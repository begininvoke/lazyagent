---
title: "How it works"
description: "lazyagent is purely observational — it reads the session data each agent already writes and derives everything from there."
sidebar:
  order: 1
---

lazyagent doesn't wrap, inject, or modify any agent. It reads whatever each agent already writes to disk — JSONL transcripts, SQLite databases, thread JSON files — and reconstructs state from there. Your agents stay on their own paths; lazyagent is a listener.

## The pipeline

```
  Agent CLIs / IDEs      Session data on disk           lazyagent
  ─────────────────     ───────────────────────       ──────────────
  claude, codex, …  →   JSONL / JSON files       →    SessionProvider
  grok, pi, amp, …      session directories            ↓
  cursor, opencode, kilo → SQLite databases      →     Session model
                                                        ↓
                                                       TUI / GUI / API
```

Each agent has a **provider** that knows where its session data lives and how to parse it into a common `Session` struct. Providers can be file-watched (fsnotify) or polled (for SQLite-backed agents), and new sessions are merged into the shared view as they appear.

## The shared core

Everything useful — the activity state machine, the file watcher, session caching, cost estimation, configuration — is coordinated by the shared core. The three interfaces (TUI, GUI, API) and commands such as `sessions`, `prune`, `compact`, `search`, and `limits` reuse that shared behavior so their session semantics stay aligned.

Sessions are cached by transcript path + mtime + size, so subsequent scans only re-parse what changed. Grok uses the `chat_history.jsonl` inside each session directory as its cache key; Kimi uses `wire.jsonl`. For large JSONL transcripts the parser resumes from the last byte offset rather than re-reading the whole file when the agent format supports it.

### Persistent discovery cache

The TUI, macOS GUI, HTTP API, and `lazyagent-cli sessions` persist supported
providers' discovery caches between process runs. The files live under the
system cache directory (`~/Library/Caches/lazyagent/` on macOS and normally
`~/.cache/lazyagent/` on Linux), are advisory, and can be deleted safely; the
next discovery starts cold and rebuilds them.

Cache files are written with `0600` permissions inside a `0700` directory.
They can contain session metadata and short transcript snippets used for
previews. See [Sessions for a directory](../usage/sessions.md#discovery-cache)
for filenames, platform paths, and cleanup details.

### Initial loading

The TUI and macOS GUI discover providers progressively on first launch, so
sessions appear as each provider finishes instead of waiting for the slowest
one. The TUI shows a loading indicator until the initial stream completes.
The HTTP API deliberately completes its initial discovery before it starts
serving requests, so its first response is a complete snapshot rather than a
partially populated list. Warm persistent caches keep this synchronous API
startup fast in the common case.

## Activity inference

lazyagent classifies each session into a state (`idle`, `thinking`, `writing`, `running`, …) by looking at the last few entries in the transcript: which tool fired, whether its output has arrived, how long ago the last entry was. The full set of states is documented in [Activity states](activity-states.md).

## What lazyagent never does

- It doesn't talk to any LLM. The only outbound network calls lazyagent makes are explicit `lazyagent-cli limits` checks for Claude, Codex, Grok, Kimi, and Cursor billing/rate-limit data. Everything else (monitoring, sessions, prune, compact, and search) is purely local.
- It doesn't interrupt or control agents. You can't kill a session from lazyagent; it only watches.
- It doesn't move or copy session files — except when you explicitly run `prune` or `compact`, which operate on the same files the agents read.
- It doesn't send telemetry. No analytics, no crash reporter, no phone-home.

## What this implies

Because every piece of state comes from a file on disk:

- Closing lazyagent never disrupts a running agent session.
- Multiple lazyagent processes (e.g. GUI + TUI on the same machine) are always consistent — they read the same files.
- Moving a session folder (or deleting a project directory) is reflected on the next scan. This is also how the `--orphaned` filter in `prune` works.
