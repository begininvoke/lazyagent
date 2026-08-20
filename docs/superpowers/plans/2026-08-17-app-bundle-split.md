# Lazyagent.app / lazyagent-cli Split Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship two artifacts from one repo: `Lazyagent.app` (real macOS bundle wrapping the full binary — fixes Cmd-Tab/LaunchServices identity) and `lazyagent-cli` (the `notray` build for darwin+linux), with the app self-linking its CLI into `~/bin`.

**Architecture:** No code split — the `notray` build tag is the seam. Go gains a bundle-path detector, bundle-aware launch semantics in main.go, and an idempotent CLI self-link. Packaging gains an icon composition, a bundle assembly script, goreleaser build/archive changes (universal binary, new `darwin-cli` build, renamed linux binary), a cask that installs the .app, a `lazyagent-cli` formula, and staple-aware notarization in CI.

**Tech Stack:** Go 1.25, goreleaser v2, macOS codesign/notarytool/stapler/iconutil, qlmanage or rsvg-convert for SVG rasterization, Homebrew tap (cask + formula).

**Spec:** `docs/superpowers/specs/2026-08-17-app-bundle-split-design.md`

## Global Constraints

- Branch: `feat/desktop-mode` (continue on it). Commit after every task; concise messages, no Co-Authored-By (user preference).
- Bundle identifier exactly `com.illegalstudio.lazyagent`; app name `Lazyagent`; inner binary name `lazyagent`; CLI command/binary name exactly `lazyagent-cli`.
- Info.plist: `LSUIElement true`, `LSMinimumSystemVersion 11.0`, `CFBundleIconFile Lazyagent`.
- Self-link: `~/bin/lazyagent-cli` → bundle binary; create `~/bin` if missing; refresh stale/broken symlinks; NEVER touch a non-symlink at that path (skip silently). Only when running from a bundle.
- The `lazyagent` command is retired — release notes/README must say so.
- No new Go dependencies. `go build -tags notray ./...` stays green. `make dev` (bare build, fork path) keeps working.
- After any change to goreleaser config: `goreleaser check` must pass. Field names in this plan are best-effort against goreleaser v2 — if `goreleaser check` rejects one, consult `goreleaser jsonschema`/docs and adjust to the current schema rather than guessing.

---

### Task 1: `core.InBundlePath` + `core.EnsureCLISymlink`

**Files:**
- Create: `internal/core/bundle.go`
- Test: `internal/core/bundle_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func InBundlePath(exePath string) bool` and `func EnsureCLISymlink(binDir, target string) error` in package `core`. Task 2 calls `InBundlePath`; Task 3 calls both.

- [ ] **Step 1: Write the failing tests**

```go
package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInBundlePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/Applications/Lazyagent.app/Contents/MacOS/lazyagent", true},
		{"/Users/x/Library/Lazyagent.app/Contents/MacOS/lazyagent", true},
		{"/opt/homebrew/bin/lazyagent-cli", false},
		{"/Users/x/dev/lazyagent/lazyagent", false},
		{"", false},
		// ".app" must be the bundle directory, not a substring elsewhere.
		{"/tmp/not.app.txt/Contents/MacOS/x", false},
	}
	for _, c := range cases {
		if got := InBundlePath(c.path); got != c.want {
			t.Errorf("InBundlePath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestEnsureCLISymlink_Creates(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	target := filepath.Join(dir, "lazyagent")
	if err := os.WriteFile(target, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCLISymlink(binDir, target); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(filepath.Join(binDir, "lazyagent-cli"))
	if err != nil {
		t.Fatal(err)
	}
	if got != target {
		t.Errorf("link points to %q, want %q", got, target)
	}
}

func TestEnsureCLISymlink_RefreshesStale(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(binDir, "lazyagent-cli")
	if err := os.Symlink(filepath.Join(dir, "old-target"), link); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "lazyagent")
	if err := os.WriteFile(target, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCLISymlink(binDir, target); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(link); got != target {
		t.Errorf("stale link not refreshed: points to %q", got)
	}
}

func TestEnsureCLISymlink_NoClobber(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(binDir, "lazyagent-cli")
	if err := os.WriteFile(link, []byte("user script"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCLISymlink(binDir, filepath.Join(dir, "lazyagent")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "user script" {
		t.Errorf("user file was clobbered")
	}
}

func TestEnsureCLISymlink_IdempotentWhenCorrect(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	target := filepath.Join(dir, "lazyagent")
	if err := os.WriteFile(target, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCLISymlink(binDir, target); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCLISymlink(binDir, target); err != nil {
		t.Errorf("second call errored: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/core/ -run 'TestInBundlePath|TestEnsureCLISymlink' -v`
