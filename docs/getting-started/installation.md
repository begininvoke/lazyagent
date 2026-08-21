---
title: "Installation"
description: "How to install lazyagent via Homebrew, go install, or a source build."
sidebar:
  order: 1
---

lazyagent ships as two artifacts: the macOS desktop app (`Lazyagent.app`, which bundles the CLI) and a standalone CLI build (TUI + HTTP API, no GUI). Both provide the same `lazyagent` command, so install one or the other — Homebrew refuses to install both at once.

## Homebrew

The recommended way on macOS and Linux.

**Desktop app** (macOS, universal binary — TUI + GUI + HTTP API):

```bash
brew install --cask illegalstudio/tap/lazyagent
```

Installs `Lazyagent.app` and links the `lazyagent` command into Homebrew's bin. Installing the app zip manually instead (from GitHub releases)? Link the CLI yourself:

```bash
ln -s /Applications/Lazyagent.app/Contents/MacOS/lazyagent /usr/local/bin/lazyagent
```

**CLI only** (macOS, Linux — TUI + HTTP API, no GUI):

```bash
brew install illegalstudio/tap/lazyagent-cli
```

## Go (TUI only)

If you only need the terminal interface and already have a Go toolchain:

```bash
go install github.com/illegalstudio/lazyagent@latest
```

`go install` names the binary after the module path, so this produces the same `lazyagent` command as the Homebrew installs — keep only one of them on your `PATH`, or alias the go-installed copy. It doesn't include the Wails-powered macOS desktop app — the GUI requires a Node.js build step, which `go install` doesn't perform. Use Homebrew or a source build if you want the GUI.

## Build from source

```bash
git clone https://github.com/illegalstudio/lazyagent
cd lazyagent

# TUI only — no Wails, no Node.js required
make tui

# Full build with macOS app (requires Node.js 18+)
make install   # npm install, only the first time
make build
```

The resulting binary is written to the repository root as `lazyagent` — same naming convention as `go install`, unrelated to the retired `lazyagent` command name.

## Verify

```bash
lazyagent --version   # whichever install owns the command: cask link, formula, or go install
```

lazyagent reads its configuration from `~/.config/lazyagent/config.json`, creating it with defaults on first run. See [Configuration](../reference/configuration.md) for the field-by-field reference.
