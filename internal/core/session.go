package core

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// Window minutes bounds.
const (
	MinWindowMinutes = 10
	MaxWindowMinutes = 480
)

// cacheSaveInterval is the minimum time between persisted-discovery-cache
// saves that a dirty (but not first) Reload() triggers. Long-lived surfaces
// (TUI/GUI/API) reload on every watcher event, and each save rewrites a few
// MB of JSON, so successful reloads after the first just mark the cache
// dirty; an actual save happens at most this often. See
// SessionManager.EnableCachePersistence.
const cacheSaveInterval = 5 * time.Minute

// SessionProvider abstracts how sessions are discovered.
type SessionProvider interface {
	// DiscoverSessions returns all available sessions.
	DiscoverSessions() ([]*model.Session, error)
	// UseWatcher returns whether a file system watcher should be started.
	UseWatcher() bool
	// RefreshInterval returns how often UpdateActivities should re-discover sessions,
	// or 0 to never re-discover (only on explicit Reload or watcher events).
	RefreshInterval() time.Duration
	// WatchDirs returns directories to watch for file system changes.
	WatchDirs() []string
}

// SessionDetailView is the full struct for a detail panel.
type SessionDetailView struct {
	model.Session
	Activity ActivityKind
}

// SessionManager manages session discovery, file watching, and activity tracking.
type SessionManager struct {
	mu       sync.RWMutex
	sessions []*model.Session
	tracker  *ActivityTracker
	watcher  *ProjectWatcher
	provider SessionProvider
	names    *SessionNames

	windowMinutes        int
	activityFilter       ActivityKind
	searchQuery          string
	lastPollDiscover     time.Time
	excludeCWDSubstrings []string

	// Persisted discovery cache (opt-in, see EnableCachePersistence). All
	// guarded by mu like everything else above; the actual Load/Save disk
	// I/O always happens outside the lock (see loadPersistedCacheOnce /
	// savePersistedCacheIfDue / Close) so a slow write never blocks readers
	// like Sessions()/VisibleSessions().
	persistEnabled  bool
	persistDir      string
	persistLoaded   bool      // Load has been attempted (first Reload only)
	persistDirty    bool      // a successful Reload happened since the last save
	lastPersistSave time.Time // zero until the first save
	persistNow      func() time.Time
}

// NewSessionManager creates a new SessionManager with the given provider.
func NewSessionManager(windowMinutes int, provider SessionProvider) *SessionManager {
	return &SessionManager{
		tracker:       NewActivityTracker(),
		windowMinutes: windowMinutes,
		provider:      provider,
		names:         NewSessionNames(),
		persistNow:    time.Now,
	}
}

// EnableCachePersistence opts the manager into persisting the provider's
// discovery caches under dir between process runs, via
// core.CachePersister / LoadProviderCaches / SaveProviderCaches (the same
// mechanism the `sessions` subcommand already uses). Call it before the
// first Reload(). Once enabled:
//   - the FIRST Reload() loads any previously persisted caches before
//     discovering, and never loads again on later reloads;
//   - after the first successful discovery, the caches are saved once;
//   - later successful reloads mark the cache dirty; an actual save then
//     happens at most once per cacheSaveInterval;
//   - Close() performs a final save if the cache is still dirty at shutdown.
//
// Persistence is purely advisory: providers that don't implement
// CachePersister (e.g. demo.Provider) make every Load/Save call a silent
// no-op via the LoadProviderCaches/SaveProviderCaches helpers, and a load or
// save failure never affects discovery.
func (m *SessionManager) EnableCachePersistence(dir string) {
	m.mu.Lock()
	m.persistEnabled = true
	m.persistDir = dir
	m.mu.Unlock()
}

// loadPersistedCacheOnce loads the provider's persisted discovery caches on
// the first call after EnableCachePersistence, and is a no-op on every call
// after that (or when persistence was never enabled). Must run before the
// provider's first DiscoverSessions call so that discovery reuses the prior
// run's cache. The actual disk I/O happens without m.mu held.
func (m *SessionManager) loadPersistedCacheOnce() {
	m.mu.Lock()
	if !m.persistEnabled || m.persistLoaded {
		m.mu.Unlock()
		return
	}
	m.persistLoaded = true
	dir := m.persistDir
	m.mu.Unlock()

	LoadProviderCaches(m.provider, dir)
}

