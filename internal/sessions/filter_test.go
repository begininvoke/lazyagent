package sessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func sess(cwd string, last time.Time, sidechain bool) *model.Session {
	return &model.Session{SessionID: cwd, CWD: cwd, LastActivity: last, IsSidechain: sidechain}
}

func TestFilterByDirMatching(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	in := []*model.Session{
		sess(base, now, false),                // exact match
		sess(sub, now.Add(-time.Hour), false), // subdirectory
		sess(base+"extra", now, false),        // false prefix — excluded
		sess("/somewhere/else", now, false),   // unrelated — excluded
		sess(base, now, true),                 // sidechain — excluded
	}
	got, err := FilterByDir(in, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(got))
	}
	if got[0].CWD != base || got[1].CWD != sub {
		t.Errorf("wrong contents/order: %s, %s", got[0].CWD, got[1].CWD)
	}
}

func TestFilterByDirSymlinkedTarget(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	got, err := FilterByDir([]*model.Session{sess(real, time.Now(), false)}, link)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("symlinked target should match resolved CWD, got %d matches", len(got))
	}
}

func TestFilterByDirSortsByLastActivityDesc(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	in := []*model.Session{
		sess(base, now.Add(-2*time.Hour), false),
		sess(base, now, false),
		sess(base, now.Add(-time.Hour), false),
	}
	got, err := FilterByDir(in, base)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].LastActivity.Equal(now) {
		t.Errorf("most recent must be first, got %v", got[0].LastActivity)
	}
	if !got[2].LastActivity.Equal(now.Add(-2 * time.Hour)) {
		t.Errorf("oldest must be last, got %v", got[2].LastActivity)
	}
}

// sessID builds a session with an explicit SessionID distinct from its CWD,
// for tiebreak assertions where two sessions share a LastActivity.
func sessID(id, cwd string, last time.Time) *model.Session {
	return &model.Session{SessionID: id, CWD: cwd, LastActivity: last}
}

func TestFilterByDirTiebreaksEqualLastActivityBySessionID(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	in := []*model.Session{
		sessID("zzz", base, now),
		sessID("aaa", base, now),
	}
	got, err := FilterByDir(in, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(got))
	}
	if got[0].SessionID != "aaa" || got[1].SessionID != "zzz" {
		t.Errorf("equal-timestamp tiebreak must be ascending SessionID, got %s, %s", got[0].SessionID, got[1].SessionID)
	}
}

func TestFilterByDirSessionUnderSymlinkedSubdir(t *testing.T) {
	target := t.TempDir()
	external := t.TempDir() // outside target
	subLink := filepath.Join(target, "sub-link")
	if err := os.Symlink(external, subLink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	got, err := FilterByDir([]*model.Session{sess(subLink, time.Now(), false)}, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("session recorded under a symlinked subdir of the target must match, got %d matches", len(got))
	}
}
