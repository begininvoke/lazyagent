//go:build !notray && linux

package tray

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestColorSchemePrefersDark(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   any
		dark bool
		ok   bool
	}{
		{name: "prefer dark", in: uint32(1), dark: true, ok: true},
		{name: "prefer light", in: uint32(2), dark: false, ok: true},
		{name: "no preference", in: uint32(0), ok: false},
		{name: "nested variant dark", in: dbus.MakeVariant(dbus.MakeVariant(uint32(1))), dark: true, ok: true},
		{name: "nested variant light", in: dbus.MakeVariant(uint32(2)), dark: false, ok: true},
		{name: "uint64 dark", in: uint64(1), dark: true, ok: true},
		{name: "garbage", in: "dark", ok: false},
		{name: "nil", in: nil, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dark, ok := colorSchemePrefersDark(tt.in)
			if ok != tt.ok || dark != tt.dark {
				t.Fatalf("colorSchemePrefersDark(%v) = (%v, %v), want (%v, %v)", tt.in, dark, ok, tt.dark, tt.ok)
			}
		})
	}
}

func TestLinuxTrayIconsEmbedded(t *testing.T) {
	t.Parallel()
	if len(trayIconLinuxDark) < 100 {
		t.Fatal("icon-linux-dark.png not embedded")
	}
	if len(trayIconLinuxLight) < 100 {
		t.Fatal("icon-linux-light.png not embedded")
	}
}
