package sessions

import (
	"os"
	"path/filepath"
)

// userCacheDir is os.UserCacheDir, stubbable in tests so the "UserCacheDir
// fails" path (run without persistence) can be exercised without altering
// the real environment's cache directory.
var userCacheDir = os.UserCacheDir

// ResolveCacheDir returns the directory lazyagent should persist its
// discovery caches under (os.UserCacheDir()/lazyagent, e.g.
// ~/Library/Caches/lazyagent on macOS) and whether persistence is available
// at all. When UserCacheDir fails or returns an empty string, ok is false
// and the caller must run without persistence -- discovery itself is never
// affected, only whether its caches get saved/reused across runs.
//
// Exported so the long-lived surfaces (TUI, GUI/tray, API server) resolve
// the cache directory the exact same way the `sessions` subcommand does,
// without duplicating the logic -- see core.SessionManager.
// EnableCachePersistence, which they pass this to.
func ResolveCacheDir() (dir string, ok bool) {
	base, err := userCacheDir()
	if err != nil || base == "" {
		return "", false
	}
	return filepath.Join(base, "lazyagent"), true
}
