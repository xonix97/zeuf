package core

import (
	"strings"
	"time"
)

// Session is Zeuf's provider-agnostic agent state. The models may change;
// the session does not. It is reconstructed into whatever wire format the
// selected backend needs on every turn, so switching models never restarts
// the conversation.
type Session struct {
	ID                  string               `json:"id"`
	Task                string               `json:"task"`
	SystemPrompt        string               `json:"system_prompt"`
	Messages            []Message            `json:"messages"`
	Plan                []PlanStep           `json:"plan"`
	FilesInspected      []string             `json:"files_inspected"`
	PreExistingDirty    []string             `json:"pre_existing_dirty,omitempty"`
	ModifiedFiles       []string             `json:"modified_files,omitempty"`
	VerificationHistory []VerificationResult `json:"verification_history,omitempty"`
	Trace               []TraceEvent         `json:"trace,omitempty"`
	TaskGraphData       []byte               `json:"task_graph_data,omitempty"`
	PendingTools        []ToolCall           `json:"pending_tools,omitempty"`
	TokensIn            int64                `json:"tokens_in"`
	TokensOut           int64                `json:"tokens_out"`
	Checkpoints         []Checkpoint         `json:"checkpoints,omitempty"`
	SwitchTrail         []string             `json:"switch_trail"` // model FullIDs used, in order
	Meta                map[string]string    `json:"meta,omitempty"`
	Created             time.Time            `json:"created"`
	Updated             time.Time            `json:"updated"`
}

// FileVersion records a file's pre-turn content for rewind.
// Before is empty when the file did not exist (Existed=false) or was too
// large to snapshot (TooLarge=true, unrestorable).
type FileVersion struct {
	Path     string `json:"path"`
	Before   string `json:"before,omitempty"`
	Existed  bool   `json:"existed"`
	TooLarge bool   `json:"too_large,omitempty"`
}

// Checkpoint groups one turn's first-touch file versions.
type Checkpoint struct {
	Label string        `json:"label"`
	At    time.Time     `json:"at"`
	Files []FileVersion `json:"files"`
}

// PlanStep is one user-visible unit of the current plan.
type PlanStep struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Done   bool   `json:"done"`
}

// NewSession creates a session with the Zeuf system prompt.
func NewSession(id, task, systemPrompt string) *Session {
	now := time.Now()
	return &Session{
		ID:           id,
		Task:         task,
		SystemPrompt: systemPrompt,
		Messages:     []Message{},
		Meta:         map[string]string{},
		Created:      now,
		Updated:      now,
	}
}

func (s *Session) touch() { s.Updated = time.Now() }

// AppendUser records a user turn.
func (s *Session) AppendUser(content string) {
	s.Messages = append(s.Messages, Message{Role: RoleUser, Content: content})
	s.touch()
}

// AppendAssistant records an assistant turn (content and/or tool calls).
func (s *Session) AppendAssistant(content string, calls []ToolCall) {
	s.Messages = append(s.Messages, Message{Role: RoleAssistant, Content: content, ToolCalls: calls})
	s.touch()
}

// AppendTool records a structured tool result.
func (s *Session) AppendTool(callID, name, content string, isError bool) {
	s.Messages = append(s.Messages, Message{
		Role: RoleTool, Content: content, Name: name, ToolCallID: callID,
	})
	_ = isError
	s.touch()
}

// AddUsage accumulates per-turn token accounting into session totals.
func (s *Session) AddUsage(u Usage) {
	s.TokensIn += u.Input
	s.TokensOut += u.Output
	s.touch()
}

// AddCheckpoint records a finished turn's file versions (no-op when empty).
func (s *Session) AddCheckpoint(cp Checkpoint) {
	if len(cp.Files) == 0 {
		return
	}
	s.Checkpoints = append(s.Checkpoints, cp)
	s.touch()
}

// AppendSystem records a system-context message (skills, directives).
func (s *Session) AppendSystem(content string) {
	s.Messages = append(s.Messages, Message{Role: RoleSystem, Content: content})
	s.touch()
}

// NoteFile remembers an inspected file (deduplicated).
func (s *Session) NoteFile(path string) {
	for _, f := range s.FilesInspected {
		if f == path {
			return
		}
	}
	s.FilesInspected = append(s.FilesInspected, path)
	s.touch()
}

