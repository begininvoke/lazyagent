---
title: "macOS GUI"
description: "A detachable menu bar panel built with Wails v3 and Svelte 5."
sidebar:
  order: 2
---

```bash
lazyagent --gui
```

The GUI process detaches from your terminal — the shell returns immediately and the app lives in your menu bar. In its default attached mode there's no Dock icon (it's registered as a macOS *accessory* app). Click the tray icon to toggle the panel.

![lazyagent macOS menu bar app](../../assets/tray.png)

## Attached panel vs. detached desktop mode

The panel defaults to an attached popover below the menu bar icon. Press <kbd>d</kbd> (or click the detach button in the header) to pop it out.

Detaching turns lazyagent into a full desktop app: a Dock icon appears, it shows up in Cmd-Tab, and native macOS menus (App, Edit, Window) are installed. The compact popover is replaced by a card-grid dashboard, with a density switch — **compact**, **rich**, or **live** — that controls how much detail each session card shows; your choice is persisted across launches. Selecting a card pushes in a detail panel alongside the grid rather than covering it. Once detached you can also:

- **Move the window** anywhere on your screen.
- **Resize it** to whatever dimensions you want.
- **Pin it always-on-top** so it stays visible while you work.

Press <kbd>d</kbd> again or close the window to reattach — the Dock icon goes away and lazyagent returns to menu bar accessory mode.

## Keybindings

| Key | Action |
|-----|--------|
| <kbd>↑</kbd> / <kbd>k</kbd> | Move up |
| <kbd>↓</kbd> / <kbd>j</kbd> | Move down |
| <kbd>+</kbd> / <kbd>-</kbd> | Adjust time window |
| <kbd>f</kbd> | Cycle activity filter |
| <kbd>/</kbd> | Search sessions |
| <kbd>l</kbd> | Open or close the limits view |
| <kbd>r</kbd> | Rename session; refresh while the limits view is open |
| <kbd>d</kbd> | Detach / reattach panel |
| <kbd>esc</kbd> | Close detail / dismiss search |

## Right-click menu

Right-click the tray icon for a compact menu:

- **Show Panel** — open the session panel (same as left-click)
- **Refresh Now** — force reload all sessions
- **Quit** — exit the app

## Visuals

The GUI uses Catppuccin Mocha as its theme and renders sparklines as real SVG area charts (unlike the TUI's Unicode braille). Activity badges use the same color taxonomy across all interfaces.

## Startup and cache

The initial session list loads progressively: results appear as each agent
provider finishes instead of the panel waiting for the slowest provider.
Subsequent changes continue to arrive through the normal watcher and polling
paths.

The GUI reuses lazyagent's persistent discovery cache across process runs.
This makes later startups faster but also means session metadata and short
transcript snippets may be stored in the system cache directory. See
[Persistent discovery cache](../concepts/how-it-works.md#persistent-discovery-cache)
for location, permissions, and cleanup behavior.

## Combining with other interfaces

```bash
lazyagent --gui --api            # menu bar + HTTP API
lazyagent --tui --gui --api      # everything
```

The GUI always runs in its own OS process (Cocoa requires ownership of the main thread), so combined launches fork it transparently. Quitting via the tray menu kills the tray process only — any TUI or API in the same parent invocation keeps running.