Expected: FAIL (compile error: undefined `InBundlePath`, `EnsureCLISymlink`)

- [ ] **Step 3: Write minimal implementation**

```go
package core

import (
	"os"
	"path/filepath"
	"strings"
)

// InBundlePath reports whether exePath points inside a macOS .app bundle
// (…/<Name>.app/Contents/MacOS/…). The marker includes the slash after
// ".app", so names like "not.app.txt" cannot match. Callers should pass a
// symlink-resolved path: os.Executable() may return the invoking symlink.
func InBundlePath(exePath string) bool {
	return strings.Contains(exePath, ".app/Contents/MacOS/")
}

// EnsureCLISymlink idempotently maintains binDir/lazyagent-cli as a
// symlink to target. It creates binDir when missing and refreshes a
// symlink that is stale or broken, but never replaces a non-symlink:
// a user's own file at that path wins.
func EnsureCLISymlink(binDir, target string) error {
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	link := filepath.Join(binDir, "lazyagent-cli")
	if fi, err := os.Lstat(link); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			return nil // not ours — never clobber
		}
		if dst, err := os.Readlink(link); err == nil && dst == target {
			return nil // already correct
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	}
	return os.Symlink(target, link)
}
```

(Only the `os`, `path/filepath`, and `strings` imports are needed; `os` and `path/filepath` serve `EnsureCLISymlink`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/core/ -run 'TestInBundlePath|TestEnsureCLISymlink' -v`
Expected: PASS (all six InBundlePath cases and four symlink tests)

- [ ] **Step 5: Commit**

```bash
git add internal/core/bundle.go internal/core/bundle_test.go
git commit -m "feat(core): bundle-path detection and CLI self-link helpers"
```

---

### Task 2: Bundle-aware launch semantics in main.go

**Files:**
- Modify: `main.go` (the `runGUI` block, ~lines 145–190; usage text)

**Interfaces:**
- Consumes: `core.InBundlePath(string) bool` (Task 1); existing `forkTray`, `tray.Run`, `tray.Available`, `assets.HasFrontend`, `trayPidFile`.
- Produces: launch behavior relied on by Tasks 4–6: bundle launch with no mode flags runs the GUI directly; `--gui` from inside a bundle delegates to `open -b com.illegalstudio.lazyagent`.

- [ ] **Step 1: Add bundle detection before the mode computation**

In `main.go`, right after the `runAPI := *apiMode` / `--tray` deprecation block and BEFORE `runGUI := …`, add:

```go
	// A LaunchServices launch (double-click, login item) executes the
	// bundle binary with no mode flags: treat it as a GUI launch that
	// runs in-process — the process already carries the bundle identity,
	// so forking would throw it away.
	exePath, _ := os.Executable()
	if rp, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = rp
	}
	inBundle := core.InBundlePath(exePath)
	runDirectGUI := inBundle && tray.Available() &&
		!*guiMode && !*trayMode && !*tuiMode && !*apiMode
```

and change the mode lines to:

```go
	runGUI := *guiMode || *trayMode || runDirectGUI
	// Default: TUI if no other mode explicitly requested.
	runTUI := *tuiMode || (!runGUI && !runAPI)
