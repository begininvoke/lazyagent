# Desktop Mode on Detach — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the GUI detaches from the menu bar, lazyagent becomes a full macOS desktop app (Dock icon, Cmd-Tab, native menus) with a dashboard UI: a responsive grid of session cards in three switchable densities and a side-by-side detail panel.

**Architecture:** Go side adds a ~30-line cgo activation-policy toggle plus icon/menu installation wired into the existing `Detach()`/`Attach()` methods, and extends the `SessionItem` list payload with `currentTool`/`lastMessage`. Frontend refactors `App.svelte` into a thin router over shared stores, extracts the current UI as `PanelView`, and adds `DesktopView` (toolbar + card grid + push detail column).

**Tech Stack:** Go 1.25, Wails v3.0.0-alpha.74 (pinned), cgo/Objective-C (darwin only), Svelte 5 (runes), Tailwind CSS 4, Catppuccin Mocha tokens in `frontend/src/app.css`.

**Spec:** `docs/superpowers/specs/2026-08-17-desktop-mode-design.md`

## Global Constraints

- Branch: `feat/desktop-mode`. Commit after every task. No `Co-Authored-By` lines in commit messages; keep messages concise (user preference).
- No new Go or npm dependencies.
- Config JSON keys are snake_case (`card_density`, matching `window_minutes` etc. in `internal/core/config.go`).
- Card density values: exactly `compact`, `rich`, `live`. Default and fallback: `live`.
- `lastMessage` snippet: newest entry of `RecentMessages`, whitespace collapsed, truncated to 140 runes with `…` appended when truncated.
- GUI code lives behind `//go:build !notray`; new darwin-only code behind `!notray && darwin` with a non-darwin no-op stub. `go build -tags notray ./...` must stay green.
- After ANY change to `SessionService`'s exported method set or payload structs, run `make bindings` and commit the regenerated `frontend/src/bindings/` output in the same commit.
- Frontend verification for every frontend task: `cd frontend && npm run build` exits 0.
- The attached (menu bar) panel must be pixel-identical in behavior to today: any task touching `App.svelte` may move code but not change panel-mode rendering or shortcuts.
- macOS quit must always go through the existing 1.5s force-exit watchdog (Wails v3 alpha quit can deadlock — see comment in `internal/tray/app.go`).

---

### Task 1: `core.TruncateRunes` helper

**Files:**
- Create: `internal/core/strings.go`
- Test: `internal/core/strings_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func TruncateRunes(s string, max int) string` in package `core` — used by Task 2.

- [ ] **Step 1: Write the failing test**

```go
package core

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateRunes(t *testing.T) {
	if got := TruncateRunes("hello", 140); got != "hello" {
		t.Errorf("short string changed: %q", got)
	}
	if got := TruncateRunes("hello", 5); got != "hello" {
		t.Errorf("exact-length string changed: %q", got)
	}
	long := strings.Repeat("x", 150)
	got := TruncateRunes(long, 140)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("missing ellipsis: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 141 {
		t.Errorf("rune count = %d, want 141", n)
	}
	// Multibyte runes must not be split.
	accented := strings.Repeat("à", 150)
	got = TruncateRunes(accented, 140)
	if !utf8.ValidString(got) {
		t.Errorf("invalid UTF-8 after truncation")
	}
	if n := utf8.RuneCountInString(got); n != 141 {
		t.Errorf("accented rune count = %d, want 141", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestTruncateRunes -v`
Expected: FAIL (compile error: `undefined: TruncateRunes`)

- [ ] **Step 3: Write minimal implementation**

```go
package core

// TruncateRunes returns s unchanged when it contains at most max runes;
// otherwise the first max runes with "…" appended. Rune-safe: never
// splits a multibyte character.
func TruncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run TestTruncateRunes -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/strings.go internal/core/strings_test.go
git commit -m "feat(core): add rune-safe TruncateRunes helper"
```

---

### Task 2: Extend `SessionItem` payload with `currentTool` and `lastMessage`

**Files:**
- Modify: `internal/tray/service.go` (SessionItem struct ~line 148, SessionFull struct ~line 167, `buildSessionItem` ~line 204, SessionFull literal ~line 273)
- Create: `internal/tray/snippet_test.go`

**Interfaces:**
- Consumes: `core.TruncateRunes(s string, max int) string` (Task 1); `model.Session.RecentMessages []model.ConversationMessage` (chronological, newest LAST — see `internal/claude/jsonl.go:228`); `model.Session.CurrentTool string`.
- Produces: JSON list payload fields `currentTool` (string) and `lastMessage` (string) on every `SessionItem`; unexported `lastMessageSnippet(sess *model.Session) string`. Frontend Task 6 mirrors these in the TS interface.

- [ ] **Step 1: Write the failing test**

Create `internal/tray/snippet_test.go` (build tag required — the package is `!notray`):

