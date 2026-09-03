package agent

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// ApprovalReq is one blocking permission request from the agent loop to
// the UI. The loop waits on Resp; the UI answers exactly once.
type ApprovalReq struct {
	ID     string
	Action string
	Detail string
	Resp   chan bool
}

// Hub carries approval requests from background agent turns to the
// foreground UI (required in TUI mode, where stdin is owned by bubbletea).
// "Always" decisions are remembered per action kind for the session.
type Hub struct {
	Requests chan *ApprovalReq
	seq      atomic.Int64

	mu     sync.Mutex
	always map[string]bool
}

// NewHub builds a hub.
func NewHub() *Hub { return &Hub{Requests: make(chan *ApprovalReq, 16), always: map[string]bool{}} }

// AllowAlways auto-approves future requests for action (session scope,
// mirroring opencode's "allow always until restart").
func (h *Hub) AllowAlways(action string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.always[action] = true
}

// Ask blocks until the UI approves (true) or denies (false) the action.
// A nil hub denies: never execute sensitive actions without a UI.
// Previously always-allowed actions approve immediately without prompting.
func (h *Hub) Ask(action, detail string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	always := h.always[action]
	h.mu.Unlock()
	if always {
		return true
	}
	req := &ApprovalReq{
		ID:     fmt.Sprintf("appr-%d", h.seq.Add(1)),
		Action: action, Detail: detail, Resp: make(chan bool, 1),
	}
	h.Requests <- req
	return <-req.Resp
}
