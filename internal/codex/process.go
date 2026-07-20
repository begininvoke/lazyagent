package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/claude"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// SessionsDir returns the path to Codex session JSONL files under ~/.codex/sessions.
func SessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "sessions")
}

// SessionIndexPath returns the path to Codex's thread-name index file.
func SessionIndexPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "session_index.jsonl")
}

// DiscoverSessions scans the Codex sessions tree for JSONL session files.
func DiscoverSessions(cache *model.SessionCache) ([]*model.Session, error) {
	return discoverSessionsFromDir(SessionsDir(), SessionIndexPath(), cache, nil)
}

// DiscoverSessionsFiltered scans the Codex sessions tree like DiscoverSessions,
// but skips fully parsing files whose working directory does not match
// cwdMatch. A file's cwd is determined cheaply via a head-read of its first
// line (see headCWD); when that cannot be determined, the file is
// conservatively treated as matching and fully parsed, so a session is never
// silently dropped — the worst case is an unnecessary full parse.
//
// cwdMatch may be nil, in which case every session matches (equivalent to
// DiscoverSessions).
func DiscoverSessionsFiltered(cache *model.SessionCache, cwdMatch func(string) bool) ([]*model.Session, error) {
	return discoverSessionsFromDir(SessionsDir(), SessionIndexPath(), cache, cwdMatch)
}

type parseJob struct {
	path   string
	cached *model.Session
	offset int64
	mtime  time.Time
}

type parseResult struct {
	session   *model.Session
	path      string
	mtime     time.Time
	newOffset int64
}

func discoverSessionsFromDir(sessionsDir, indexPath string, cache *model.SessionCache, cwdMatch func(string) bool) ([]*model.Session, error) {
	if sessionsDir == "" {
		return nil, fmt.Errorf("could not find home directory")
	}

	names := loadSessionNames(indexPath)
	seen := make(map[string]struct{})
	var sessions []*model.Session
	var jobs []parseJob

	// Phase 1: walk the tree, collect cache hits and jobs for parsing.
	err := filepath.WalkDir(sessionsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}

		seen[path] = struct{}{}
		cached, offset, mtime := cache.GetIncremental(path)

		if cached != nil && offset == 0 {
			// Full cache hit — we already know the cwd, no need to touch the file.
			if cwdMatch != nil && !cwdMatch(cached.CWD) {
				return nil
			}
			sessions = append(sessions, cached)
			return nil
		}

		if cwdMatch != nil {
			// Cheap prefilter before committing to a full (or incremental)
			// parse: head-read the file's cwd from its first line. If it
			// can't be determined, conservatively fall through to a full
			// parse rather than risk silently dropping a session.
			if cwd, ok := headCWD(path); ok && !cwdMatch(cwd) {
				return nil
			}
		}

		jobs = append(jobs, parseJob{
			path:   path,
			cached: cached,
			offset: offset,
			mtime:  mtime,
		})
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("walk codex sessions: %w", err)
	}

	if len(jobs) > 0 {
		// Phase 2: parse files in parallel.
		workers := runtime.GOMAXPROCS(0)
		if workers > len(jobs) {
			workers = len(jobs)
		}

		results := make([]parseResult, len(jobs))
		var wg sync.WaitGroup
		jobCh := make(chan int, len(jobs))

		for i := range jobs {
			jobCh <- i
		}
		close(jobCh)

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for idx := range jobCh {
					j := &jobs[idx]
					var session *model.Session
					var newOffset int64

					if j.cached != nil && j.offset > 0 {
						s, off, err := ParseJSONLIncremental(j.path, j.offset, j.cached)
						if err != nil {
							continue
						}
						session = s
						newOffset = off
					} else {
						s, size, err := ParseJSONL(j.path)
						if err != nil {
							continue
						}
						session = s
						newOffset = size
					}

					results[idx] = parseResult{
						session:   session,
						path:      j.path,
						mtime:     j.mtime,
						newOffset: newOffset,
					}
				}
			}()
		}
		wg.Wait()

		// Phase 3: enrich and update cache (sequential — wtCache is not thread-safe).
		wtCache := make(map[string]wtInfo)
		for _, r := range results {
			if r.session == nil {
				continue
			}
			enrichSession(r.session, wtCache, names)
			cache.Put(r.path, r.mtime, r.newOffset, r.session)
			sessions = append(sessions, r.session)
		}
	}

	cache.Prune(seen)
	return sessions, nil
}

type wtInfo struct {
	isWorktree bool
	mainRepo   string
}

func enrichSession(session *model.Session, wtCache map[string]wtInfo, names map[string]string) {
	if session.SessionID != "" && session.Name == "" {
		session.Name = names[session.SessionID]
	}
	if session.CWD == "" {
		return
	}
	if _, ok := wtCache[session.CWD]; !ok {
		isWT, mainRepo := claude.IsWorktree(session.CWD)
		wtCache[session.CWD] = wtInfo{isWorktree: isWT, mainRepo: mainRepo}
	}
	wt := wtCache[session.CWD]
	session.IsWorktree = wt.isWorktree
	session.MainRepo = wt.mainRepo
}