```go
//go:build !notray

package tray

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestLastMessageSnippet(t *testing.T) {
	if got := lastMessageSnippet(&model.Session{}); got != "" {
		t.Errorf("empty session: got %q, want \"\"", got)
	}

	sess := &model.Session{RecentMessages: []model.ConversationMessage{
		{Role: "user", Text: "first"},
		{Role: "assistant", Text: "  line one\n\n\tline two  "},
	}}
	if got := lastMessageSnippet(sess); got != "line one line two" {
		t.Errorf("whitespace collapse: got %q", got)
	}

	long := &model.Session{RecentMessages: []model.ConversationMessage{
		{Role: "assistant", Text: strings.Repeat("y", 300)},
	}}
	got := lastMessageSnippet(long)
	if n := utf8.RuneCountInString(got); n != 141 {
		t.Errorf("rune count = %d, want 141 (140 + ellipsis)", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tray/ -run TestLastMessageSnippet -v`
Expected: FAIL (compile error: `undefined: lastMessageSnippet`). Note: this compiles Wails via cgo — the first run is slow.

- [ ] **Step 3: Implement**

In `internal/tray/service.go`:

Add to the `SessionItem` struct (after `SparklineData`):

```go
	CurrentTool   string    `json:"currentTool"`
	LastMessage   string    `json:"lastMessage"`
```

Remove from the `SessionFull` struct the now-duplicated field line:

```go
	CurrentTool         string             `json:"currentTool"`
```

and remove the `CurrentTool: sess.CurrentTool,` line from the `SessionFull` composite literal in `GetSessionDetail` (the value now arrives via the embedded `SessionItem`, filled by `buildSessionItem`).

Add to the `SessionItem` composite literal in `buildSessionItem`:

```go
		CurrentTool:   sess.CurrentTool,
		LastMessage:   lastMessageSnippet(sess),
```

Add the helper (bottom of service.go):

```go
// lastMessageSnippet returns a short single-line snippet of the newest
// recent message, shown on the desktop "live" card. RecentMessages is
// chronological, so the newest entry is the last one.
func lastMessageSnippet(sess *model.Session) string {
	if len(sess.RecentMessages) == 0 {
		return ""
	}
	text := sess.RecentMessages[len(sess.RecentMessages)-1].Text
	text = strings.Join(strings.Fields(text), " ")
	return core.TruncateRunes(text, 140)
}
```