// NoteModelSwitch records a model change without touching history.
func (s *Session) NoteModelSwitch(fullID string) {
	s.SwitchTrail = append(s.SwitchTrail, fullID)
	s.touch()
}

// NoteModifiedFile remembers a modified file (deduplicated).
func (s *Session) NoteModifiedFile(path string) {
	for _, f := range s.ModifiedFiles {
		if f == path {
			return
		}
	}
	s.ModifiedFiles = append(s.ModifiedFiles, path)
	s.touch()
}

// AddVerification records a verification step outcome.
func (s *Session) AddVerification(res VerificationResult) {
	s.VerificationHistory = append(s.VerificationHistory, res)
	s.touch()
}

// AddTrace records an observability trace event.
func (s *Session) AddTrace(ev TraceEvent) {
	s.Trace = append(s.Trace, ev)
	s.touch()
}

// SetTaskGraphData stores the serialized task graph.
func (s *Session) SetTaskGraphData(data []byte) {
	s.TaskGraphData = data
	s.touch()
}

// Snapshot returns a deep copy for tests / safe handoff.
func (s *Session) Snapshot() *Session {
	cp := *s
	cp.Messages = append([]Message(nil), s.Messages...)
	cp.Plan = append([]PlanStep(nil), s.Plan...)
	cp.FilesInspected = append([]string(nil), s.FilesInspected...)
	cp.PreExistingDirty = append([]string(nil), s.PreExistingDirty...)
	cp.ModifiedFiles = append([]string(nil), s.ModifiedFiles...)
	cp.VerificationHistory = append([]VerificationResult(nil), s.VerificationHistory...)
	cp.Trace = append([]TraceEvent(nil), s.Trace...)
	if len(s.TaskGraphData) > 0 {
		cp.TaskGraphData = append([]byte(nil), s.TaskGraphData...)
	}
	cp.PendingTools = append([]ToolCall(nil), s.PendingTools...)
	cp.SwitchTrail = append([]string(nil), s.SwitchTrail...)
	cp.Checkpoints = append([]Checkpoint(nil), s.Checkpoints...)
	for i := range cp.Checkpoints {
		cp.Checkpoints[i].Files = append([]FileVersion(nil), s.Checkpoints[i].Files...)
	}
	cp.Meta = map[string]string{}
	for k, v := range s.Meta {
		cp.Meta[k] = v
	}
	return &cp
}

// Transcript renders the session as plain text for delegated backends
// (gateways that only accept a single prompt string, e.g. the opencode/kilo
// CLI gateways). Nothing is dropped: system instructions, plan, files
// inspected, tool calls and tool results are all included so the next model
// can continue the same task.
func (s *Session) Transcript() string {
	var b strings.Builder
	if s.SystemPrompt != "" {
		b.WriteString("System instructions:\n" + s.SystemPrompt + "\n\n")
	}
	if s.Task != "" {
		b.WriteString("Current task:\n" + s.Task + "\n\n")
	}
	if len(s.Plan) > 0 {
		b.WriteString("Current plan:\n")
		for i, p := range s.Plan {
			mark := "[ ]"
			if p.Done {
				mark = "[x]"
			}
			b.WriteString(strings.TrimSpace(strings.Join([]string{mark, p.Title}, " ")) + "\n")
			if p.Detail != "" {
				b.WriteString("    " + p.Detail + "\n")
			}
			_ = i
		}
		b.WriteString("\n")
	}
	if len(s.FilesInspected) > 0 {
		b.WriteString("Files inspected so far:\n" + strings.Join(s.FilesInspected, "\n") + "\n\n")
	}
	for _, m := range s.Messages {
		switch m.Role {
		case RoleUser:
			b.WriteString("User:\n" + m.Content + "\n\n")
		case RoleAssistant:
			if m.Content != "" {
				b.WriteString("Assistant:\n" + m.Content + "\n\n")
			}
			for _, tc := range m.ToolCalls {
				b.WriteString("Assistant requested tool `" + tc.Name + "` with arguments:\n" + tc.Arguments + "\n\n")
			}
		case RoleTool:
			b.WriteString("Tool result (" + m.Name + "):\n" + m.Content + "\n\n")
		case RoleSystem:
			b.WriteString("System:\n" + m.Content + "\n\n")
		}
	}
	return b.String()
}
