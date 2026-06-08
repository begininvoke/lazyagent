package kimi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

const (
	maxRecentMessages  = 10
	maxRecentTools     = 20
	maxEntryTimestamps = 500
)

// fileWriteTools are the tool names whose execution touches a file on disk,
// across both legacy kimi-cli (WriteFile/StrReplaceFile) and the new kimi-code
// (Write/Edit/NotebookEdit) tool vocabularies.
var fileWriteTools = map[string]bool{
	"WriteFile":      true,
	"StrReplaceFile": true,
	"Write":          true,
	"Edit":           true,
	"NotebookEdit":   true,
}

// ParseSession reads one Kimi session directory into a model.Session.
func ParseSession(sessionDir, workDir string) (*model.Session, int64, error) {
	return ParseSessionIncremental(sessionDir, workDir, 0, nil)
}

// ParseSessionIncremental reads one Kimi session directory, optionally parsing
// only the bytes appended to the wire stream and merging them into base.
//
// kimi-code stores the main agent's event stream at
// <sessionDir>/agents/main/wire.jsonl. Two encodings coexist on disk:
//
//   - Sessions imported from legacy kimi-cli materialize the whole conversation
//     as context.append_message events (user/assistant/tool roles, tool calls
//     embedded under message.toolCalls).
//   - Native kimi-code sessions append user turns as context.append_message but
//     stream assistant content, tool calls/results and token usage through
//     context.append_loop_event / usage.record events.
//
// The two encodings populate disjoint event sets for assistant content, so a
// single pass over the stream parses both without double counting.
func ParseSessionIncremental(sessionDir, workDir string, offset int64, base *model.Session) (*model.Session, int64, error) {
	wirePath := wireFile(sessionDir)
	f, err := os.Open(wirePath)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, 0); err != nil {
			return nil, 0, err
		}
	}

	var session *model.Session
	if base != nil {
		session = base.Clone()
		session.JSONLPath = sessionDir
	} else {
		session = &model.Session{
			Agent:     "kimi",
			SessionID: filepath.Base(sessionDir),
			JSONLPath: sessionDir,
			CWD:       workDir,
		}
	}
	if session.CWD == "" {
		session.CWD = workDir
	}
	if session.SessionID == "" {
		session.SessionID = filepath.Base(sessionDir)
	}

	state := readState(stateFile(sessionDir))
	if state.Title != "" {
		session.Name = state.Title
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)

	bytesConsumed := offset
	status := session.Status
	currentTool := session.CurrentTool
	if base == nil {
		status = model.StatusIdle
	}
	clock := session.LastActivity

	for scanner.Scan() {
		line := scanner.Bytes()
		bytesConsumed += int64(len(line)) + 1

		var ev wireEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}

		// Advance the running clock from whichever timestamp the event carries;
		// context.append_message events have none and inherit the last seen one.
		if ts := eventTime(ev); !ts.IsZero() {
			clock = ts
			session.LastActivity = ts
			session.EntryTimestamps = append(session.EntryTimestamps, ts)
			if len(session.EntryTimestamps) > maxEntryTimestamps {
				session.EntryTimestamps = session.EntryTimestamps[len(session.EntryTimestamps)-maxEntryTimestamps:]
			}
		}

		switch ev.Type {
		case "metadata":
			if ev.ProtocolVersion != "" {
				session.Version = ev.ProtocolVersion
			}
		case "config.update":
			if ev.ModelAlias != "" {
				session.Model = ev.ModelAlias
			}
		case "context.append_message":
			if ev.Message != nil {
				status, currentTool = applyMessage(session, ev.Message, clock, status, currentTool)
			}
		case "context.append_loop_event":
			if ev.Event != nil {
				status, currentTool = applyLoopEvent(session, ev.Event, clock, status, currentTool)
			}
		case "usage.record":
			addUsage(session, ev.Usage)
		case "turn.end", "turn.cancel":
			status = model.StatusWaitingForUser
			currentTool = ""
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}

	session.TotalMessages = session.UserMessages + session.AssistantMessages
	session.Status = status
	session.CurrentTool = ""
	if status == model.StatusExecutingTool {
		session.CurrentTool = currentTool
	}
	if session.Name == "" {
		session.Name = firstUserMessage(session.RecentMessages)
	}

	if fi, err := f.Stat(); err == nil && bytesConsumed > fi.Size() {
		bytesConsumed = fi.Size()
	}
	return session, bytesConsumed, nil
}

