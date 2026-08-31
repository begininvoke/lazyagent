//go:build !notray

package tray

import (
	"strings"
)

// quotedJoin shell-quotes every argv element and joins them with spaces.
func quotedJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = shellQuote(a)
	}
	return strings.Join(parts, " ")
}
