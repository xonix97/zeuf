package agent

import (
	"fmt"
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
type Hub struct {
	Requests chan *ApprovalReq
	seq      atomic.Int64
}

// NewHub builds a hub.
func NewHub() *Hub { return &Hub{Requests: make(chan *ApprovalReq, 16)} }

// Ask blocks until the UI approves (true) or denies (false) the action.
// A nil hub denies: never execute sensitive actions without a UI.
func (h *Hub) Ask(action, detail string) bool {
	if h == nil {
		return false
	}
	req := &ApprovalReq{
		ID:     fmt.Sprintf("appr-%d", h.seq.Add(1)),
		Action: action, Detail: detail, Resp: make(chan bool, 1),
	}
	h.Requests <- req
	return <-req.Resp
}
