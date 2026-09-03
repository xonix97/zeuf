package tui

import "zeuf/internal/agent"

// Action is a UI-to-core request (model pin, backend connect). The TUI
// never touches config, registry or router directly.
type Action interface{ action() }

// ActionOpenPicker asks core to send picker rows (opened via ctrl+p).
type ActionOpenPicker struct{}

func (ActionOpenPicker) action() {}

// ActionPin pins routing to one model FullID ("provider/id").
type ActionPin struct{ FullID string }

func (ActionPin) action() {}

// ActionUnpin returns to automatic routing.
type ActionUnpin struct{}

func (ActionUnpin) action() {}

// ActionConnect attaches a new direct endpoint. Secret stays a string in
// memory only until the core stores it via the auth package.
type ActionConnect struct {
	Name    string
	BaseURL string
	KeyEnv  string // use environment variable ("" when Secret is set)
	Secret  string // paste-now key ("" when KeyEnv is set)
}

func (ActionConnect) action() {}

// ActionLogin asks core to rescan after a manual CLI login.
type ActionLogin struct{ Backend string } // "opencode" | "kilo"

func (ActionLogin) action() {}

// PickerModel is one row of the /models picker (primitives only).
type PickerModel struct {
	FullID  string
	Display string
	Detail  string
	Free    bool
	Pinned  bool
}

// Event is a core-to-UI update.
type Event struct {
	Kind string // "token","text","tool","switch","status","plan","error","done","picker"
	Text string
	// Approval carries a blocking permission request (answer via Resp).
	Approval *agent.ApprovalReq
	// Models carries picker rows for Kind=="picker".
	Models []PickerModel
}