// applyMessage folds a context.append_message event into the session.
func applyMessage(session *model.Session, msg *wireMessage, ts time.Time, status model.SessionStatus, currentTool string) (model.SessionStatus, string) {
	switch msg.Role {
	case "user":
		if text := contentText(msg.Content); text != "" {
			session.UserMessages++
			appendMessage(session, "user", text, ts)
		}
		return model.StatusThinking, ""
	case "assistant":
		if text := contentText(msg.Content); text != "" {
			session.AssistantMessages++
			appendMessage(session, "assistant", text, ts)
			status = model.StatusWaitingForUser
		}
		for _, tc := range msg.ToolCalls {
			name := tc.Function.Name
			if name == "" {
				name = tc.Name
			}
			appendTool(session, name, ts)
			setLastFileWrite(session, name, []byte(tc.Function.Arguments), ts)
			status = model.StatusExecutingTool
			currentTool = name
		}
		return status, currentTool
	case "tool":
		return model.StatusProcessingResult, currentTool
	}
	return status, currentTool
}

// applyLoopEvent folds a context.append_loop_event into the session.
func applyLoopEvent(session *model.Session, ev *loopEvent, ts time.Time, status model.SessionStatus, currentTool string) (model.SessionStatus, string) {
	switch ev.Type {
	case "step.begin":
		return model.StatusThinking, ""
	case "content.part":
		if ev.Part != nil && ev.Part.Type == "text" && strings.TrimSpace(ev.Part.Text) != "" {
			session.AssistantMessages++
			appendMessage(session, "assistant", ev.Part.Text, ts)
		}
		return model.StatusThinking, currentTool
	case "tool.call":
		appendTool(session, ev.Name, ts)
		setLastFileWrite(session, ev.Name, ev.Args, ts)
		return model.StatusExecutingTool, ev.Name
	case "tool.result":
		return model.StatusProcessingResult, currentTool
	case "step.end":
		if ev.FinishReason == "tool_use" {
			return model.StatusProcessingResult, currentTool
		}
		return model.StatusWaitingForUser, ""
	case "compaction.begin", "compaction.end":
		session.LastSummaryAt = ts
	}
	return status, currentTool
}

func addUsage(session *model.Session, u *usageRecord) {
	if u == nil {
		return
	}
	session.InputTokens += u.InputOther
	session.OutputTokens += u.Output
	session.CacheReadTokens += u.InputCacheRead
	session.CacheCreationTokens += u.InputCacheCreation
}

// eventTime returns the timestamp an event carries, in local time. kimi-code
// emits Unix-millisecond timestamps (metadata uses created_at, every other
// timestamped event uses time).
func eventTime(ev wireEvent) time.Time {
	switch {
	case ev.Time != 0:
		return unixMillis(ev.Time)
	case ev.CreatedAt != 0:
		return unixMillis(ev.CreatedAt)
	}
	return time.Time{}
}

type kimiState struct {
	Title     string `json:"title"`
	UpdatedAt string `json:"updatedAt"`
}

func readState(path string) kimiState {
	data, err := os.ReadFile(path)
	if err != nil {
		return kimiState{}
	}
	var state kimiState
	_ = json.Unmarshal(data, &state)
	return state
}

// wireEvent is the union of every kimi-code wire envelope shape we read.
type wireEvent struct {
	Type            string `json:"type"`
	ProtocolVersion string `json:"protocol_version"`
	CreatedAt       int64  `json:"created_at"`
	Time            int64  `json:"time"`
	ModelAlias      string `json:"modelAlias"`

	Input   []contentBlock `json:"input"` // turn.prompt
	Message *wireMessage   `json:"message"`
	Event   *loopEvent     `json:"event"`
	Usage   *usageRecord   `json:"usage"`
}

type wireMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCalls  []toolCall      `json:"toolCalls"`
	ToolCallID string          `json:"toolCallId"`
}

