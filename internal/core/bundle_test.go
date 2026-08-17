package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInBundlePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/Applications/Lazyagent.app/Contents/MacOS/lazyagent", true},
		{"/Users/x/Library/Lazyagent.app/Contents/MacOS/lazyagent", true},
		{"/opt/homebrew/bin/lazyagent-cli", false},
		{"/Users/x/dev/lazyagent/lazyagent", false},
		{"", false},
		// ".app" must be the bundle directory, not a substring elsewhere.
		{"/tmp/not.app.txt/Contents/MacOS/x", false},
	}
	for _, c := range cases {
		if got := InBundlePath(c.path); got != c.want {
			t.Errorf("InBundlePath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestEnsureCLISymlink_Creates(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	target := filepath.Join(dir, "lazyagent")
	if err := os.WriteFile(target, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCLISymlink(binDir, target); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(filepath.Join(binDir, "lazyagent-cli"))
	if err != nil {
		t.Fatal(err)
	}
	if got != target {
		t.Errorf("link points to %q, want %q", got, target)
	}
}

func TestEnsureCLISymlink_RefreshesStale(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(binDir, "lazyagent-cli")
	if err := os.Symlink(filepath.Join(dir, "old-target"), link); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "lazyagent")
	if err := os.WriteFile(target, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCLISymlink(binDir, target); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(link); got != target {
		t.Errorf("stale link not refreshed: points to %q", got)
	}
}

func TestEnsureCLISymlink_NoClobber(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(binDir, "lazyagent-cli")
	if err := os.WriteFile(link, []byte("user script"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCLISymlink(binDir, filepath.Join(dir, "lazyagent")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "user script" {
		t.Errorf("user file was clobbered")
	}
}

func TestEnsureCLISymlink_IdempotentWhenCorrect(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	target := filepath.Join(dir, "lazyagent")
	if err := os.WriteFile(target, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCLISymlink(binDir, target); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCLISymlink(binDir, target); err != nil {
		t.Errorf("second call errored: %v", err)
	}
}