// savePersistedCacheIfDue runs after a successful Reload when persistence is
// enabled. The very first save (lastPersistSave still zero) always happens,
// so a persisted cache exists as early as possible; subsequent saves happen
// at most once per cacheSaveInterval, with reloads in between just marking
// the cache dirty for Close's final save. The actual disk I/O happens
// without m.mu held.
func (m *SessionManager) savePersistedCacheIfDue() {
	m.mu.Lock()
	if !m.persistEnabled {
		m.mu.Unlock()
		return
	}
	now := m.persistNow()
	due := m.lastPersistSave.IsZero() || now.Sub(m.lastPersistSave) >= cacheSaveInterval
	if !due {
		m.persistDirty = true
		m.mu.Unlock()
		return
	}
	dir := m.persistDir
	m.lastPersistSave = now
	m.persistDirty = false
	m.mu.Unlock()

	SaveProviderCaches(m.provider, dir)
}

// SetEventBus attaches an event bus so activity transitions are published
// to subscribers. Pass nil to detach.
func (m *SessionManager) SetEventBus(bus *EventBus) {
	m.mu.Lock()
	m.tracker.SetEventBus(bus)
	m.mu.Unlock()
}

// SetExcludeCWDSubstrings sets the CWD substring patterns used to exclude
// sessions from filtered views (VisibleSessions / QuerySessions).
func (m *SessionManager) SetExcludeCWDSubstrings(patterns []string) {
	m.mu.Lock()
	m.excludeCWDSubstrings = slices.Clone(patterns)
	m.mu.Unlock()
}

// SessionName returns the custom name for a session, or empty string.
func (m *SessionManager) SessionName(sessionID string) string {
	return m.names.Get(sessionID)
}

// SetSessionName stores a custom name for a session.
func (m *SessionManager) SetSessionName(sessionID, name string) error {
	return m.names.Set(sessionID, name)
}

// StartWatcher starts the file system watcher if the provider supports it.
func (m *SessionManager) StartWatcher() error {
	if !m.provider.UseWatcher() {
		return nil
	}
	w, err := NewProjectWatcher(m.provider.WatchDirs()...)
	if err != nil {
		return err
	}
	m.watcher = w
	return nil
}

// StopWatcher stops the file system watcher.
func (m *SessionManager) StopWatcher() {
	if m.watcher != nil {
		m.watcher.Close()
	}
}

// Close is the shutdown hook the long-lived surfaces (TUI, GUI/tray, API)
// should call in place of StopWatcher: it stops the file watcher and, if
// cache persistence is enabled and a Reload happened since the last save
// (the cache is dirty — see savePersistedCacheIfDue), performs one final
// save so shutdown never loses more than the most recent in-flight reload.
// A no-op final save (persistence disabled, or already clean) costs nothing
// beyond the StopWatcher call it always makes.
func (m *SessionManager) Close() {
	m.StopWatcher()

	m.mu.Lock()
	due := m.persistEnabled && m.persistDirty
	dir := m.persistDir
	m.persistDirty = false
	m.mu.Unlock()

	if due {
		SaveProviderCaches(m.provider, dir)
	}
}

// WatcherEvents returns the channel for file change notifications, or nil.
func (m *SessionManager) WatcherEvents() <-chan struct{} {
	if m.watcher != nil {
		return m.watcher.Events
	}
	return nil
}

