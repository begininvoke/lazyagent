package core

import (
	"path/filepath"
	"strings"
)

// resolveSymlink returns the symlink-resolved, cleaned form of cleaned, and
// whether that differs from cleaned (i.e. there was actually something to
// resolve). Both DirMatchVariants and CWDMatchesDir need this same
// clean+resolve step, just applied to different paths. A failure to resolve
// (e.g. cleaned doesn't exist on disk) is not an error here: it just means
// there is no additional variant to add, so callers fall back to the
// cleaned path alone.
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

// DirMatchVariants normalizes dir for matching: the cleaned absolute path
// plus, when it differs, the symlink-resolved form — so /tmp also matches
// sessions recorded under /private/tmp on macOS.
//
// dir need not exist on disk: this is pure path arithmetic
// (filepath.Abs + filepath.Clean) plus a best-effort symlink resolution
// that is silently skipped when filepath.EvalSymlinks fails (nonexistent
// path, permission error, etc.) — callers that require dir to exist enforce
// that themselves before calling this (see sessions.Run's os.Stat check).
func DirMatchVariants(dir string) ([]string, error) {
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

// CWDMatchesDir reports whether a session's recorded cwd falls under one of
// the target variants (see DirMatchVariants). The spec matches on the
// cleaned, *unresolved* recorded CWD first: a session recorded under a
// symlinked subdirectory of the target (e.g. <target>/sub-link -> somewhere
// else entirely) must still match, because the recorded path itself is
// what lies under the target. Only when that fails do we fall back to
// resolving symlinks in cwd, which is what lets e.g. /tmp-recorded sessions
// match a /private/tmp target on macOS.
func CWDMatchesDir(cwd string, variants []string) bool {
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
