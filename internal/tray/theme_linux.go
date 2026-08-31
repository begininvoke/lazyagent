//go:build !notray && linux

package tray

import (
	"github.com/godbus/dbus/v5"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	portalDesktopDest = "org.freedesktop.portal.Desktop"
	portalDesktopPath = "/org/freedesktop/portal/desktop"
	portalSettings    = "org.freedesktop.portal.Settings"
	appearanceNS      = "org.freedesktop.appearance"
	colorSchemeKey    = "color-scheme"
)

// portalPrefersDark reports the xdg-desktop-portal color-scheme, if set.
// 1 = prefer dark, 2 = prefer light, 0 = no preference.
func portalPrefersDark() (dark bool, ok bool) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false, false
	}
	obj := conn.Object(portalDesktopDest, portalDesktopPath)

	var value dbus.Variant
	if err := obj.Call(portalSettings+".ReadOne", 0, appearanceNS, colorSchemeKey).Store(&value); err != nil {
		if err := obj.Call(portalSettings+".Read", 0, appearanceNS, colorSchemeKey).Store(&value); err != nil {
			return false, false
		}
	}
	return colorSchemePrefersDark(value)
}

func watchLinuxColorScheme(tray *application.SystemTray) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	defer conn.Close()

	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface(portalSettings),
		dbus.WithMatchMember("SettingChanged"),
	); err != nil {
		return
	}

	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)
	for sig := range signals {
		if len(sig.Body) < 3 {
			continue
		}
		ns, _ := sig.Body[0].(string)
		key, _ := sig.Body[1].(string)
		if ns != appearanceNS || key != colorSchemeKey {
			continue
		}
		if dark, ok := colorSchemePrefersDark(sig.Body[2]); ok {
			applyLinuxTrayIcon(tray, dark)
		}
	}
}

// colorSchemePrefersDark interprets an xdg-desktop-portal color-scheme value.
func colorSchemePrefersDark(v any) (dark bool, ok bool) {
	switch n := unwrapPortalValue(v).(type) {
	case uint32:
		switch n {
		case 1:
			return true, true
		case 2:
			return false, true
		default:
			return false, false
		}
	case uint64:
		return colorSchemePrefersDark(uint32(n))
	case int32:
		if n < 0 {
			return false, false
		}
		return colorSchemePrefersDark(uint32(n))
	default:
		return false, false
	}
}

func unwrapPortalValue(v any) any {
	for range 4 {
		variant, ok := v.(dbus.Variant)
		if !ok {
			return v
		}
		v = variant.Value()
	}
	return v
}
