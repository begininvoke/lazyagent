# Lazyagent.app / lazyagent-cli split — Design

**Date:** 2026-08-17
**Branch:** `feat/desktop-mode` (continues on the desktop-mode branch)

## Goal

Ship two artifacts from the one repo/module:

1. **`Lazyagent.app`** (macOS) — a real app bundle wrapping the full binary
   (GUI + TUI + API). Gives the GUI a LaunchServices identity: correct
   Cmd-Tab icon and name, working "Keep in Dock", login-item friendliness.
   Root cause being fixed: a bare Mach-O has `bundleID=NULL`, so the
   Cmd-Tab switcher falls back to the generic Unix-executable icon
   (`applicationIconImage` only paints the Dock tile).
2. **`lazyagent-cli`** — the existing `notray` build (no Wails, no cgo),
   now for **darwin and linux**, binary and command named `lazyagent-cli`.

No repo or module split: the code seam already exists (`notray` build
tag). This is an artifact/packaging split.

**Breaking change:** the `lazyagent` command is retired. Desktop users get
`lazyagent-cli` self-linked by the app (below); CLI users install the
`lazyagent-cli` formula. Called out in release notes and README.

## Part 1 — Bundle behavior (Go)

### Bundle detection

`os.Executable()`, symlinks resolved, contains `.app/Contents/MacOS/` →
the process runs from a bundle. Helper in `internal/core` (pure function
on a path string, unit-testable):

```go
// InBundlePath reports whether exePath points inside a macOS .app bundle.
func InBundlePath(exePath string) bool
```

### Launch semantics

- **Bundle binary, no args** (Finder/LaunchServices launch): GUI mode —
  same code path as `--gui` today, run directly (no fork; the process is
  already the bundle identity). `LSUIElement=true` in Info.plist keeps it
  a menu bar accessory at launch; the existing runtime activation-policy
  toggle keeps working on detach (runtime `setActivationPolicy` overrides
  the plist).
- **Full binary, `--gui`, inside a bundle** (e.g. via the CLI symlink):
  delegate to `open -b com.illegalstudio.lazyagent` so the GUI process
  gets the bundle identity; the current self-fork (`forkTray`) stays as
  the fallback for bare dev builds (`make dev` unchanged).
- **`lazyagent-cli` (notray), `--gui`**: existing "not available in this
  build" error, unchanged.
- All other flags/subcommands unchanged in both artifacts.

### CLI self-link

On every GUI startup, idempotently: ensure the symlink
`~/bin/lazyagent-cli` → `<bundle>/Contents/MacOS/lazyagent`.

- Create `~/bin` if missing.
- Refresh the link if it is a symlink pointing elsewhere (stale version,
  moved app) or broken.
- **Never touch** a `~/bin/lazyagent-cli` that is not a symlink (user's
  own file/script wins); skip silently.
- Homebrew's `lazyagent-cli` lives in the brew prefix; PATH order decides
  which one wins — by design.
- If `~/bin` is not on the user's PATH the link is inert; documented, not
  detected.

Logic lives in a small `internal/tray` (or `internal/core`) helper,
unit-tested against a temp dir (create, refresh-stale, no-clobber cases).

## Part 2 — App icon

`assets/icon.svg` is true vector artwork (Lucide-style scan-eye glyph,
`currentColor`). Build-time composition, no new hand-made assets:

- New `assets/appicon.svg`: dark rounded-rect tile (`#1e1e2e`, macOS
  squircle-ish corner radius) with the glyph in accent mauve (`#cba6f7`),
  sized with standard macOS icon margins.
- Build step rasterizes it at the icns sizes (16→1024) and runs
  `iconutil` → `Lazyagent.icns`. Rasterizer: `rsvg-convert` (brew) on CI;
  `qlmanage` fallback acceptable locally.

## Part 3 — Release pipeline

### goreleaser

- **Builds:**
  - `darwin` (existing, full, CGO): binary `lazyagent`, arm64 + amd64,
    combined via `universal_binaries` → one universal binary for the app.
  - `darwin-cli` (new): tags `notray`, `CGO_ENABLED=0`, binary
    `lazyagent-cli`, arm64 + amd64.
  - `linux` (existing): binary renamed `lazyagent-cli`.
- **Bundle assembly:** `scripts/make-app.sh` builds
  `Lazyagent.app/Contents/{Info.plist,MacOS/lazyagent,Resources/Lazyagent.icns}`
  from the universal binary, signs the **bundle** (existing identity +
  entitlements), zips it. Also runnable locally: `make app` (unsigned) to
  test without a release.
- **Info.plist keys:** `CFBundleIdentifier com.illegalstudio.lazyagent`,
  `CFBundleName Lazyagent`, `CFBundleExecutable lazyagent`,
  `CFBundleIconFile Lazyagent`, `LSUIElement true`,
  `LSMinimumSystemVersion 11.0` (the current link floor),
  `CFBundleShortVersionString`/`CFBundleVersion` from the release version.
- **Notarization:** CI already runs `notarytool submit` on the zips; it
  now submits the app zip and runs `xcrun stapler staple` on the .app
  before final zipping.
- **Homebrew tap:**
  - Cask `lazyagent`: installs `Lazyagent.app` (`app` stanza, no
    `binary` stanza).
  - New formula `lazyagent-cli` (goreleaser `brews:`), darwin + linux.

## Testing

- Unit (Go): `InBundlePath` cases (bundle path, bare path, symlinked
  path); self-link create/refresh/no-clobber in a temp dir.
- Local: `make app` assembles an unsigned bundle; launching it must show
  the icns in Cmd-Tab when detached and self-link the CLI.
- Manual checklist: Cmd-Tab icon + name, Dock icon, "Keep in Dock"
  relaunches the app, login item works, `~/bin/lazyagent-cli --tui` runs,
  brew-vs-app PATH precedence, `make dev` (bare build) still works via
  the fork fallback.

## Out of scope

- Windows/Linux desktop packaging.
- Auto-update/Sparkle.
- Renaming the repo or Go module.
- Migrating existing users' config (none needed — config paths unchanged).