type indexEntry struct {
	ID         string `json:"id"`
	ThreadName string `json:"thread_name"`
}

func loadSessionNames(path string) map[string]string {
	names := make(map[string]string)
	f, err := os.Open(path)
	if err != nil {
		return names
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var e indexEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		if e.ID != "" && e.ThreadName != "" {
			names[e.ID] = e.ThreadName
		}
	}
	return names
}

type jsonlEnvelope struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type sessionMetaPayload struct {
	ID            string          `json:"id"`
	CWD           string          `json:"cwd"`
	CLIVersion    string          `json:"cli_version"`
	AgentNickname string          `json:"agent_nickname"`
	Source        json.RawMessage `json:"source"`
}

// maxHeadLineSize bounds how much of a rollout file's first line headCWD
// will read before giving up, so a pathological line can't force reading
// large amounts of data into memory.
const maxHeadLineSize = 1 << 20 // 1 MiB

// headCWD reads only the first line of a Codex rollout JSONL file and, if it
// is a well-formed session_meta entry with a non-empty cwd, returns that cwd.
// It never reads past the first newline (or maxHeadLineSize, whichever comes
// first), so it stays cheap even on multi-GiB files. ok is false whenever the
// cwd could not be determined (missing file, empty file, unparsable first
// line, wrong envelope type, or missing cwd) — callers should treat that as
// "unknown, don't filter it out" rather than "no match".
func headCWD(path string) (cwd string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 64*1024)
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > maxHeadLineSize {
			return "", false
		}
		if err == nil {
			break
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF {
			if len(line) == 0 {
				return "", false
			}
			break // last (only) line, no trailing newline
		}
		return "", false
	}

	var env jsonlEnvelope
	if err := json.Unmarshal(bytes.TrimRight(line, "\r\n"), &env); err != nil {
		return "", false
	}
	if env.Type != "session_meta" {
		return "", false
	}
	var meta sessionMetaPayload
	if err := json.Unmarshal(env.Payload, &meta); err != nil || meta.CWD == "" {
		return "", false
	}
	return meta.CWD, true
}

type turnContextPayload struct {
	CWD   string `json:"cwd"`
	Model string `json:"model"`
	Git   gitCtx `json:"git"`
}

type gitCtx struct {
	Branch string `json:"branch"`
}

type responseItemPayload struct {
	Type      string              `json:"type"`
	Name      string              `json:"name"`
	Role      string              `json:"role"`
	Arguments string              `json:"arguments"`
	Content   []responseItemBlock `json:"content"`
}

type responseItemBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type eventPayload struct {
	Type             string          `json:"type"`
	LastAgentMessage string          `json:"last_agent_message"`
	Info             *tokenCountInfo `json:"info"`
}

type tokenCountInfo struct {
	TotalTokenUsage tokenUsage `json:"total_token_usage"`
}

type tokenUsage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	OutputTokens      int `json:"output_tokens"`
	ReasoningOutput   int `json:"reasoning_output_tokens"`
}

type lastMeaningful struct {
	Kind      string
	Timestamp time.Time
	ToolName  string
}

// ParseJSONL reads a Codex session file and builds a Session snapshot.
func ParseJSONL(path string) (*model.Session, int64, error) {
	return parseJSONL(path, 0, nil)
}

// ParseJSONLIncremental reads only new lines and merges them into a prior session.
func ParseJSONLIncremental(path string, offset int64, base *model.Session) (*model.Session, int64, error) {
	return parseJSONL(path, offset, base)
}

