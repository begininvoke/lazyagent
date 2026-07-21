package core

// firstErr returns the first non-nil error among errs, or nil if all are
// nil. Used by providers' SaveCaches/LoadCaches to report a problem from
// whichever underlying cache file failed, while still attempting to save/
// load every one of them (a failure on the cwd-index, say, shouldn't skip
// saving the discovery cache).
func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// CachePersister is implemented by providers whose discovery caches can be
// persisted between runs. internal/sessions.Run wires it up explicitly
// around its own one-shot control flow; SessionManager.EnableCachePersistence
// (used by the TUI, GUI/tray, and API server) wires it up once for every
// long-lived surface. Both methods are best-effort: the cache is purely
// advisory, so a non-nil error must never fail discovery. Callers are
// expected to invoke these and ignore the returned error; it exists mainly
// so tests can assert on it directly.
type CachePersister interface {
	// LoadCaches best-effort loads previously persisted caches from dir
	// into the provider's in-memory caches.
	LoadCaches(dir string) error
	// SaveCaches best-effort persists the provider's current in-memory
	// caches to dir, creating dir (0700) if it doesn't exist.
	SaveCaches(dir string) error
}

// LoadProviderCaches calls LoadCaches(dir) on p if p implements
// CachePersister, recursing into a MultiProvider's members (so each member
// that implements the interface gets a chance, and non-implementers are
// silently skipped). The returned errors, if any, are discarded — loading a
// persisted cache is advisory and must never fail discovery; see
// CachePersister's doc comment.
func LoadProviderCaches(p SessionProvider, dir string) {
	if mp, ok := p.(MultiProvider); ok {
		for _, member := range mp.Providers {
			LoadProviderCaches(member, dir)
		}
		return
	}
	if cp, ok := p.(CachePersister); ok {
		_ = cp.LoadCaches(dir)
	}
}

// SaveProviderCaches is LoadProviderCaches's counterpart for SaveCaches.
func SaveProviderCaches(p SessionProvider, dir string) {
	if mp, ok := p.(MultiProvider); ok {
		for _, member := range mp.Providers {
			SaveProviderCaches(member, dir)
		}
		return
	}
	if cp, ok := p.(CachePersister); ok {
		_ = cp.SaveCaches(dir)
	}
}
