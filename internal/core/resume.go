package core

import "fmt"

// ResumeCommand returns the CLI command to resume a session for the given agent.
// Returns empty string for unknown agents or empty session IDs.
func ResumeCommand(agent, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	switch agent {
	case "claude":
		return fmt.Sprintf("claude --resume %s", sessionID)
	case "codex":
		return fmt.Sprintf("codex resume %s", sessionID)
	case "amp":
		return fmt.Sprintf("amp threads continue %s", sessionID)
	case "pi":
		return fmt.Sprintf("pi --session %s", sessionID)
	case "opencode":
		return fmt.Sprintf("opencode -s %s", sessionID)
	case "kilo":
		return fmt.Sprintf("kilo --session=%s", sessionID)
	case "cursor":
		return fmt.Sprintf("cursor-agent --resume=%q", sessionID)
	case "grok":
		return fmt.Sprintf("grok --resume %s", sessionID)
	case "kimi":
		return fmt.Sprintf("kimi --resume %s", sessionID)
	default:
		return ""
	}
}

// ResumeArgv returns the executable argv to resume a session, or nil when
// the agent has no resume command lazyagent is willing to exec.
// OpenCode, Kilo, and Cursor have a display string only; see ResumeCommand.
// "Openable" everywhere in the codebase means ResumeArgv != nil.
func ResumeArgv(agent, sessionID string) []string {
	if sessionID == "" {
		return nil
	}
	switch agent {
	case "claude":
		return []string{"claude", "--resume", sessionID}
	case "codex":
		return []string{"codex", "resume", sessionID}
	case "amp":
		return []string{"amp", "threads", "continue", sessionID}
	case "pi":
		return []string{"pi", "--session", sessionID}
	case "grok":
		return []string{"grok", "--resume", sessionID}
	case "kimi":
		return []string{"kimi", "--resume", sessionID}
	default:
		return nil
	}
}