type toolCall struct {
	Name     string `json:"name"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type loopEvent struct {
	Type         string          `json:"type"`
	Part         *contentBlock   `json:"part"`
	Name         string          `json:"name"`
	Args         json.RawMessage `json:"args"`
	ToolCallID   string          `json:"toolCallId"`
	FinishReason string          `json:"finishReason"`
	Usage        *usageRecord    `json:"usage"`
}

type contentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Think string `json:"think"`
}

type usageRecord struct {
	InputOther         int `json:"inputOther"`
	Output             int `json:"output"`
	InputCacheRead     int `json:"inputCacheRead"`
	InputCacheCreation int `json:"inputCacheCreation"`
}

func unixMillis(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.Unix(ms/1000, (ms%1000)*int64(time.Millisecond))
}

// contentText flattens a message content field, which is either a plain string
// or an array of typed blocks, keeping only human-readable text.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return strings.TrimSpace(s)
		}
		return ""
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	return blocksText(blocks)
}

// blocksText joins the text of "text" blocks, dropping thinking and other
// non-user-facing block types.
func blocksText(blocks []contentBlock) string {
	var parts []string
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func appendTool(session *model.Session, name string, ts time.Time) {
	if name == "" {
		return
	}
	session.RecentTools = append(session.RecentTools, model.ToolCall{Name: name, Timestamp: ts})
	if len(session.RecentTools) > maxRecentTools {
		session.RecentTools = session.RecentTools[len(session.RecentTools)-maxRecentTools:]
	}
}

func appendMessage(session *model.Session, role, text string, ts time.Time) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	session.RecentMessages = append(session.RecentMessages, model.ConversationMessage{
		Role:      role,
		Text:      model.Truncate(text, 300),
		Timestamp: ts,
	})
	if len(session.RecentMessages) > maxRecentMessages {
		session.RecentMessages = session.RecentMessages[len(session.RecentMessages)-maxRecentMessages:]
	}
}

// setLastFileWrite records the most recently written/edited file. Arguments may
// arrive as a JSON string (embedded tool calls) or object (loop tool calls);
// both decode the same. Legacy tools use "path", new tools use "file_path".
func setLastFileWrite(session *model.Session, toolName string, args []byte, ts time.Time) {
	if !fileWriteTools[toolName] {
		return
	}
	path := writePath(args)
	if path == "" {
		session.LastFileWrite = toolName
		session.LastFileWriteAt = ts
		return
	}
	if filepath.IsAbs(path) || session.CWD == "" {
		session.LastFileWrite = path
	} else {
		session.LastFileWrite = filepath.Join(session.CWD, path)
	}
	session.LastFileWriteAt = ts
}

func writePath(args []byte) string {
	if len(args) == 0 {
		return ""
	}
	var payload struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return ""
	}
	if payload.Path != "" {
		return payload.Path
	}
	return payload.FilePath
}

func firstUserMessage(messages []model.ConversationMessage) string {
	for _, msg := range messages {
		if msg.Role == "user" && msg.Text != "" {
			return msg.Text
		}
	}
	return ""
}

// ContextChunk is a normalized Kimi transcript chunk for search indexing.
type ContextChunk struct {
	SessionID string
	CWD       string
	Name      string
	Role      string
	Text      string
}

// ExtractContextChunks reads the session wire stream for search indexing,
// yielding one chunk per user/assistant text message across both wire encodings.
func ExtractContextChunks(sessionDir, workDir string) ([]ContextChunk, error) {
	sessionID := filepath.Base(sessionDir)
	state := readState(stateFile(sessionDir))
	return extractWireChunks(wireFile(sessionDir), sessionID, workDir, state.Title)
}

func extractWireChunks(path, sessionID, cwd, name string) ([]ContextChunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var chunks []ContextChunk
	add := func(role, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		chunks = append(chunks, ContextChunk{SessionID: sessionID, CWD: cwd, Name: name, Role: role, Text: text})
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		var ev wireEvent
		if json.Unmarshal(scanner.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "context.append_message":
			if ev.Message == nil {
				continue
			}
			switch ev.Message.Role {
			case "user":
				add("user", contentText(ev.Message.Content))
			case "assistant":
				add("assistant", contentText(ev.Message.Content))
			}
		case "context.append_loop_event":
			if ev.Event != nil && ev.Event.Type == "content.part" && ev.Event.Part != nil && ev.Event.Part.Type == "text" {
				add("assistant", ev.Event.Part.Text)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("kimi: no searchable content in %s", path)
	}
	return chunks, nil
}
