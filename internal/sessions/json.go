package sessions

import (
	"encoding/json"
	"io"
	"time"

	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// sessionJSON is the wire shape of one row in `lazyagent sessions --json`.
// Field names are part of the CLI contract — do not rename.
type sessionJSON struct {
	Agent         string    `json:"agent"`
	SessionID     string    `json:"session_id"`
	Name          string    `json:"name,omitempty"`
	CWD           string    `json:"cwd"`
	LastActivity  time.Time `json:"last_activity"`
	Messages      int       `json:"messages"`
	ResumeCommand string    `json:"resume_command,omitempty"`
}

// writeJSON emits the filtered sessions as an indented JSON array.
// nameFor resolves the display name for a session ("" omits the field).
func writeJSON(w io.Writer, sessions []*model.Session, nameFor func(*model.Session) string) error {
	out := make([]sessionJSON, 0, len(sessions)) // non-nil so empty encodes as []
	for _, s := range sessions {
		out = append(out, sessionJSON{
			Agent:         s.Agent,
			SessionID:     s.SessionID,
			Name:          nameFor(s),
			CWD:           s.CWD,
			LastActivity:  s.LastActivity,
			Messages:      s.TotalMessages,
			ResumeCommand: core.ResumeCommand(s.Agent, s.SessionID),
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
