# lazyagent

![GitHub Downloads](https://img.shields.io/github/downloads/illegalstudio/lazyagent/total?v=1)
![License: MIT](https://img.shields.io/badge/license-MIT-blue)
[![Product Hunt](https://img.shields.io/badge/Product%20Hunt-Launch-ff6154?logo=producthunt&logoColor=white)](https://www.producthunt.com/products/lazy-agent)
[![Download on the App Store](https://img.shields.io/badge/App%20Store-Download-0D96F6?logo=apple&logoColor=white)](https://apps.apple.com/us/app/lazyagent/id6773359156)
[![Follow @nahime0 on X](https://img.shields.io/badge/Follow%20%40nahime0-000000?logo=x&logoColor=white)](https://x.com/nahime0)

> 🐦 **[Follow me on X (@nahime0)](https://x.com/nahime0) for updates, new features, and behind-the-scenes development.**

---

**A terminal UI, macOS menu bar app, and HTTP API for monitoring all your coding agents from a single place.**

Watch sessions from [Claude Code](https://claude.ai/code), [Cursor](https://cursor.com/), [Codex](https://developers.openai.com/codex/), [Grok CLI](https://x.ai/cli), [Kilo](https://kilo.ai/), Kimi Code CLI, [Amp](https://ampcode.com/), [pi](https://github.com/badlogic/pi-mono), and [OpenCode](https://opencode.ai/) — no lock-in, no server, purely observational.

Inspired by [lazygit](https://github.com/jesseduffield/lazygit), [lazyworktree](https://github.com/chmouel/lazyworktree), and [pixel-agents](https://github.com/pablodelucca/pixel-agents).

## Support the project

⭐ If lazyagent is useful to you, consider [starring the repo](https://github.com/illegalstudio/lazyagent) — it helps others discover it!

💛 Loving it? Consider [becoming a sponsor](https://github.com/sponsors/nahime0) to keep the project alive and growing.

## lazyagent for iOS

Want to keep an eye on your agents from your pocket? **[lazyagent is available on the App Store](https://apps.apple.com/us/app/lazyagent/id6773359156)** for iPhone and iPad.

The iOS app is a **paid** app — and that's on purpose. Buying it is one of the easiest ways to support the project and keep development going. Thank you! 💛

That said, lazyagent and its API are **fully open source**. If you'd rather not pay for the app, you're more than welcome to build your own client on top of the API — that's exactly what it's there for. No hard feelings, the choice is yours. 🙂

## News

📢 **Session tools are here!** Commands to find, reopen, search, and maintain your agent sessions — plus keep rate-limit usage visible:

- **[`lazyagent-cli prune`](docs/maintenance/prune.md)** — delete chat files older than N days or whose project folder no longer exists. Interactive agent picker, dry-run previews, and per-project row selection at the confirmation prompt.
- **[`lazyagent-cli compact`](docs/maintenance/compact.md)** — shrink session files in place by truncating bulky tool outputs, thinking blocks, and embedded images — sessions stay resumable with the originating agent. Supports Claude Code, pi, Codex, Grok, and Kimi.
- **[`lazyagent-cli search`](docs/maintenance/search.md)** — search transcript-file agents (Claude, Codex, pi, Amp, Grok, Kimi) with highlighted snippets and an incremental local index.
- **[`lazyagent-cli limits`](docs/maintenance/limits.md)** — on-demand rate-limit / billing summary for Claude Code (5h + 7d), Codex (5h + 7d), Grok (monthly), Kimi Code, and Cursor (monthly, Models + API pools), with a detailed pace view available via `--detailed`.
- **[`lazyagent-cli sessions`](docs/usage/sessions.md)** — list every session recorded for the current directory — across all agents — and reopen one with the originating agent's CLI. Interactive picker, `--json` for scripts.
- **Outbound webhooks on session state transitions** — send a signed JSON payload to Slack, a custom dashboard, or a CI endpoint whenever a session goes idle, waits for input, or changes state. See [Webhooks](docs/reference/webhooks.md).

Typical savings on a year of daily use: **80+ MiB reclaimed** across the cleanup commands, with every rewrite validated and backed up by default.

## Why lazyagent?

Unlike other tools, lazyagent doesn't replace your workflow — it watches it. Launch agents wherever you want (terminal, IDE, desktop app), lazyagent just observes. No lock-in, no server, no account required.

### Terminal UI
![lazyagent TUI](assets/tui.png)

### macOS Menu Bar App
![lazyagent macOS tray](assets/tray.png)

Detach the panel and lazyagent becomes a full desktop app — Dock icon, Cmd-Tab, native menus — with a card-grid dashboard and a `compact | rich | live` density switch. Attach again to return it to the menu bar.

### HTTP API
![lazyagent API playground](assets/api.png)

## Install

> **Breaking change:** the `lazyagent` command has been retired. The CLI is now `lazyagent-cli`, and the macOS desktop app is `Lazyagent.app`.

### Homebrew

**Desktop app** (macOS, universal binary — TUI + GUI + HTTP API):

```bash
brew install --cask illegalstudio/tap/lazyagent
```

Installs `Lazyagent.app`. On each GUI launch, the app self-links `~/bin/lazyagent-cli` to its inner binary (creating `~/bin` if it doesn't exist, and never overwriting a file that isn't already a symlink) — add `~/bin` to your `PATH` to use it from the terminal.

**CLI only** (macOS, Linux — TUI + HTTP API, no GUI):

```bash
brew install illegalstudio/tap/lazyagent-cli
```

### Go (TUI only)

```bash
go install github.com/illegalstudio/lazyagent@latest
```

### Build from source

```bash
git clone https://github.com/illegalstudio/lazyagent
cd lazyagent

# TUI only (no Wails/Node.js needed)
make tui

# Full build with menu bar app (requires Node.js for frontend)
make install   # npm install (first time only)
make build
```

## Launch

```
lazyagent-cli                    Launch the terminal UI (monitors all agents)
lazyagent-cli --agent claude     Monitor only Claude Code sessions
lazyagent-cli --agent grok       Monitor only Grok CLI sessions
lazyagent-cli --agent kimi       Monitor only Kimi Code CLI sessions
lazyagent-cli --api              Start the HTTP API (Bearer-token protected)
lazyagent-cli --gui              Launch the desktop app (menu bar)
lazyagent-cli --tui --gui --api  Run everything together
lazyagent-cli prune --days N     Delete chat sessions older than N days
lazyagent-cli compact            Shrink chat files by truncating bulky payloads
lazyagent-cli search "query"     Search chat transcripts with snippets
lazyagent-cli sessions           List and reopen sessions for the current directory
lazyagent-cli limits             Show 5h / weekly / monthly usage summary
lazyagent-cli passphrase         Set or rotate the HTTP API passphrase
lazyagent-cli --help             Show full help
```

## Documentation

Full documentation — supported agents, activity states, keybindings, configuration, the HTTP API, maintenance commands, and architecture — lives at:

- **[lazyagent.dev/docs](https://lazyagent.dev/docs)** — rendered website
- [`docs/`](docs/) — Markdown sources in this repository, organized by topic:
  - [Getting started](docs/getting-started/) — install, quickstart
  - [Concepts](docs/concepts/) — how it works, supported agents, activity states, session info
  - [Interfaces](docs/interfaces/) — terminal UI, macOS GUI, HTTP API
  - [Usage](docs/usage/) — CLI reference, directory-scoped sessions, recipes
  - [Maintenance](docs/maintenance/) — `prune`, `compact`, `search`, and `limits` commands
  - [Reference](docs/reference/) — configuration, architecture, development, roadmap

## License

MIT
