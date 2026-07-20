// Package sessions implements the `lazyagent sessions` subcommand: list the
// sessions recorded for a directory (all agents) and reopen one.
package sessions

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/illegalstudio/lazyagent/internal/model"
)

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
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		if resolved = filepath.Clean(resolved); resolved != abs {
			variants = append(variants, resolved)
		}
	}
	return variants, nil
}

// matchesDir reports whether cwd equals a target variant or lies beneath one
// (prefix on a path boundary: /foo/bar must not match /foo/barbaz).
func matchesDir(cwd string, variants []string) bool {
	if cwd == "" {
		return false
	}
	cwd = filepath.Clean(cwd)
	// Resolve symlinks in CWD for accurate matching, especially on macOS where
	// /tmp may be a symlink to /private/tmp
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		if resolved = filepath.Clean(resolved); resolved != cwd {
			cwd = resolved
		}
	}
	for _, v := range variants {
		if cwd == v || strings.HasPrefix(cwd, v+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// FilterByDir returns the sessions whose CWD is dir or a subdirectory of it,
// excluding sidechains, sorted by LastActivity descending.
func FilterByDir(sessions []*model.Session, dir string) ([]*model.Session, error) {
	variants, err := targetVariants(dir)
	if err != nil {
		return nil, err
	}
	var out []*model.Session
	for _, s := range sessions {
		if s.IsSidechain {
			continue
		}
		if matchesDir(s.CWD, variants) {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b *model.Session) int {
		return b.LastActivity.Compare(a.LastActivity)
	})
	return out, nil
}
