package core

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateRunes(t *testing.T) {
	if got := TruncateRunes("hello", 140); got != "hello" {
		t.Errorf("short string changed: %q", got)
	}
	if got := TruncateRunes("hello", 5); got != "hello" {
		t.Errorf("exact-length string changed: %q", got)
	}
	long := strings.Repeat("x", 150)
	got := TruncateRunes(long, 140)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("missing ellipsis: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 141 {
		t.Errorf("rune count = %d, want 141", n)
	}
	// Multibyte runes must not be split.
	accented := strings.Repeat("à", 150)
	got = TruncateRunes(accented, 140)
	if !utf8.ValidString(got) {
		t.Errorf("invalid UTF-8 after truncation")
	}
	if n := utf8.RuneCountInString(got); n != 141 {
		t.Errorf("accented rune count = %d, want 141", n)
	}
}
