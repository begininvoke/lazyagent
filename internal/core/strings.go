package core

// TruncateRunes returns s unchanged when it contains at most max runes;
// otherwise the first max runes with "…" appended. Rune-safe: never
// splits a multibyte character.
func TruncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
