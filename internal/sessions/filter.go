// Package sessions implements the `lazyagent sessions` subcommand: list the
// sessions recorded for a directory (all agents) and reopen one.
package sessions

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// resolveSymlink returns the symlink-resolved, cleaned form of cleaned, and
// whether that differs from cleaned (i.e. there was actually something to
// resolve). Both targetVariants and matchesDir need this same clean+resolve
// step, just applied to different paths.
func resolveSymlink(cleaned string) (resolved string, ok bool) {
	r, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", false
	}
	r = filepath.Clean(r)
	if r == cleaned {
		return "", false
	}
	return r, true
}

// targetVariants normalizes dir for matching: the cleaned absolute path
// plus, when it differs, the symlink-resolved form — so /tmp also matches
// sessions recorded under /private/tmp on macOS.
func targetVariants(dir string) ([]string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	abs = filepath.Clean(abs)
	variants := []string{abs}
	if resolved, ok := resolveSymlink(abs); ok {
		variants = append(variants, resolved)
	}
	return variants, nil
}

// matchesVariants reports whether cwd equals a variant or lies beneath one
// (prefix on a path boundary: /foo/bar must not match /foo/barbaz).
func matchesVariants(cwd string, variants []string) bool {
	for _, v := range variants {
		if cwd == v || strings.HasPrefix(cwd, v+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// matchesDir reports whether a session's recorded cwd falls under one of the
// target variants. The spec matches on the cleaned, *unresolved* recorded
// CWD first: a session recorded under a symlinked subdirectory of the target
// (e.g. <target>/sub-link -> somewhere else entirely) must still match,
// because the recorded path itself is what lies under the target. Only when
// that fails do we fall back to resolving symlinks in cwd, which is what
// lets e.g. /tmp-recorded sessions match a /private/tmp target on macOS.
func matchesDir(cwd string, variants []string) bool {
	if cwd == "" {
		return false
	}
	cwd = filepath.Clean(cwd)
	if matchesVariants(cwd, variants) {
		return true
	}
	if resolved, ok := resolveSymlink(cwd); ok {
		return matchesVariants(resolved, variants)
	}
	return false
}

// bySessionRecency orders sessions by LastActivity descending, breaking ties
// by SessionID ascending. This is the single source of ordering truth
// shared by FilterByDir's sort and the picker's incremental stream merge
// (see mergeSessions in picker.go) — one comparator, so a session's rank
// relative to its neighbors never depends on which code path placed it.
func bySessionRecency(a, b *model.Session) int {
	if c := b.LastActivity.Compare(a.LastActivity); c != 0 {
		return c
	}
	return strings.Compare(a.SessionID, b.SessionID)
}

// filterBatch returns the sessions in batch whose CWD falls under one of
// variants (see targetVariants), excluding sidechains. It is FilterByDir's
// per-session filtering rule factored out so the picker's streaming path can
// apply the exact same rule to each arriving batch without recomputing
// variants (a symlink-resolving, syscall-touching step) on every call — see
// runPicker in picker.go.
func filterBatch(batch []*model.Session, variants []string) []*model.Session {
	var out []*model.Session
	for _, s := range batch {
		if s.IsSidechain {
			continue
		}
		if matchesDir(s.CWD, variants) {
			out = append(out, s)
		}
	}
	return out
}

// FilterByDir returns the sessions whose CWD is dir or a subdirectory of it,
// excluding sidechains, sorted by LastActivity descending.
func FilterByDir(sessions []*model.Session, dir string) ([]*model.Session, error) {
	variants, err := targetVariants(dir)
	if err != nil {
		return nil, err
	}
	out := filterBatch(sessions, variants)
	slices.SortStableFunc(out, bySessionRecency)
	return out, nil
}
