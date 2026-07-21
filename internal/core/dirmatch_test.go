package core

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDirMatchDecisionTable is the shared-helper decision-equivalence table
// (see task 15): every case here documents a match decision that
// sessions.FilterByDir (via its targetVariants/matchesDir wrappers, which
// delegate to DirMatchVariants/CWDMatchesDir) and the API's dir filter (via
// these same functions called directly) must agree on, because both paths
// run this exact code.
func TestDirMatchDecisionTable(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		dir  string
		cwd  string
		want bool
	}{
		{"exact match", base, base, true},
		{"subdirectory", base, sub, true},
		{"false prefix sibling excluded", base, base + "extra", false},
		{"unrelated path excluded", base, "/somewhere/else", false},
		{"empty cwd excluded", base, "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			variants, err := DirMatchVariants(c.dir)
			if err != nil {
				t.Fatalf("DirMatchVariants(%q): %v", c.dir, err)
			}
			if got := CWDMatchesDir(c.cwd, variants); got != c.want {
				t.Errorf("CWDMatchesDir(%q, variants of %q) = %v, want %v", c.cwd, c.dir, got, c.want)
			}
		})
	}
}

// TestDirMatchVariantsSymlinkedTarget: matching a symlinked target dir must
// resolve it, so e.g. /tmp also matches sessions recorded under
// /private/tmp on macOS.
func TestDirMatchVariantsSymlinkedTarget(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	variants, err := DirMatchVariants(link)
	if err != nil {
		t.Fatal(err)
	}
	if !CWDMatchesDir(real, variants) {
		t.Fatal("symlinked target should match the resolved cwd")
	}
}

// TestDirMatchSessionUnderSymlinkedSubdir: a session recorded under a
// symlinked subdirectory of the target (which resolves to somewhere else
// entirely) must still match, because the recorded path itself lies under
// the target on a string/path-boundary basis.
func TestDirMatchSessionUnderSymlinkedSubdir(t *testing.T) {
	target := t.TempDir()
	external := t.TempDir()
	subLink := filepath.Join(target, "sub-link")
	if err := os.Symlink(external, subLink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	variants, err := DirMatchVariants(target)
	if err != nil {
		t.Fatal(err)
	}
	if !CWDMatchesDir(subLink, variants) {
		t.Fatal("session recorded under a symlinked subdir of the target must match")
	}
}

// TestDirMatchVariantsNonexistentDir documents the API's relaxation vs. the
// CLI: DirMatchVariants itself never requires dir to exist on disk (it's
// pure path arithmetic — filepath.Abs + filepath.Clean — with a best-effort
// symlink resolution that's silently skipped when EvalSymlinks fails, e.g.
// because the path doesn't exist). The CLI's existence requirement is
// enforced by sessions.Run's own os.Stat check, a layer above this
// function, so it is not exercised here.
func TestDirMatchVariantsNonexistentDir(t *testing.T) {
	base := t.TempDir()
	nonexistent := filepath.Join(base, "does-not-exist")
	variants, err := DirMatchVariants(nonexistent)
	if err != nil {
		t.Fatalf("DirMatchVariants on a nonexistent dir must not error: %v", err)
	}
	if !CWDMatchesDir(nonexistent, variants) {
		t.Fatal("a session recorded under the nonexistent-but-cleaned path must still match")
	}
}

// TestDirMatchVariantsRelativeDir: a relative dir resolves against the
// process's current working directory, same as filepath.Abs. The API
// handler rejects non-absolute dir values before ever calling this
// function; this test documents that the function itself still supports
// relative input, which the CLI (--dir) relies on.
func TestDirMatchVariantsRelativeDir(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(base)

	variants, err := DirMatchVariants(".")
	if err != nil {
		t.Fatal(err)
	}
	if !CWDMatchesDir(sub, variants) {
		t.Fatal("relative dir '.' should resolve against the process cwd, matching its subdirectories")
	}
}
