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

// ActionAllowAlways records a session-scoped always-allow for an action
// kind (e.g. "write file"), mirroring opencode's allow-always.
type ActionAllowAlways struct{ Tool string }

func (ActionAllowAlways) action() {}

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
	Kind string // "token","reasoning","text","tool-start","tool-end","switch","status","plan","session","task","usage","error","done","picker"
	Text string
	// Approval carries a blocking permission request (answer via Resp).
	Approval *agent.ApprovalReq
	// Models carries picker rows for Kind=="picker".
	Models []PickerModel
	// Tool carries the tool name for Kind=="tool-start"/"tool-end".
	Tool string
	// Args carries raw JSON arguments for Kind=="tool-start".
	Args string
	// Ok reports tool success for Kind=="tool-end".
	Ok bool
	// Depth marks subagent-nested content (0 = orchestrator itself).
	Depth int
	// Detail carries extra lines: result preview for "tool-end",
	// "1:title"/"0:title" plan lines for "plan",
	// "workdir|branch|dirty" for "session".
	Detail string
}
