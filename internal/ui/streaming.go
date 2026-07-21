package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// streamStartedMsg carries the update/done channels a just-launched
// progressive-first-load goroutine will signal on. core.SessionManager's
// streaming reload itself has no channel of its own to expose (it just
// calls onUpdate, like watchCmd's events channel is owned by the watcher)
// -- these channels are created once, here, for the whole stream, and must
// be threaded back into the model via Update so later batch/done messages
// can keep waiting on them.
type streamStartedMsg struct {
	updates <-chan struct{}
	done    <-chan struct{}
}

// streamBatchMsg is sent each time the progressive first load merges a new
// provider member's sessions into the manager. Like fileWatchMsg, it
// carries no data -- the handler just re-reads whatever's now in the
// manager.
type streamBatchMsg struct{}

// streamDoneMsg is sent once the progressive first load's underlying
// core.DiscoverMatchingStream has finished (every provider member has
// completed).
type streamDoneMsg struct{}

// runStreamingLoadCmd starts the actual streaming discovery work (run, from
// core.SessionManager.BeginReloadStreaming) in a background goroutine and
// returns a tea.Cmd that reports back once it has: the actual discovery
// work happens off of bubbletea's own command-runner goroutine so batches
// can be delivered as they arrive, rather than bubbletea only learning
// about the load once the whole thing finishes.
//
// run must come from BeginReloadStreaming, called synchronously by the
// caller (Init, below) BEFORE this Cmd -- or any other Cmd in the same
// batch, including watchCmd -- is dispatched. That ordering is what
// actually guards against a watcher event or poll tick racing the stream's
// startup (see BeginReloadStreaming's doc): this function only owns
// delivering batches progressively, not the guard itself.
func runStreamingLoadCmd(run func(onUpdate func())) tea.Cmd {
	return func() tea.Msg {
		updates := make(chan struct{}, 1) // buffered: coalesce bursts, onUpdate must never block
		done := make(chan struct{})
		go func() {
			run(func() {
				select {
				case updates <- struct{}{}:
				default:
				}
			})
			close(done)
		}()
		return streamStartedMsg{updates: updates, done: done}
	}
}

// streamNextCmd blocks until either channel fires and reuses watchCmd's
// pattern for watcher events: a Cmd that waits for the next "something
// happened" signal and translates it into a Msg, re-armed by the Update
// handler after each message so the wait continues until the stream itself
// finishes.
func streamNextCmd(updates <-chan struct{}, done <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-updates:
			return streamBatchMsg{}
		case <-done:
			return streamDoneMsg{}
		}
	}
}
