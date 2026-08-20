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
// for the search field), View (density, limits, refresh), and Window.
// The built-in Quit role calls app.Quit() directly and would bypass the
// deadlock watchdog, so the Quit item is custom.
func installAppMenu(app *application.App, svc *SessionService) {
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

	// View menu: density is persisted Go-side, then broadcast so the
	// frontend store follows; limits is pure frontend state, so the menu
	// only emits and the webview opens it.
	viewMenu := menu.AddSubmenu("View")
	for i, d := range []string{"compact", "rich", "live"} {
		density := d
		viewMenu.Add("Density: " + density).
			SetAccelerator("CmdOrCtrl+" + string(rune('1'+i))).
			OnClick(func(ctx *application.Context) {
				_ = svc.SetCardDensity(density)
				app.Event.Emit("density:changed", density)
			})
	}
	viewMenu.AddSeparator()
	viewMenu.Add("Show Limits").
		SetAccelerator("CmdOrCtrl+l").
		OnClick(func(ctx *application.Context) {
			app.Event.Emit("menu:showLimits")
		})
	viewMenu.Add("Refresh Sessions").
		SetAccelerator("CmdOrCtrl+r").
		OnClick(func(ctx *application.Context) {
			svc.Refresh()
		})

	menu.AddRole(application.WindowMenu)

	// AppKit requires [NSApp setMainMenu:] to run on the main thread, and
	// Wails v3.0.0-alpha.74's macosApp.setApplicationMenu calls it directly
	// on the caller's goroutine with no internal dispatch. Detach() runs on
	// a Wails binding-call goroutine, so this must be forced onto the main
	// thread explicitly.
	application.InvokeSync(func() { app.Menu.SetApplicationMenu(menu) })
}
