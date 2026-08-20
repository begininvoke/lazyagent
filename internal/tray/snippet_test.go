//go:build !notray

package tray

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestLastMessageSnippet(t *testing.T) {
	if got := lastMessageSnippet(&model.Session{}); got != "" {
		t.Errorf("empty session: got %q, want \"\"", got)
	}

	sess := &model.Session{RecentMessages: []model.ConversationMessage{
		{Role: "user", Text: "first"},
		{Role: "assistant", Text: "  line one\n\n\tline two  "},
	}}
	if got := lastMessageSnippet(sess); got != "line one line two" {
		t.Errorf("whitespace collapse: got %q", got)
	}

	long := &model.Session{RecentMessages: []model.ConversationMessage{
		{Role: "assistant", Text: strings.Repeat("y", 300)},
	}}
	got := lastMessageSnippet(long)
	if n := utf8.RuneCountInString(got); n != 141 {
		t.Errorf("rune count = %d, want 141 (140 + ellipsis)", n)
	}
}
