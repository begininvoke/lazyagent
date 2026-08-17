---
title: "Installation"
description: "How to install lazyagent via Homebrew, go install, or a source build."
sidebar:
  order: 1
---

lazyagent ships as two artifacts: a macOS desktop app (`Lazyagent.app`, includes a self-linked CLI) and a standalone CLI (`lazyagent-cli`, TUI + HTTP API, no GUI). The `lazyagent` command itself has been retired — see below.

## Homebrew

The recommended way on macOS and Linux.

**Desktop app** (macOS, universal binary — TUI + GUI + HTTP API):

```bash
brew install --cask illegalstudio/tap/lazyagent
```

Installs `Lazyagent.app`. On each GUI launch, the app self-links `~/bin/lazyagent-cli` to its inner binary (creating `~/bin` if it doesn't exist, and never overwriting a file that isn't already a symlink) — add `~/bin` to your `PATH` to use it from the terminal.

**CLI only** (macOS, Linux — TUI + HTTP API, no GUI):

```bash
brew install illegalstudio/tap/lazyagent-cli
```

## Go (TUI only)

If you only need the terminal interface and already have a Go toolchain:

```bash
go install github.com/illegalstudio/lazyagent@latest
```

`go install` names the binary after the module path, so this still produces a binary literally called `lazyagent` (not `lazyagent-cli`) — rename or alias it if you want it alongside a Homebrew install. It doesn't include the Wails-powered macOS menu bar app — the GUI requires a Node.js build step, which `go install` doesn't perform. Use Homebrew or a source build if you want the tray.

## Build from source

```bash
git clone https://github.com/illegalstudio/lazyagent
cd lazyagent

# TUI only — no Wails, no Node.js required
make tui

# Full build with menu bar app (requires Node.js 18+)
make install   # npm install, only the first time
make build
```

The resulting binary is written to the repository root as `lazyagent` — same naming convention as `go install`, unrelated to the retired `lazyagent` command name.

## Verify

```bash
lazyagent-cli --version   # Homebrew (cask self-link or lazyagent-cli formula)
lazyagent --version       # go install or a source build
```

lazyagent reads its configuration from `~/.config/lazyagent/config.json`, creating it with defaults on first run. See [Configuration](../reference/configuration.md) for the field-by-field reference.
