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

static void lazyagent_activatePid(int pid) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSRunningApplication *app =
			[NSRunningApplication runningApplicationWithProcessIdentifier:(pid_t)pid];
		if (app == nil) {
			return;
		}
		if (@available(macOS 14.0, *)) {
			// Cooperative activation: the frontmost app (us — the user just
			// clicked Resume) must yield, or activate is a no-op on 14+.
			[NSApp yieldActivationToApplication:app];
			[app activateWithOptions:0];
		} else {
			#pragma clang diagnostic push
			#pragma clang diagnostic ignored "-Wdeprecated-declarations"
			[app activateWithOptions:NSApplicationActivateIgnoringOtherApps];
			#pragma clang diagnostic pop
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

// activateProcess brings another application's windows to the foreground
// by pid. Unlike scripting System Events, NSRunningApplication needs no
// Automation/TCC permission. Used after spawning terminal windows whose
// process cannot activate itself (e.g. a second kitty instance).
func activateProcess(pid int) {
	C.lazyagent_activatePid(C.int(pid))
}