func parseJSONL(path string, offset int64, base *model.Session) (*model.Session, int64, error) {
	f, err := os.Open(path)
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
		session.JSONLPath = path
	} else {
		session = &model.Session{
			JSONLPath: path,
			Agent:     "codex",
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	bytesConsumed := offset
	var last lastMeaningful
	if base != nil {
		last = lastMeaningful{Kind: statusKind(base.Status), Timestamp: base.LastActivity, ToolName: base.CurrentTool}
	}

	for scanner.Scan() {
		bytesConsumed += int64(len(scanner.Bytes())) + 1

		var env jsonlEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &env); err != nil {
			continue
		}

		ts, _ := time.Parse(time.RFC3339Nano, env.Timestamp)
		if !ts.IsZero() {
			session.EntryTimestamps = append(session.EntryTimestamps, ts)
			if len(session.EntryTimestamps) > 500 {
				session.EntryTimestamps = session.EntryTimestamps[len(session.EntryTimestamps)-500:]
			}
		}

		switch env.Type {
		case "session_meta":
			var meta sessionMetaPayload
			if err := json.Unmarshal(env.Payload, &meta); err != nil {
				continue
			}
			if meta.ID != "" {
				session.SessionID = meta.ID
			}
			if meta.CWD != "" {
				session.CWD = meta.CWD
			}
			if meta.CLIVersion != "" {
				session.Version = meta.CLIVersion
			}
			if meta.AgentNickname != "" || strings.Contains(string(meta.Source), "\"subagent\"") {
				session.IsSidechain = true
			}
		case "turn_context":
			var ctx turnContextPayload
			if err := json.Unmarshal(env.Payload, &ctx); err != nil {
				continue
			}
			if ctx.CWD != "" {
				session.CWD = ctx.CWD
			}
			if ctx.Model != "" {
				session.Model = ctx.Model
			}
			if ctx.Git.Branch != "" {
				session.GitBranch = ctx.Git.Branch
			}
		case "response_item":
			var item responseItemPayload
			if err := json.Unmarshal(env.Payload, &item); err != nil {
				continue
			}
			switch item.Type {
			case "message":
				text := strings.TrimSpace(joinItemText(item.Content))
				switch item.Role {
				case "user":
					session.UserMessages++
					if text != "" {
						appendMessage(session, "user", text, ts)
					}
					last = lastMeaningful{Kind: "user", Timestamp: ts}
				case "assistant":
					session.AssistantMessages++
					if text != "" {
						appendMessage(session, "assistant", text, ts)
					}
					last = lastMeaningful{Kind: "assistant", Timestamp: ts}
				}
			case "function_call":
				appendTool(session, item.Name, ts)
				last = lastMeaningful{Kind: "tool", Timestamp: ts, ToolName: item.Name}
				if item.Name == "apply_patch" {
					session.LastFileWrite = "apply_patch"
					session.LastFileWriteAt = ts
				}
			case "function_call_output":
				last = lastMeaningful{Kind: "tool_output", Timestamp: ts}
			}
		case "event_msg":
			var event eventPayload
			if err := json.Unmarshal(env.Payload, &event); err != nil {
				continue
			}
			switch event.Type {
			case "user_message":
				last = lastMeaningful{Kind: "user", Timestamp: ts}
			case "agent_message":
				last = lastMeaningful{Kind: "assistant", Timestamp: ts}
			case "token_count":
				if event.Info != nil {
					session.InputTokens = event.Info.TotalTokenUsage.InputTokens
					session.CacheReadTokens = event.Info.TotalTokenUsage.CachedInputTokens
					session.OutputTokens = event.Info.TotalTokenUsage.OutputTokens + event.Info.TotalTokenUsage.ReasoningOutput
				}
			case "task_complete":
				if strings.TrimSpace(event.LastAgentMessage) != "" {
					last = lastMeaningful{Kind: "assistant", Timestamp: ts}
				}
			}
		}
	}

	session.TotalMessages = session.UserMessages + session.AssistantMessages
	session.Status = statusFromKind(last.Kind)
	session.CurrentTool = ""
	if session.Status == model.StatusExecutingTool {
		session.CurrentTool = last.ToolName
	}
	if !last.Timestamp.IsZero() {
		session.LastActivity = last.Timestamp
	}

	if fi, err := f.Stat(); err == nil && bytesConsumed > fi.Size() {
		bytesConsumed = fi.Size()
	}

	return session, bytesConsumed, nil
}

func appendTool(session *model.Session, name string, ts time.Time) {
	if name == "" {
		return
	}
	session.RecentTools = append(session.RecentTools, model.ToolCall{Name: name, Timestamp: ts})
	if len(session.RecentTools) > 20 {
		session.RecentTools = session.RecentTools[len(session.RecentTools)-20:]
	}
}

func appendMessage(session *model.Session, role, text string, ts time.Time) {
	session.RecentMessages = append(session.RecentMessages, model.ConversationMessage{
		Role:      role,
		Text:      model.Truncate(text, 300),
		Timestamp: ts,
	})
	if len(session.RecentMessages) > 10 {
		session.RecentMessages = session.RecentMessages[len(session.RecentMessages)-10:]
	}
}

func joinItemText(content []responseItemBlock) string {
	var parts []string
	for _, block := range content {
		if (block.Type == "input_text" || block.Type == "output_text") && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func statusKind(status model.SessionStatus) string {
	switch status {
	case model.StatusWaitingForUser:
		return "assistant"
	case model.StatusThinking:
		return "user"
	case model.StatusExecutingTool:
		return "tool"
	case model.StatusProcessingResult:
		return "tool_output"
	default:
		return ""
	}
}

func statusFromKind(kind string) model.SessionStatus {
	switch kind {
	case "assistant":
		return model.StatusWaitingForUser
	case "user":
		return model.StatusThinking
	case "tool":
		return model.StatusExecutingTool
	case "tool_output":
		return model.StatusProcessingResult
	default:
		return model.StatusIdle
	}
}
