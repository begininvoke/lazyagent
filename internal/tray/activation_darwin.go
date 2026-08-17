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
