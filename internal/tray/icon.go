//go:build !notray

package tray

import _ "embed"

//go:embed icon.png
var trayIcon []byte

// Linux tray badges. Template glyphs do not invert on StatusNotifierItem,
// so these are opaque rounded squares that contrast with the panel:
// dark = white square / black glyph, light = black square / white glyph.
//
//go:embed icon-linux-dark.png
var trayIconLinuxDark []byte

//go:embed icon-linux-light.png
var trayIconLinuxLight []byte

//go:embed appicon.png
var appIcon []byte
