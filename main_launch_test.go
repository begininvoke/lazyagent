package main

import "testing"

func TestShouldRunDirectGUI(t *testing.T) {
	tests := []struct {
		name          string
		inBundle      bool
		trayAvailable bool
		stdinTTY      bool
		appImage      bool
		modes         launchModes
		want          bool
	}{
		{name: "Finder bundle launch", inBundle: true, trayAvailable: true, want: true},
		{name: "bundle command from terminal", inBundle: true, trayAvailable: true, stdinTTY: true},
		{name: "AppImage gui", appImage: true, modes: launchModes{gui: true}, want: true},
		{name: "AppImage deprecated tray flag", appImage: true, modes: launchModes{tray: true}, want: true},
		{name: "AppImage gui plus api keeps parent", appImage: true, modes: launchModes{gui: true, api: true}},
		{name: "native Linux gui detaches", modes: launchModes{gui: true}},
		{name: "bundle without tray support", inBundle: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRunDirectGUI(tt.inBundle, tt.trayAvailable, tt.stdinTTY, tt.appImage, tt.modes)
			if got != tt.want {
				t.Fatalf("shouldRunDirectGUI() = %v, want %v", got, tt.want)
			}
		})
	}
}
