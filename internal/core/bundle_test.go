package core

import "testing"

func TestInBundlePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/Applications/Lazyagent.app/Contents/MacOS/lazyagent", true},
		{"/Users/x/Library/Lazyagent.app/Contents/MacOS/lazyagent", true},
		{"/opt/homebrew/bin/lazyagent", false},
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
