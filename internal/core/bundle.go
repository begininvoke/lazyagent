package core

import "strings"

// InBundlePath reports whether exePath points inside a macOS .app bundle
// (…/<Name>.app/Contents/MacOS/…). The marker includes the slash after
// ".app", so names like "not.app.txt" cannot match. Callers should pass a
// symlink-resolved path: os.Executable() may return the invoking symlink.
func InBundlePath(exePath string) bool {
	return strings.Contains(exePath, ".app/Contents/MacOS/")
}
