//go:build !notray && !linux

package tray

import "github.com/wailsapp/wails/v3/pkg/application"

// configurePlatformTray uses a macOS template glyph so the menu bar
// icon follows light/dark automatically. Windows ignores the template
// flag but still picks up the same PNG.
func configurePlatformTray(tray *application.SystemTray) {
	tray.SetTemplateIcon(trayIcon)
}
