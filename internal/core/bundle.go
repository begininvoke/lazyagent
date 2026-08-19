package core

import (
	"os"
	"path/filepath"
	"strings"
)

// InBundlePath reports whether exePath points inside a macOS .app bundle
// (…/<Name>.app/Contents/MacOS/…). The marker includes the slash after
// ".app", so names like "not.app.txt" cannot match. Callers should pass a
// symlink-resolved path: os.Executable() may return the invoking symlink.
func InBundlePath(exePath string) bool {
	return strings.Contains(exePath, ".app/Contents/MacOS/")
}

// EnsureCLISymlink idempotently maintains binDir/lazyagent as a symlink
// to target. It creates binDir when missing and refreshes a symlink that
// is stale or broken, but never replaces a non-symlink: a user's own
// file at that path wins.
func EnsureCLISymlink(binDir, target string) error {
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	link := filepath.Join(binDir, "lazyagent")
	if fi, err := os.Lstat(link); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			return nil // not ours — never clobber
		}
		if dst, err := os.Readlink(link); err == nil && dst == target {
			return nil // already correct
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	}
	return os.Symlink(target, link)
}