```

(`path/filepath` needs importing if not already imported in main.go — check.)

- [ ] **Step 2: Rework the fork/exec decision inside the `runGUI` block**

Replace the current `if os.Getenv("LAZYAGENT_DETACHED") == "" { forkTray… } else { direct tray.Run… }` with:

```go
		if os.Getenv("LAZYAGENT_DETACHED") == "" && !runDirectGUI {
			if inBundle {
				// Relaunch through LaunchServices so the GUI process
				// keeps the bundle identity (Cmd-Tab icon and name).
				if err := exec.Command("open", "-b", "com.illegalstudio.lazyagent").Run(); err != nil {
					// Dev copies moved outside a registered bundle can
					// fail `open`; the bare fork still works, minus the
					// LaunchServices identity.
					forkTray(*demoMode, *agentMode)
				}
			} else {
				// Bare binary (make dev): fork with its own main thread.
				forkTray(*demoMode, *agentMode)
			}
			if !runTUI && !runAPI {
				return
			}
		} else {
			// Detached tray process, or a direct LaunchServices launch.
			_ = os.WriteFile(trayPidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
			defer os.Remove(trayPidFile)

			if err := tray.Run(*demoMode, *agentMode); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
```

(`os/exec` needs importing if not already imported — check; `killPreviousTray` stays inside `forkTray`, and the direct-launch path intentionally skips it: LaunchServices already enforces single-instance for bundle launches.)

- [ ] **Step 3: Update the usage text**

In `flag.Usage`, replace the `lazyagent` command name in the Usage lines with `lazyagent-cli` (the command users will actually have on PATH), and change the `--gui` line's description to `Launch the desktop app (menu bar)`. Subcommand lines change the same way (e.g. `lazyagent-cli prune --days N`).

- [ ] **Step 4: Verify builds**

Run: `go build ./... && go vet ./... && go build -tags notray ./...`
Expected: all pass. In the notray build `tray.Available()` is false, so `runDirectGUI` is always false there — no behavior change for `lazyagent-cli`.

- [ ] **Step 5: Commit**

```bash
git add main.go
git commit -m "feat: bundle-aware launch semantics (direct GUI, open -b delegation)"
```

---

### Task 3: Self-link on GUI startup

**Files:**
- Modify: `internal/tray/app.go` (inside `Run`, after the `assets.HasFrontend` check)

**Interfaces:**
- Consumes: `core.InBundlePath`, `core.EnsureCLISymlink` (Task 1).
- Produces: `~/bin/lazyagent-cli` maintained on every GUI start when running from a bundle.

- [ ] **Step 1: Wire the self-link**

In `tray.Run`, after the frontend-assets check and before `application.New`, add:

```go
	// Bundle installs self-serve the CLI: keep ~/bin/lazyagent-cli
	// pointing at this binary. Bare dev builds skip it. Errors are
	// non-fatal — a read-only home must not stop the GUI.
	go func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		if rp, err := filepath.EvalSymlinks(exe); err == nil {
			exe = rp
		}
		if !core.InBundlePath(exe) {
			return
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		_ = core.EnsureCLISymlink(filepath.Join(home, "bin"), exe)
	}()
```

(`path/filepath` needs importing in app.go; `os` and `core` are already imported.)

- [ ] **Step 2: Verify builds**

Run: `go build ./... && go vet ./internal/tray/`
Expected: pass.

- [ ] **Step 3: Commit**

```bash
git add internal/tray/app.go
git commit -m "feat(gui): self-link ~/bin/lazyagent-cli when running from a bundle"
```

---

### Task 4: App icon + bundle assembly script + `make app`

**Files:**
- Create: `assets/appicon.svg`
- Create: `scripts/make-app.sh` (executable)
- Modify: `Makefile` (add `app` target, extend `.PHONY` and `clean`)

**Interfaces:**
- Consumes: the full binary (any path), `scripts/codesign.sh` (existing; no-ops without `APPLE_SIGNING_IDENTITY`), `scripts/entitlements.plist`.
- Produces: `scripts/make-app.sh BINARY VERSION OUTDIR` → assembles `OUTDIR/Lazyagent.app` (signed when the identity env is set) — invoked by goreleaser in Task 5 and by `make app` locally.

- [ ] **Step 1: Create `assets/appicon.svg`**

The scan-eye glyph from `assets/icon.svg` on a dark rounded tile, Catppuccin accent stroke. 1024 canvas, 824 tile with macOS-style margins, glyph scaled 24→560:

```svg
<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 1024 1024">
  <rect x="100" y="100" width="824" height="824" rx="185" fill="#1e1e2e"/>
  <g transform="translate(232,232) scale(23.333)"
     fill="none" stroke="#cba6f7" stroke-width="2"
     stroke-linecap="round" stroke-linejoin="round">
    <path d="M3 7V5a2 2 0 0 1 2-2h2"/>
    <path d="M17 3h2a2 2 0 0 1 2 2v2"/>
    <path d="M21 17v2a2 2 0 0 1-2 2h-2"/>
    <path d="M7 21H5a2 2 0 0 1-2-2v-2"/>
    <circle cx="12" cy="12" r="1"/>
    <path d="M18.944 12.33a1 1 0 0 0 0-.66 7.5 7.5 0 0 0-13.888 0 1 1 0 0 0 0 .66 7.5 7.5 0 0 0 13.888 0"/>
  </g>
</svg>
```

- [ ] **Step 2: Create `scripts/make-app.sh`**

```bash
#!/usr/bin/env bash
# Assemble Lazyagent.app from a built binary.
#
# Usage: scripts/make-app.sh BINARY VERSION OUTDIR
#   BINARY  — path to the full lazyagent binary (universal or single-arch)
#   VERSION — version string for the Info.plist (e.g. 0.4.0 or "dev")
#   OUTDIR  — directory to create Lazyagent.app in
#
# Signs the bundle via scripts/codesign.sh when APPLE_SIGNING_IDENTITY is
# set; otherwise leaves it unsigned (local/dev use).

set -euo pipefail

BINARY="$1"
VERSION="$2"
OUTDIR="$3"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

APP="$OUTDIR/Lazyagent.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

# --- Icon: rasterize assets/appicon.svg into an .icns -------------------
ICONSET="$(mktemp -d)/Lazyagent.iconset"
mkdir -p "$ICONSET"
SVG="$REPO_ROOT/assets/appicon.svg"

render() { # render SIZE OUTPNG
  local size="$1" out="$2"
  if command -v rsvg-convert >/dev/null 2>&1; then
    rsvg-convert -w "$size" -h "$size" "$SVG" -o "$out"
  else
    # qlmanage names its output <basename>.svg.png in the target dir.
    local tmp
    tmp="$(mktemp -d)"
    qlmanage -t -s "$size" -o "$tmp" "$SVG" >/dev/null
    mv "$tmp"/*.png "$out"
    rm -rf "$tmp"
  fi
}

BASE_PNG="$(mktemp -d)/appicon_1024.png"
render 1024 "$BASE_PNG"
for size in 16 32 64 128 256 512; do
  sips -z "$size" "$size" "$BASE_PNG" --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
  double=$((size * 2))
  sips -z "$double" "$double" "$BASE_PNG" --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/Lazyagent.icns"

# --- Info.plist ---------------------------------------------------------
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key><string>com.illegalstudio.lazyagent</string>
	<key>CFBundleName</key><string>Lazyagent</string>
	<key>CFBundleDisplayName</key><string>Lazyagent</string>
	<key>CFBundleExecutable</key><string>lazyagent</string>
	<key>CFBundleIconFile</key><string>Lazyagent</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>${VERSION}</string>
	<key>CFBundleVersion</key><string>${VERSION}</string>
	<key>LSMinimumSystemVersion</key><string>11.0</string>
	<key>LSUIElement</key><true/>
	<key>NSHighResolutionCapable</key><true/>
	<key>NSPrincipalClass</key><string>NSApplication</string>
</dict>
</plist>
PLIST

# --- Binary + signature -------------------------------------------------
cp "$BINARY" "$APP/Contents/MacOS/lazyagent"
chmod 0755 "$APP/Contents/MacOS/lazyagent"
plutil -lint "$APP/Contents/Info.plist"
"$SCRIPT_DIR/codesign.sh" "$APP"
echo "Assembled $APP"
```

Then: `chmod +x scripts/make-app.sh`

- [ ] **Step 3: Add the Makefile target**

```make
# Assemble an unsigned Lazyagent.app locally (for testing the bundle)
app: build
	scripts/make-app.sh ./lazyagent dev dist-app
```

Add `app` to `.PHONY`, and `rm -rf dist-app` to `clean`.

- [ ] **Step 4: Verify locally**

Run: `make app`
Expected: prints `Assembled dist-app/Lazyagent.app`; `plutil -lint` passed inside the script; `codesign.sh` prints its "skipping" warning (no identity locally). Then sanity-check the pieces:

```bash
test -x dist-app/Lazyagent.app/Contents/MacOS/lazyagent
sips -g pixelWidth dist-app/Lazyagent.app/Contents/Resources/Lazyagent.icns >/dev/null && echo icns-ok
```

- [ ] **Step 5: Commit**

```bash
git add assets/appicon.svg scripts/make-app.sh Makefile
git commit -m "feat(build): app icon and Lazyagent.app assembly script"
```

---

### Task 5: goreleaser split (universal binary, darwin-cli, renamed linux, cask + formula)

**Files:**
- Modify: `.goreleaser.yaml`

**Interfaces:**
- Consumes: `scripts/make-app.sh BINARY VERSION OUTDIR` (Task 4).
- Produces: release artifacts `Lazyagent_<ver>_darwin_universal.zip` (contains `Lazyagent.app`), `lazyagent-cli_<ver>_darwin_{arm64,amd64}.zip`, `lazyagent-cli_<ver>_linux_*.tar.gz`; cask installing the app; formula `lazyagent-cli`. Task 6's CI loop globs `dist/Lazyagent_*_darwin_universal.zip` and `dist/lazyagent-cli_*_darwin_*.zip`.

- [ ] **Step 1: Rework builds**

- `darwin` build: unchanged flags, but REMOVE its `hooks.post` codesign line (the bundle gets signed as a whole by `make-app.sh`; goreleaser would otherwise sign per-arch binaries that are then replaced by the universal one).
- Add after `builds:`... a `universal_binaries` section:

```yaml
universal_binaries:
  - id: darwin
    ids:
      - darwin
    name_template: lazyagent
    replace: true
    hooks:
      post:
        - scripts/make-app.sh "{{ .Path }}" "{{ .Version }}" dist
```

- Add the CLI build:

```yaml
  # macOS: CLI/TUI only (no Wails, no CGo) — ships as lazyagent-cli
  - id: darwin-cli
    main: .
    binary: lazyagent-cli
    env:
      - CGO_ENABLED=0
    ldflags:
      - -s -w
      - -X github.com/illegalstudio/lazyagent/internal/version.Version={{.Version}}
      - -X github.com/illegalstudio/lazyagent/internal/version.Commit={{.ShortCommit}}
    tags:
      - notray
    goos:
      - darwin
    goarch:
      - amd64
      - arm64
    hooks:
      post:
        - scripts/codesign.sh {{ .Path }}
```

- `linux` build: change `binary: lazyagent` → `binary: lazyagent-cli`.

- [ ] **Step 2: Rework archives**

```yaml
archives:
  # The desktop app: a zip containing Lazyagent.app (no bare binary).
  - id: darwin-app
    meta: true
    name_template: "Lazyagent_{{ .Version }}_darwin_universal"
    formats:
      - zip
    files:
      - src: dist/Lazyagent.app
        dst: Lazyagent.app
  - id: darwin-cli
    ids:
      - darwin-cli
    name_template: "lazyagent-cli_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    formats:
      - zip
    files:
      - README.md
      - LICENSE
  - id: linux
    ids:
      - linux
    name_template: "lazyagent-cli_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    formats:
      - tar.gz
    files:
      - README.md
      - LICENSE
```

(If the installed goreleaser rejects `ids:` under an archive or `meta` archives with `src/dst` file mappings, run `goreleaser check`, read the error, and use the schema-correct equivalent — the deliverable is fixed archive names and contents, not these exact keys.)

- [ ] **Step 3: Rework the tap section**

Replace `homebrew_casks` and add `brews`:

```yaml
homebrew_casks:
  - name: lazyagent
    ids:
      - darwin-app
    app: Lazyagent.app
    repository:
      owner: illegalstudio
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"
    homepage: https://github.com/illegalstudio/lazyagent
    description: Desktop app for monitoring coding agent sessions (includes the CLI)

brews:
  - name: lazyagent-cli
    ids:
      - darwin-cli
      - linux
    repository:
      owner: illegalstudio
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"
    homepage: https://github.com/illegalstudio/lazyagent
    description: A lazy TUI for monitoring coding agent sessions
```

- [ ] **Step 4: Validate config and dry-run**

Run: `goreleaser check`
Expected: config valid (iterate on schema errors per the Global Constraints rule).

Run: `goreleaser release --snapshot --clean --skip=publish`
Expected: completes; `dist/` contains `Lazyagent.app`, `Lazyagent_*_darwin_universal.zip` with the app inside (`unzip -l`), `lazyagent-cli_*_darwin_*.zip`, `lazyagent-cli_*_linux_*.tar.gz`. codesign.sh warns and skips locally. This takes several minutes (6 builds + frontend).

- [ ] **Step 5: Commit**

```bash
git add .goreleaser.yaml
git commit -m "feat(release): ship Lazyagent.app and lazyagent-cli artifacts"
```

---

### Task 6: CI notarization + staple for the app zip

**Files:**
- Modify: `.github/workflows/release.yml` (the "Notarize and update macOS binaries" step)

**Interfaces:**
- Consumes: artifact names from Task 5.
- Produces: notarized+stapled `Lazyagent_*_darwin_universal.zip` and notarized `lazyagent-cli_*_darwin_*.zip` in the GitHub release, refreshed checksums.

- [ ] **Step 1: Update the notarize loop**

Replace the loop body of the notarize step with:

```bash
          TAG="${GITHUB_REF#refs/tags/}"

          # App bundle: notarize, then staple the ticket into the .app
          # and re-zip so offline Gatekeeper checks pass.
          for archive in dist/Lazyagent_*_darwin_universal.zip; do
            echo "Notarizing $archive ..."
            xcrun notarytool submit "$archive" \
              --apple-id "$APPLE_ID" \
              --team-id "$APPLE_TEAM_ID" \
              --password "$APPLE_APP_PASSWORD" \
              --wait --timeout 360m

            STAPLE_DIR="$(mktemp -d)"
            ditto -x -k "$archive" "$STAPLE_DIR"
            xcrun stapler staple "$STAPLE_DIR/Lazyagent.app"
            rm -f "$archive"
            ditto -c -k --keepParent "$STAPLE_DIR/Lazyagent.app" "$archive"

            FILENAME="$(basename "$archive")"
            echo "Replacing $FILENAME in release $TAG ..."
            gh release delete-asset "$TAG" "$FILENAME" --repo "$GITHUB_REPOSITORY" --yes
            gh release upload "$TAG" "$archive" --repo "$GITHUB_REPOSITORY"
          done

          # CLI binaries: notarize the zips as before (no bundle to staple).
          for archive in dist/lazyagent-cli_*_darwin_*.zip; do
            echo "Notarizing $archive ..."
            xcrun notarytool submit "$archive" \
              --apple-id "$APPLE_ID" \
              --team-id "$APPLE_TEAM_ID" \
              --password "$APPLE_APP_PASSWORD" \
              --wait --timeout 360m

            FILENAME="$(basename "$archive")"
            echo "Replacing $FILENAME in release $TAG ..."
            gh release delete-asset "$TAG" "$FILENAME" --repo "$GITHUB_REPOSITORY" --yes
            gh release upload "$TAG" "$archive" --repo "$GITHUB_REPOSITORY"
          done

          # Update checksums
          echo "Updating checksums ..."
          CHECKSUMS="dist/lazyagent_${TAG#v}_checksums.txt"
          if [ -f "$CHECKSUMS" ]; then
            cd dist
            shasum -a 256 Lazyagent_*.zip lazyagent-cli_*.zip lazyagent-cli_*.tar.gz > "$(basename "$CHECKSUMS")"
            cd ..
            CHECKSUMS_NAME="$(basename "$CHECKSUMS")"
            gh release delete-asset "$TAG" "$CHECKSUMS_NAME" --repo "$GITHUB_REPOSITORY" --yes
            gh release upload "$TAG" "$CHECKSUMS" --repo "$GITHUB_REPOSITORY"
          fi
```

IMPORTANT: re-zipping after stapling changes the archive's checksum AND invalidates the sha256 goreleaser wrote into the cask. Add a follow-up step AFTER the notarize step that patches the cask in the tap with the final sha256:

```yaml
      - name: Update cask sha256 after stapling
        env:
          HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}
        run: |
          TAG="${GITHUB_REF#refs/tags/}"
          NEW_SHA="$(shasum -a 256 dist/Lazyagent_*_darwin_universal.zip | awk '{print $1}')"
          git clone "https://x-access-token:${HOMEBREW_TAP_TOKEN}@github.com/illegalstudio/homebrew-tap.git" tap
          cd tap
          # goreleaser writes the cask with the pre-staple sha256; fix it.
          sed -i '' -E "s/sha256 \"[0-9a-f]{64}\"/sha256 \"${NEW_SHA}\"/" Casks/lazyagent.rb
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git commit -am "lazyagent: update sha256 after notarization staple (${TAG})"
          git push
```

(Verify the cask filename/path in the tap repo — goreleaser v2 writes casks under `Casks/`; adjust the `sed` path if the tap layout differs.)

- [ ] **Step 2: Validate the workflow YAML**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/release.yml'))" && echo yaml-ok`
Expected: `yaml-ok`. (Full behavior is only provable on a real tag — flag this in the report as CI-verifiable-only.)

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci(release): notarize and staple Lazyagent.app, patch cask sha256"
```

---

### Task 7: Docs — install split, breaking change, GUI page, roadmap

**Files:**
- Modify: `README.md` (install section, command examples)
- Modify: `docs/interfaces/macos-gui.md`
- Modify: `docs/reference/roadmap.md` (in-place, per the project's current-behavior convention)

- [ ] **Step 1: README**

- Install section becomes two paths: `brew install --cask illegalstudio/tap/lazyagent` (desktop app; on first GUI launch it self-links `~/bin/lazyagent-cli`) and `brew install illegalstudio/tap/lazyagent-cli` (CLI only, darwin/linux).
- Add a "Breaking change" note: the `lazyagent` command is now `lazyagent-cli`; the desktop app is `Lazyagent.app`.
- Update command examples from `lazyagent …` to `lazyagent-cli …`.

- [ ] **Step 2: macos-gui.md**

- Describe the .app: LaunchServices identity (correct Cmd-Tab icon/name — the previous limitation is fixed), launch from Finder/login item starts the menu bar accessory, self-linked CLI at `~/bin/lazyagent-cli` (PATH order decides vs a brew-installed one, by design; never overwrites a non-symlink).

- [ ] **Step 3: roadmap.md**

Rewrite the relevant current-version bullets in place to reflect: desktop app ships as a bundled, signed, notarized `Lazyagent.app`; CLI ships as `lazyagent-cli`.

- [ ] **Step 4: Commit**

```bash
git add README.md
git add -f docs/interfaces/macos-gui.md docs/reference/roadmap.md
git commit -m "docs: Lazyagent.app + lazyagent-cli install split"
```

(The `-f` is needed only if the global gitignore interferes; plain `git add` is fine when it doesn't complain — `docs/interfaces` and `docs/reference` are normally tracked without force.)

---

### Task 8: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Automated pass**

```bash
go test ./... && go vet ./... && go build -tags notray ./... && make build && make app
```

Expected: all green; `dist-app/Lazyagent.app` assembled.

- [ ] **Step 2: Local bundle smoke test (manual, needs eyes)**

```bash
open dist-app/Lazyagent.app
```

Checklist: menu bar icon appears (no Dock icon at start — LSUIElement); detach → Dock + **Cmd-Tab show the Lazyagent icon and name** (the bug that started this); `~/bin/lazyagent-cli` exists and `~/bin/lazyagent-cli --version` prints the version; `lazyagent-cli --gui` from a terminal focuses/relaunches the app via LaunchServices; quit cleanly. Unsigned local builds may need right-click → Open the first time.

- [ ] **Step 3: Dev-flow regression**

```bash
make dev
```

Expected: bare-build fork path still works exactly as before (tray icon, detach).

- [ ] **Step 4: Commit any fixes found, then report**

The manual checklist results go to the human partner — CI-side behavior (notarization, cask/formula publication) is only provable on the next tagged release.