// Reload discovers sessions via the provider and updates activity states.
// When cache persistence is enabled (EnableCachePersistence), this is also
// where the persisted cache gets loaded (once, before the first discovery)
// and saved (see loadPersistedCacheOnce / savePersistedCacheIfDue).
//
// When exclude_cwd_substrings patterns are configured, discovery itself is
// scoped via DiscoverMatching(m.provider, excludeCWDPredicate(patterns)):
// dir-scoped providers (DirScopedProvider) skip parsing excluded sessions
// entirely instead of discovering-then-hiding them at the view layer. With
// no patterns set, Reload calls the provider's plain DiscoverSessions(),
// exactly as before this pushdown existed — zero behavior change for the
// common case. Either way, the view-level filter (filterSessionsLocked)
// still re-applies the same exclusion: DiscoverMatching's contract leaves
// non-dir-scoped providers' results unfiltered, so the view filter remains
// the only thing guaranteeing exclusion for those.
//
// Runtime pattern changes: SetExcludeCWDSubstrings can be called at any
// time, and Reload re-reads m.excludeCWDSubstrings fresh on every call, so
// the very next Reload() automatically discovers under the new patterns —
// no extra plumbing is needed. (As of this change, no caller actually
// changes patterns after startup; see the report for that investigation.)
func (m *SessionManager) Reload() error {
	m.loadPersistedCacheOnce()

	m.mu.RLock()
	patterns := m.excludeCWDSubstrings
	m.mu.RUnlock()

	var sessions []*model.Session
	var err error
	if len(patterns) == 0 {
		sessions, err = m.provider.DiscoverSessions()
	} else {
		sessions, err = DiscoverMatching(m.provider, excludeCWDPredicate(patterns))
	}
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.sessions = sessions
	m.tracker.Update(sessions, time.Now())
	SortSessions(m.sessions)
	m.lastPollDiscover = time.Now()
	m.mu.Unlock()

	m.savePersistedCacheIfDue()
	return nil
}

// UpdateActivities refreshes activity states without reloading from disk.
// Returns true if any activity state changed.
// If the provider specifies a RefreshInterval, sessions are re-discovered periodically.
// Also refreshes session names from disk if modified externally.
func (m *SessionManager) UpdateActivities() bool {
	namesChanged := m.names.Refresh()
	if interval := m.provider.RefreshInterval(); interval > 0 {
		now := time.Now()
		m.mu.RLock()
		needsRefresh := len(m.sessions) == 0 || now.Sub(m.lastPollDiscover) > interval
		m.mu.RUnlock()
		if needsRefresh {
			sessions, err := m.provider.DiscoverSessions()
			if err == nil {
				m.mu.Lock()
				m.sessions = sessions
				m.tracker.Update(sessions, time.Now())
				SortSessions(m.sessions)
				m.lastPollDiscover = time.Now()
				m.mu.Unlock()
				return true
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old := make(map[string]ActivityKind, len(m.sessions))
	for _, s := range m.sessions {
		old[s.SessionID] = m.tracker.Get(s.SessionID)
	}
	m.tracker.Update(m.sessions, time.Now())
	for _, s := range m.sessions {
		if m.tracker.Get(s.SessionID) != old[s.SessionID] {
			return true
		}
	}
	return namesChanged
}

// SetWindowMinutes sets the time window filter, clamped to [MinWindowMinutes, MaxWindowMinutes].
func (m *SessionManager) SetWindowMinutes(minutes int) {
	m.mu.Lock()
	m.windowMinutes = Clamp(MinWindowMinutes, MaxWindowMinutes, minutes)
	m.mu.Unlock()
}

// WindowMinutes returns the current time window.
func (m *SessionManager) WindowMinutes() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.windowMinutes
}

// ActivityFilter returns the current activity filter.
func (m *SessionManager) ActivityFilter() ActivityKind {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activityFilter
}

// SetActivityFilter sets the activity filter.
func (m *SessionManager) SetActivityFilter(filter ActivityKind) {
	m.mu.Lock()
	m.activityFilter = filter
	m.mu.Unlock()
}

// SetSearchQuery sets the search query.
func (m *SessionManager) SetSearchQuery(query string) {
	m.mu.Lock()
	m.searchQuery = query
	m.mu.Unlock()
}

// ActivityFor returns the current activity for a session.
func (m *SessionManager) ActivityFor(sessionID string) ActivityKind {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tracker.Get(sessionID)
}

// Sessions returns all raw sessions (unfiltered).
func (m *SessionManager) Sessions() []*model.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions
}

// VisibleSessions returns sessions filtered by the manager's internal state
// (time window, activity filter, search query). Used by TUI and tray.
func (m *SessionManager) VisibleSessions() []*model.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.filterSessionsLocked(m.searchQuery, m.activityFilter)
}

