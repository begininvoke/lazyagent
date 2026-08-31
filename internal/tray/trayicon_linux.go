//go:build !notray && linux

package tray

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// configurePlatformTray sets the StatusNotifierItem title (Linux hover
// text defaults to "Wails" when the label is empty) and a filled badge
// that contrasts with the panel. Template icons do not invert on SNI.
func configurePlatformTray(tray *application.SystemTray) {
	tray.SetLabel("Lazyagent")
	applyLinuxTrayIcon(tray, linuxPanelIsDark())
	go watchLinuxColorScheme(tray)
}

func applyLinuxTrayIcon(tray *application.SystemTray, darkPanel bool) {
	if darkPanel {
		tray.SetIcon(trayIconLinuxDark)
		return
	}
	tray.SetIcon(trayIconLinuxLight)
}

func linuxPanelIsDark() bool {
	if dark, ok := portalPrefersDark(); ok {
		return dark
	}
	// Portal unavailable: Linux bars are typically dark.
	return true
}