(`strings` is already imported in service.go.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tray/ ./internal/core/ -v -run 'TestLastMessageSnippet|TestTruncateRunes'`
Expected: PASS. Then `go build ./...` — must succeed (catches the SessionFull field removal).

- [ ] **Step 5: Regenerate bindings and commit**

```bash
make bindings
git add internal/tray/service.go internal/tray/snippet_test.go frontend/src/bindings
git commit -m "feat(gui): expose currentTool and lastMessage in session list payload"
```

---

### Task 3: Card density config + service accessors

**Files:**
- Modify: `internal/core/config.go` (Config struct ~line 100)
- Create: `internal/core/carddensity_test.go`
- Modify: `internal/tray/service.go`

**Interfaces:**
- Consumes: `core.LoadConfig() Config`, `core.SaveConfig(cfg Config) error` (both exist in config.go).
- Produces: `Config.CardDensity string` (JSON `card_density`); `func NormalizeCardDensity(d string) string` in package `core`; service methods `GetCardDensity() string` and `SetCardDensity(d string) error` — the frontend (Task 10) calls these via generated bindings.

- [ ] **Step 1: Write the failing test**

Create `internal/core/carddensity_test.go`:

```go
package core

import (
	"encoding/json"
	"testing"
)

func TestNormalizeCardDensity(t *testing.T) {
	for _, valid := range []string{"compact", "rich", "live"} {
		if got := NormalizeCardDensity(valid); got != valid {
			t.Errorf("NormalizeCardDensity(%q) = %q, want passthrough", valid, got)
		}
	}
	for _, invalid := range []string{"", "dense", "LIVE"} {
		if got := NormalizeCardDensity(invalid); got != "live" {
			t.Errorf("NormalizeCardDensity(%q) = %q, want \"live\"", invalid, got)
		}
	}
}

func TestCardDensityJSONKey(t *testing.T) {
	b, err := json.Marshal(Config{CardDensity: "rich"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["card_density"] != "rich" {
		t.Errorf("card_density key missing or wrong: %v", m)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run 'TestNormalizeCardDensity|TestCardDensityJSONKey' -v`
Expected: FAIL (compile error: unknown field `CardDensity`, undefined `NormalizeCardDensity`)

- [ ] **Step 3: Implement in `internal/core/config.go`**

Add to the `Config` struct (after the `TUI` field):

```go
	// CardDensity is the GUI desktop-mode card density: "compact",
	// "rich" or "live". Empty or invalid values mean "live".
	CardDensity string `json:"card_density,omitempty"`
```

Add near the other validation helpers:

```go
// validCardDensities lists the accepted desktop card density values.
var validCardDensities = map[string]struct{}{
	"compact": {}, "rich": {}, "live": {},
}

// NormalizeCardDensity returns d if it is a valid card density, "live" otherwise.
func NormalizeCardDensity(d string) string {
	if _, ok := validCardDensities[d]; ok {
		return d
	}
	return "live"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run 'TestNormalizeCardDensity|TestCardDensityJSONKey' -v`
Expected: PASS

- [ ] **Step 5: Add service accessors**

In `internal/tray/service.go` (near `GetConfig`):

```go
// GetCardDensity returns the persisted desktop card density. Missing or
// invalid values fall back to "live".
func (s *SessionService) GetCardDensity() string {
	return core.NormalizeCardDensity(core.LoadConfig().CardDensity)
}

// SetCardDensity persists the desktop card density choice.
func (s *SessionService) SetCardDensity(d string) error {
	if core.NormalizeCardDensity(d) != d {
		return fmt.Errorf("invalid card density %q", d)
	}
	cfg := core.LoadConfig()
	cfg.CardDensity = d
	return core.SaveConfig(cfg)
}
```

(`fmt` is already imported in service.go.)

- [ ] **Step 6: Build, regenerate bindings, commit**

```bash
go build ./... && make bindings
git add internal/core/config.go internal/core/carddensity_test.go internal/tray/service.go frontend/src/bindings
git commit -m "feat(gui): persist desktop card density in config"
```

---

### Task 4: Activation policy toggle, app icon, native menus, quit helper

**Files:**
- Create: `internal/tray/activation_darwin.go`
- Create: `internal/tray/activation_other.go`
- Create: `internal/tray/appicon.png` (copy of `assets/icon.png`)
- Create: `internal/tray/menu.go`
- Modify: `internal/tray/icon.go` (add appicon embed)
- Modify: `internal/tray/app.go` (Quit item uses the shared helper)

**Interfaces:**
- Consumes: Wails `application.App` (`app.SetIcon(icon []byte)`, `app.Menu.SetApplicationMenu(*Menu)`, `application.NewMenu()`, `Menu.AddSubmenu`, `Menu.AddRole`, role constants `About`, `Hide`, `HideOthers`, `UnHide`, `EditMenu`, `WindowMenu` — all verified present in v3.0.0-alpha.74).
- Produces: `setDesktopActivation(regular bool)` (all `!notray` platforms), `installAppMenu(app *application.App)`, `quitWithWatchdog(app *application.App)`, `var appIcon []byte` — consumed by Task 5.

- [ ] **Step 1: Copy the colored app icon into the package**

```bash
cp assets/icon.png internal/tray/appicon.png
```

(`internal/tray/icon.png` is the monochrome menu bar template icon — do NOT reuse it for the Dock.)

- [ ] **Step 2: Add the embed to `internal/tray/icon.go`**

```go
//go:embed appicon.png
var appIcon []byte
```

- [ ] **Step 3: Create `internal/tray/activation_darwin.go`**

```go
//go:build !notray && darwin

package tray

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

static void lazyagent_setActivationPolicy(bool regular) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (regular) {
			[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
			[NSApp activateIgnoringOtherApps:YES];
		} else {
			[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
		}
	});
}
*/
import "C"

// setDesktopActivation switches the app between a Regular desktop app
// (Dock icon, Cmd-Tab entry) and a menu bar Accessory. Wails v3 only
// applies the activation policy at startup, so this goes straight to
// AppKit. Safe from any goroutine: the call is dispatched onto the main
// queue. Switching to Regular also activates the app so the detached
// window comes to the foreground with keyboard focus.
func setDesktopActivation(regular bool) {
	C.lazyagent_setActivationPolicy(C.bool(regular))
}
```

- [ ] **Step 4: Create `internal/tray/activation_other.go`**

```go
//go:build !notray && !darwin

package tray

// setDesktopActivation is a no-op off macOS: activation policies and the
// Dock are AppKit concepts.
func setDesktopActivation(regular bool) {}
```

- [ ] **Step 5: Create `internal/tray/menu.go`**

```go
//go:build !notray

package tray

import (
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// quitWithWatchdog quits the app, force-exiting if the Wails v3 alpha
// macOS shutdown deadlocks. See the detailed comment on the tray Quit
// item in app.go: on the deadlock path app.Quit() never returns, so the
// watchdog must start first.
func quitWithWatchdog(app *application.App) {
	go func() {
		time.Sleep(1500 * time.Millisecond)
		os.Exit(0)
	}()
	app.Quit()
}

// installAppMenu installs the native application menu used in desktop
// (detached) mode: App menu with standard roles, Edit (clipboard support
// for the search field), and Window. The built-in Quit role calls
// app.Quit() directly and would bypass the deadlock watchdog, so the
// Quit item is custom.
func installAppMenu(app *application.App) {
	menu := application.NewMenu()

	appMenu := menu.AddSubmenu("lazyagent")
	appMenu.AddRole(application.About)
	appMenu.AddSeparator()
	appMenu.AddRole(application.Hide)
	appMenu.AddRole(application.HideOthers)
	appMenu.AddRole(application.UnHide)
	appMenu.AddSeparator()
	appMenu.Add("Quit lazyagent").
		SetAccelerator("CmdOrCtrl+q").
		OnClick(func(ctx *application.Context) { quitWithWatchdog(app) })

	menu.AddRole(application.EditMenu)
	menu.AddRole(application.WindowMenu)

	app.Menu.SetApplicationMenu(menu)
}
```

- [ ] **Step 6: Reuse the helper for the tray context menu**

In `internal/tray/app.go`, replace the body of the `Quit lazyagent` OnClick handler (keep the existing explanatory comment, then):

```go
	menu.Add("Quit lazyagent").OnClick(func(ctx *application.Context) {
		quitWithWatchdog(app)
	})
```

Remove the now-unused `os` and `time` imports from app.go **only if** nothing else in the file uses them (check first — `os` is used by the slog handler; keep it).

- [ ] **Step 7: Verify all build flavors**

Run: `go build ./... && go vet ./... && go build -tags notray ./...`
Expected: all succeed. (`activation_other.go` is exercised by CI/linux builds, not locally.)

- [ ] **Step 8: Commit**

```bash
git add internal/tray/activation_darwin.go internal/tray/activation_other.go internal/tray/appicon.png internal/tray/icon.go internal/tray/menu.go internal/tray/app.go
git commit -m "feat(gui): runtime activation policy toggle, app icon, native menus"
```

---

### Task 5: Wire desktop mode into Detach()/Attach()

**Files:**
- Modify: `internal/tray/service.go` (struct ~line 27, `Detach` ~line 445, `Attach` ~line 460)

**Interfaces:**
- Consumes: `setDesktopActivation(bool)`, `installAppMenu(*application.App)`, `appIcon []byte` (Task 4).
- Produces: desktop-mode behavior on the existing bound methods `Detach()` / `Attach()` — no signature changes, no binding regeneration needed.

- [ ] **Step 1: Add the one-time setup field**

Add to the `SessionService` struct (after `pinned bool`):

```go
	desktopOnce    sync.Once // one-time desktop setup: app icon + native menu
```

(`sync` is already imported.)

- [ ] **Step 2: Wire `Detach()`**

Replace the tail of `Detach()` (after the mutex block) with:

```go
	s.panelWindow.Hide()
	s.desktopOnce.Do(func() {
		s.app.SetIcon(appIcon)
		installAppMenu(s.app)
	})
	setDesktopActivation(true)
	s.detachedWindow.Show().Focus()
	s.detachedWindow.Center()
	s.emitDetachChanged()
```

(Policy switch before `Show()` so the window appears as a normal app window; activation happens inside the dispatch after the policy change.)

- [ ] **Step 3: Wire `Attach()`**

In `Attach()`, after `s.detachedWindow.Hide()` add:

```go
	setDesktopActivation(false)
```

- [ ] **Step 4: Build and smoke-test**

Run: `go build ./...` — must pass.
Then: `make dev` — in the GUI press `d` (or click ⤢): the Dock icon and Cmd-Tab entry must appear and the window come to front with native menus in the menu bar. Press `d` again: window hides, Dock icon disappears, tray panel works. Quit via tray menu.

- [ ] **Step 5: Commit**

```bash
git add internal/tray/service.go
git commit -m "feat(gui): become a desktop app on detach, accessory on attach"
```

---

### Task 6: Frontend stores + shared actions refactor

**Files:**
- Modify: `frontend/src/lib/stores.ts`
- Create: `frontend/src/lib/actions.ts`
- Modify: `frontend/src/App.svelte`

**Interfaces:**
- Consumes: generated bindings in `frontend/src/bindings/.../sessionservice` (incl. `GetCardDensity`/`SetCardDensity` from Task 3).
- Produces (for Tasks 7–10):
  - stores: `cardDensity: Writable<CardDensity>`, `searching: Writable<boolean>`, `showLimits: Writable<boolean>`, `limitsRefreshToken: Writable<number>`, `updateVersion: Writable<string>`, `isDetached: Writable<boolean>`, `isPinned: Writable<boolean>`; `type CardDensity = "compact" | "rich" | "live"`.
  - `SessionItem` TS interface gains `agent: string; source: string; currentTool: string; lastMessage: string;`; `SessionFull` loses its own `currentTool` (now inherited).
  - actions: `loadSessions(): Promise<void>`, `loadDetail(id: string): Promise<void>`, `cycleFilter(): void`, `adjustWindow(delta: number): void`, `toggleDetach(): void`, `togglePin(): void`, `setSearch(value: string): void`, `syncDetachState(): void`.

- [ ] **Step 1: Extend `stores.ts`**

Add to the `SessionItem` interface:

```ts
  agent: string;
  source: string;
  currentTool: string;
  lastMessage: string;
```

Remove `currentTool: string;` from the `SessionFull` interface (inherited now). Add after the existing stores:

```ts
export type CardDensity = "compact" | "rich" | "live";
export const cardDensity = writable<CardDensity>("live");
export const searching = writable(false);
export const showLimits = writable(false);
export const limitsRefreshToken = writable(0);
export const updateVersion = writable("");
export const isDetached = writable(false);
export const isPinned = writable(false);
```

- [ ] **Step 2: Create `frontend/src/lib/actions.ts`**

Move the service-calling logic out of `App.svelte`, operating on stores via `get()`:

```ts
import { get } from "svelte/store";
import {
  sessions, selectedDetail, windowMinutes, activityFilter, searchQuery,
  isDetached, isPinned,
} from "./stores";
import * as SessionService from "../bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";

export async function loadSessions(): Promise<void> {
  try {
    const items = await SessionService.GetSessions();
    sessions.set(items || []);
  } catch {
    // Dev mode without Go backend
  }
}

export async function loadDetail(id: string): Promise<void> {
  try {
    selectedDetail.set((await SessionService.GetSessionDetail(id)) as any);
  } catch {
    selectedDetail.set(null);
  }
}

const allFilters = ["", "idle", "waiting", "thinking", "compacting", "reading", "writing", "running", "searching", "browsing", "spawning"];

export function cycleFilter(): void {
  const idx = allFilters.indexOf(get(activityFilter));
  const next = allFilters[(idx + 1) % allFilters.length];
  activityFilter.set(next);
  SessionService.SetActivityFilter(next).catch(() => {});
  loadSessions();
}

export function adjustWindow(delta: number): void {
  const next = Math.max(10, Math.min(480, get(windowMinutes) + delta));
  windowMinutes.set(next);
  SessionService.SetWindowMinutes(next).catch(() => {});
  loadSessions();
}

export function toggleDetach(): void {
  if (get(isDetached)) {
    SessionService.Attach().catch(() => {});
  } else {
    SessionService.Detach().catch(() => {});
  }
}

export function togglePin(): void {
  SessionService.TogglePin().catch(() => {});
}

export function setSearch(value: string): void {
  searchQuery.set(value);
  SessionService.SetSearchQuery(value).catch(() => {});
  loadSessions();
}

export function syncDetachState(): void {
  SessionService.IsDetached().then((d) => isDetached.set(d)).catch(() => {});
  SessionService.IsPinned().then((p) => isPinned.set(p)).catch(() => {});
}
```

- [ ] **Step 3: Rewire `App.svelte`**

Mechanical conversion — rendering and behavior must not change:
- Delete local `$state` for `searching`, `showLimits`, `limitsRefreshToken`, `updateVersion`, `isDetached`, `isPinned`; use the `$store` forms (`$searching`, `$showLimits`, …) everywhere in the markup and handlers.
- Delete the local `loadSessions`/`loadDetail`/`cycleFilter`/`adjustWindow`/`toggleDetach`/`syncDetachState`/`handleSearchInput`/`allFilters` definitions; import from `./lib/actions` (for search: `oninput={(e) => setSearch((e.target as HTMLInputElement).value)}`; the Escape branch in `handleKeydown` calls `setSearch("")` then `$searching = false`).
- The pin button calls `togglePin()` from actions; the update check writes `$updateVersion`.
- Keep: `handleKeydown`, `onMount` lifecycle (initial loads, `Events.On` subscriptions, `syncDetachState()`, update check), the `$effect` on `$selectedId`, and all markup.

- [ ] **Step 4: Verify**

Run: `cd frontend && npm run build`
Expected: exit 0. Then `make dev`: panel looks and behaves exactly as before (search, filter, limits, +/-, detach/attach, pin).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/stores.ts frontend/src/lib/actions.ts frontend/src/App.svelte
git commit -m "refactor(frontend): move UI state to stores and shared actions"
```

---

### Task 7: Extract `PanelView.svelte`

**Files:**
- Create: `frontend/src/lib/PanelView.svelte`
- Modify: `frontend/src/App.svelte`

**Interfaces:**
- Consumes: stores + actions from Task 6; existing `SessionList`, `SessionDetail`, `LimitsPage` components.
- Produces: `PanelView.svelte` (no props — reads stores directly). `App.svelte` becomes: global keydown + lifecycle + `<PanelView />`. Task 9 turns this into the `{#if $isDetached}` router.

- [ ] **Step 1: Create `PanelView.svelte`**

Cut the entire template of `App.svelte` (the `<div class="flex flex-col h-screen bg-surface">` block: header, search bar, content, footer) plus the derived `showDetail` and paste into `PanelView.svelte`. Imports: stores (`selectedId`, `activeCount`, `windowMinutes`, `activityFilter`, `searchQuery`, `searching`, `showLimits`, `limitsRefreshToken`, `updateVersion`, `isDetached`, `isPinned`), actions (`cycleFilter`, `adjustWindow`, `toggleDetach`, `togglePin`, `setSearch`), components (`SessionList`, `SessionDetail`, `LimitsPage`), `SessionService` (for `OpenReleases`). Markup is moved verbatim.

- [ ] **Step 2: Slim `App.svelte`**

`App.svelte` keeps: `<svelte:window onkeydown={handleKeydown} />`, `handleKeydown`, `onMount`, the `$effect` on `$selectedId` — and renders `<PanelView />` only. The derived `showDetail` moved to `PanelView`, so in `handleKeydown` replace the `if (showDetail)` check in the Escape branch with `if ($selectedId !== null)` (same semantics: Escape deselects when a detail is open).

- [ ] **Step 3: Verify**

Run: `cd frontend && npm run build`
Expected: exit 0. `make dev`: panel unchanged.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/PanelView.svelte frontend/src/App.svelte
git commit -m "refactor(frontend): extract PanelView from App"
```

---

### Task 8: `SessionCard.svelte` — three densities

**Files:**
- Create: `frontend/src/lib/SessionCard.svelte`

**Interfaces:**
- Consumes: `SessionItem` and `CardDensity` types, `activityColor`, `formatCost`, `timeAgo` from stores; `Sparkline`, `ActivityBadge` components.
- Produces: `<SessionCard session={...} density={...} selected={...} onselect={(id: string) => void} />` — used by Task 9.

- [ ] **Step 1: Create the component**

```svelte
<script lang="ts">
  import type { SessionItem, CardDensity } from "./stores";
  import { activityColor, formatCost, timeAgo } from "./stores";
  import Sparkline from "./Sparkline.svelte";
  import ActivityBadge from "./ActivityBadge.svelte";

  interface Props {
    session: SessionItem;
    density: CardDensity;
    selected: boolean;
    onselect: (id: string) => void;
  }
  let { session, density, selected, onselect }: Props = $props();

  let color = $derived(activityColor(session.activity));
  let name = $derived(session.customName || session.agentName || session.shortName);

  const glyphs: Record<string, string> = {
    pi: "π", opencode: "O", kilo: "L", cursor: "C", codex: "X", amp: "A", kimi: "K",
  };
  let glyph = $derived(
    glyphs[session.agent] ?? (session.source === "desktop" ? "D" : "")
  );
</script>

<button
  class="flex flex-col text-left rounded-lg border bg-surface px-3 py-2.5 transition-colors duration-75 no-drag
    {selected ? 'border-accent' : 'border-border hover:border-subtext/50'}"
  onclick={() => onselect(session.sessionId)}
>
  <div class="flex items-center justify-between gap-2 w-full">
    <div class="flex items-center gap-1.5 min-w-0">
      <span
        class="shrink-0 h-2 w-2 rounded-full"
        class:animate-pulse-dot={session.isActive}
        style="background: {color};"
      ></span>
      <span class="truncate text-[13px] font-semibold text-text">
        {#if glyph}<span class="{session.agent === 'pi' ? 'text-activity-spawning' : session.source === 'desktop' ? 'text-accent' : 'text-subtext'} font-normal">{glyph}</span>{/if}
        {name}
      </span>
    </div>
    <div class="shrink-0">
      <ActivityBadge activity={session.activity} isActive={session.isActive} />
    </div>
  </div>

  <div class="mt-1.5">
    <Sparkline data={session.sparklineData} {color} width={140} height={16} />
  </div>

  <div class="mt-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-0.5 text-[10.5px] text-subtext w-full">
    {#if density !== "compact"}
      {#if session.model}<span class="truncate max-w-[9rem]">{session.model}</span>{/if}
      {#if session.gitBranch}<span class="truncate max-w-[9rem]">⎇ {session.gitBranch}</span>{/if}
    {/if}
    <span class="text-activity-reading">{formatCost(session.costUsd)}</span>
    {#if density === "compact"}
      <span>{timeAgo(session.lastActivity)}</span>
    {:else}
      <span>{session.totalMessages} msg</span>
    {/if}
  </div>

  {#if density !== "compact" && session.currentTool}
    <div class="mt-2 pt-1.5 border-t border-surface-hover w-full flex items-center gap-1.5 text-[10.5px] text-subtext">
      <span>▸</span>
      <code class="bg-surface-hover rounded px-1 py-px text-[10px] text-text">{session.currentTool}</code>
    </div>
  {/if}

  {#if density === "live" && session.lastMessage}
    <div class="mt-1.5 w-full rounded border-l-2 border-border bg-surface/60 px-2 py-1 text-[10.5px] italic leading-snug text-subtext line-clamp-2">
      {session.lastMessage}
    </div>
  {/if}
</button>
```

Note: Tailwind 4 ships `line-clamp-*` in core. The card background sits on the darker desktop backdrop (Task 9 uses a darker page background so `bg-surface` cards read as raised).

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npm run build`
Expected: exit 0 (component not yet mounted anywhere — visual check comes with Task 9).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/SessionCard.svelte
git commit -m "feat(frontend): session card with compact/rich/live densities"
```

---

### Task 9: `DesktopView.svelte`, `DetailPanel.svelte`, router

**Files:**
- Create: `frontend/src/lib/DesktopView.svelte`
- Create: `frontend/src/lib/DetailPanel.svelte`
- Modify: `frontend/src/App.svelte` (router)

**Interfaces:**
- Consumes: `SessionCard` (Task 8), stores + actions (Task 6), `SessionDetail`, `LimitsPage`, `SessionService.OpenReleases`.
- Produces: detached-mode UI. `App.svelte` renders `{#if $isDetached}<DesktopView />{:else}<PanelView />{/if}`.

- [ ] **Step 1: Create `DetailPanel.svelte`**

```svelte
<script lang="ts">
  import { selectedId, selectedDetail } from "./stores";
  import SessionDetail from "./SessionDetail.svelte";

  let title = $derived(
    $selectedDetail
      ? $selectedDetail.customName || $selectedDetail.agentName || $selectedDetail.shortName
      : ""
  );
</script>

<div class="flex flex-col h-full border-l border-border bg-surface">
  <div class="flex items-center justify-between px-3 py-1.5 border-b border-border">
    <span class="truncate text-[11px] font-medium text-subtext">{title}</span>
    <button
      class="text-subtext hover:text-text text-[13px] leading-none px-1"
      onclick={() => ($selectedId = null)}
      title="Close detail (esc)"
    >✕</button>
  </div>
  <div class="flex-1 min-h-0 overflow-hidden">
    <SessionDetail />
  </div>
</div>
```

- [ ] **Step 2: Create `DesktopView.svelte`**

```svelte
<script lang="ts">
  import {
    sessions, selectedId, activeCount, windowMinutes, activityFilter,
    searchQuery, searching, showLimits, limitsRefreshToken, updateVersion,
    isPinned, cardDensity,
  } from "./stores";
  import type { CardDensity } from "./stores";
  import {
    cycleFilter, adjustWindow, toggleDetach, togglePin, setSearch,
  } from "./actions";
  import SessionCard from "./SessionCard.svelte";
  import DetailPanel from "./DetailPanel.svelte";
  import LimitsPage from "./LimitsPage.svelte";
  import * as SessionService from "../bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";

  const densities: CardDensity[] = ["compact", "rich", "live"];
  const minCardWidth: Record<CardDensity, number> = { compact: 220, rich: 260, live: 300 };

  function pickDensity(d: CardDensity) {
    $cardDensity = d;
    SessionService.SetCardDensity(d).catch(() => {});
  }

  function select(id: string) {
    $selectedId = $selectedId === id ? null : id;
  }

  // Grid j/k navigation while the detail panel is open or closed.
  function handleKeydown(e: KeyboardEvent) {
    const tag = (e.target as HTMLElement)?.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA") return;
    const list = $sessions;
    if (!list.length) return;
    const idx = list.findIndex((s) => s.sessionId === $selectedId);
    if (e.key === "j" || e.key === "ArrowDown") {
      e.preventDefault();
      $selectedId = list[Math.min(idx + 1, list.length - 1)].sessionId;
    } else if (e.key === "k" || e.key === "ArrowUp") {
      e.preventDefault();
      $selectedId = list[Math.max(idx - 1, 0)].sessionId;
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="flex flex-col h-screen bg-[#11111b]">
  <!-- Toolbar -->
  <header class="flex items-center justify-between px-4 py-2 bg-surface border-b border-border drag-region shrink-0">
    <div class="flex items-center gap-2 no-drag">
      <h1 class="text-[14px] font-bold text-accent">lazyagent</h1>
      <span class="text-[11px] text-subtext">{$activeCount} active</span>
    </div>
    <div class="flex items-center gap-2.5 no-drag">
      <div class="flex rounded-md bg-surface-hover overflow-hidden">
        {#each densities as d}
          <button
            class="px-2 py-0.5 text-[10.5px] {$cardDensity === d ? 'bg-accent text-surface font-semibold' : 'text-subtext hover:text-text'}"
            onclick={() => pickDensity(d)}
          >{d}</button>
        {/each}
      </div>
      <button
        class="rounded px-1.5 py-0.5 text-[11px] font-medium {$showLimits ? 'text-accent bg-accent/10' : 'text-subtext hover:text-text'}"
        onclick={() => ($showLimits = !$showLimits)}
        title="Show limits (l)"
      >limits</button>
      {#if $activityFilter}
        <button
          class="rounded px-1.5 py-0.5 text-[11px] font-medium text-accent bg-accent/10 hover:bg-accent/20"
          onclick={cycleFilter}
        >{$activityFilter}</button>
      {/if}
      <span class="text-[11px] text-subtext">{$windowMinutes}m</span>
      <button class="text-subtext hover:text-text text-[14px] leading-none" onclick={() => adjustWindow(-10)} title="Decrease time window">−</button>
      <button class="text-subtext hover:text-text text-[14px] leading-none" onclick={() => adjustWindow(10)} title="Increase time window">+</button>
      <button
        class="leading-none text-[11px] font-medium rounded px-1 py-0.5 {$isPinned ? 'text-accent bg-accent/10' : 'text-subtext hover:text-text'}"
        onclick={togglePin}
        title={$isPinned ? "Unpin from top" : "Pin on top"}
      >pin</button>
      <button
        class="text-subtext hover:text-text text-[14px] leading-none"
        onclick={toggleDetach}
        title="Attach to tray (d)"
      >⤡</button>
    </div>
  </header>

  <!-- Search bar -->
  {#if $searching}
    <div class="px-4 py-1.5 bg-surface border-b border-border shrink-0">
      <input
        type="text"
        class="w-full bg-transparent text-text text-[13px] outline-none placeholder-subtext"
        placeholder="Search sessions..."
        value={$searchQuery}
        oninput={(e) => setSearch((e.target as HTMLInputElement).value)}
      />
    </div>
  {/if}

  <!-- Content -->
  <div class="flex-1 flex min-h-0">
    {#if $showLimits}
      <div class="flex-1 overflow-hidden bg-surface">
        <LimitsPage refreshToken={$limitsRefreshToken} />
      </div>
    {:else}
      <div class="flex-1 min-w-0 overflow-y-auto p-3">
        {#if $sessions.length}
          <div
            class="grid gap-3"
            style="grid-template-columns: repeat(auto-fill, minmax({minCardWidth[$cardDensity]}px, 1fr));"
          >
            {#each $sessions as session (session.sessionId)}
              <SessionCard
                {session}
                density={$cardDensity}
                selected={session.sessionId === $selectedId}
                onselect={select}
              />
            {/each}
          </div>
        {:else}
          <div class="flex items-center justify-center h-full text-subtext text-sm">
            No sessions found
          </div>
        {/if}
      </div>
      {#if $selectedId}
        <div class="w-[400px] shrink-0 min-h-0">
          <DetailPanel />
        </div>
      {/if}
    {/if}
  </div>

  <!-- Footer -->
  <footer class="px-3 py-1 bg-surface border-t border-border shrink-0">
    {#if $updateVersion}
      <div class="flex items-center justify-center gap-1 text-[10px] text-accent pb-0.5">
        <span>↑ lazyagent {$updateVersion} available —</span>
        <button class="underline hover:text-text cursor-pointer" onclick={() => SessionService.OpenReleases()}>releases</button>
      </div>
    {/if}
    <div class="flex items-center justify-center gap-3 text-[10px] text-subtext">
      <span><kbd class="text-text/60">j/k</kbd> navigate</span>
      <span><kbd class="text-text/60">/</kbd> search</span>
      <span><kbd class="text-text/60">f</kbd> filter</span>
      <span><kbd class="text-text/60">l</kbd> limits</span>
      <span><kbd class="text-text/60">+/−</kbd> window</span>
      <span><kbd class="text-text/60">d</kbd> attach</span>
      <span><kbd class="text-text/60">esc</kbd> close</span>
    </div>
  </footer>
</div>
```

- [ ] **Step 3: Router in `App.svelte`**

```svelte
{#if $isDetached}
  <DesktopView />
{:else}
  <PanelView />
{/if}
```

with the two imports added. `SessionList`'s own `j/k` `svelte:window` handler only exists while `PanelView` is mounted, so it cannot double-fire with `DesktopView`'s.

- [ ] **Step 4: Verify**

Run: `cd frontend && npm run build` → exit 0.
Then `make dev`: detach → dashboard grid appears (live cards), click a card → push panel opens and grid narrows, `j`/`k` moves selection, `Esc` closes panel, density switch changes the grid, limits toggle works, re-attach (`d`) → compact panel unchanged.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/DesktopView.svelte frontend/src/lib/DetailPanel.svelte frontend/src/App.svelte
git commit -m "feat(frontend): desktop dashboard with card grid and push detail panel"
```

---

### Task 10: Load persisted density at startup

**Files:**
- Modify: `frontend/src/App.svelte` (onMount)

**Interfaces:**
- Consumes: `SessionService.GetCardDensity(): Promise<string>` (Task 3 bindings), `cardDensity` store, `core.NormalizeCardDensity` semantics (backend already normalizes, so the value is always valid).

- [ ] **Step 1: Fetch once on mount**

In `App.svelte`'s `onMount`, alongside the `GetWindowMinutes` fetch:

```ts
SessionService.GetCardDensity()
  .then((d) => $cardDensity = d as CardDensity)
  .catch(() => {});
```

(import `cardDensity` and `type CardDensity` from stores.)

- [ ] **Step 2: Verify persistence end-to-end**

Run: `cd frontend && npm run build` → exit 0. Then `make dev`: detach, switch density to `compact`, quit the app (tray menu), `make dev` again, detach → density is still `compact`. Check `~/.config/lazyagent/config.json` (or the path `lazyagent` prints via `core.ConfigPath()`) contains `"card_density": "compact"`. Switch back to `live` when done.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/App.svelte
git commit -m "feat(frontend): load persisted card density on startup"
```

---

### Task 11: Full verification, manual checklist, docs

**Files:**
- Modify: `README.md` (GUI section — mention desktop mode on detach)
- Modify: `docs/superpowers/specs/2026-08-17-desktop-mode-design.md` (align the config key mention `cardDensity` → `card_density`)

- [ ] **Step 1: Full automated pass**

```bash
go test ./... && go vet ./... && go build -tags notray ./... && make build
```

Expected: all green.

- [ ] **Step 2: Manual checklist (run the built binary: `./lazyagent --gui`)**

- Dock icon appears on detach, disappears on attach — repeat the cycle 3×, watch for focus/ordering glitches after re-attach (spec's known-quirk area).
- Cmd-Tab lists lazyagent while detached.
- Native menus: About opens, Edit roles work in the search field (Cmd+C/Cmd+V), Window → Minimize works.
- Cmd+W re-attaches (window closes back to tray mode); red traffic light does the same.
- Cmd+Q quits fully with no orphaned process (`pgrep lazyagent` empty after ~2s).
- Tray icon visible in desktop mode; tray click toggles the detached window.
- Three densities render per the mockups; `live` shows tool + message snippet; long messages clamp at 2 lines.
- Push panel: open/close, `j`/`k` with panel open, `Esc` closes, rename (`r`, double-click) still works in the panel.
- Attached panel identical to pre-change behavior.

- [ ] **Step 3: Update docs**

In `README.md`, extend the GUI/detach description: detaching now turns lazyagent into a full desktop app (Dock icon, Cmd-Tab, native menus) with a card-grid dashboard and a `compact | rich | live` density switch; attaching returns it to the menu bar. In the spec, change the literal `cardDensity` to `card_density`.

- [ ] **Step 4: Commit**

```bash
git add README.md
git add -f docs/superpowers/specs/2026-08-17-desktop-mode-design.md
git commit -m "docs: desktop mode on detach"
```
