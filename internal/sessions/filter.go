// Package sessions implements the `lazyagent sessions` subcommand: list the
// sessions recorded for a directory (all agents) and reopen one.
package sessions

import (
	"slices"
	"strings"

	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// targetVariants normalizes dir for matching: the cleaned absolute path
// plus, when it differs, the symlink-resolved form — so /tmp also matches
// sessions recorded under /private/tmp on macOS. Thin wrapper delegating to
// core.DirMatchVariants, the single shared implementation of this path
// arithmetic (see task 15: the API's GET /api/sessions?dir= filter uses the
// same function directly).
func targetVariants(dir string) ([]string, error) {
	return core.DirMatchVariants(dir)
}

// matchesDir reports whether a session's recorded cwd falls under one of the
// target variants. Thin wrapper delegating to core.CWDMatchesDir — see that
// function's doc comment for the matching semantics (unresolved-then-
// resolved cwd matching against target variants).
func matchesDir(cwd string, variants []string) bool {
	return core.CWDMatchesDir(cwd, variants)
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