// QuerySessions returns sessions filtered by explicit parameters without using
// the manager's internal filter/search state. Safe for concurrent API use.
// Empty search/filter means no filtering.
func (m *SessionManager) QuerySessions(search string, filter ActivityKind) []*model.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.filterSessionsLocked(search, filter)
}

// excludedByCWD reports whether cwd matches any of patterns, using
// case-sensitive substring containment (strings.Contains). An empty-string
// pattern never matches anything (otherwise it would match every CWD).
//
// This is the single implementation of exclude_cwd_substrings matching
// semantics: both the view-level filter (filterSessionsLocked) and the
// discovery-time pushdown predicate (excludeCWDPredicate, used by Reload)
// call it, so the two layers can never disagree about what's excluded.
func excludedByCWD(cwd string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(cwd, pattern) {
			return true
		}
	}
	return false
}

// excludeCWDPredicate returns a DiscoverMatching cwdMatch predicate — it
// returns true to KEEP a cwd — that keeps exactly the CWDs excludedByCWD
// would not exclude. Because it's built directly from excludedByCWD, it can
// only ever match what the view-level filter would keep: a dir-scoped
// provider can never be made to skip, via this predicate, a session the
// view filter would have shown.
func excludeCWDPredicate(patterns []string) func(cwd string) bool {
	return func(cwd string) bool {
		return !excludedByCWD(cwd, patterns)
	}
}

// filterSessionsLocked applies time window, activity, and search filters.
// Must be called with m.mu held (at least RLock).
func (m *SessionManager) filterSessionsLocked(search string, filter ActivityKind) []*model.Session {
	cutoff := time.Now().Add(-time.Duration(m.windowMinutes) * time.Minute)
	lowerQuery := strings.ToLower(search)
	var visible []*model.Session
	for _, s := range m.sessions {
		// CWD-based exclusion: skip sessions whose CWD contains any excluded
		// substring. Defense in depth: Reload already pushes this same
		// exclusion into discovery for dir-scoped providers (see
		// excludeCWDPredicate), but this check must stay here too since
		// DiscoverMatching leaves non-dir-scoped providers' results
		// unfiltered.
		if excludedByCWD(s.CWD, m.excludeCWDSubstrings) {
			continue
		}
		if s.IsSidechain || !s.LastActivity.After(cutoff) {
			continue
		}
		// Grok and Kimi both create a session directory the moment the CLI is
		// launched, before any message is sent. Hide such never-used sessions
		// from the list. They are still returned by the provider, so `prune`
		// and `compact` can reach very old empty sessions.
		if (s.Agent == "grok" || s.Agent == "kimi") && s.UserMessages == 0 {
			continue
		}
		if filter != "" && m.tracker.Get(s.SessionID) != filter {
			continue
		}
		if lowerQuery != "" {
			matchCWD := strings.Contains(strings.ToLower(s.CWD), lowerQuery)
			matchCustom := strings.Contains(strings.ToLower(m.names.Get(s.SessionID)), lowerQuery)
			matchAgent := strings.Contains(strings.ToLower(s.Name), lowerQuery)
			if !matchCWD && !matchCustom && !matchAgent {
				continue
			}
		}
		visible = append(visible, s)
	}
	return visible
}

// SessionDetail returns the full detail view for a session.
func (m *SessionManager) SessionDetail(id string) *SessionDetailView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if s.SessionID == id {
			return &SessionDetailView{
				Session:  *s,
				Activity: m.tracker.Get(id),
			}
		}
	}
	return nil
}

// SortSessions sorts sessions by last activity (most recent first).
func SortSessions(sessions []*model.Session) {
	slices.SortFunc(sessions, func(a, b *model.Session) int {
		return cmp.Compare(b.LastActivity.UnixNano(), a.LastActivity.UnixNano())
	})
}
