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
	ID             string            `json:"id"`
	Task           string            `json:"task"`
	SystemPrompt   string            `json:"system_prompt"`
	Messages       []Message         `json:"messages"`
	Plan           []PlanStep        `json:"plan"`
	FilesInspected []string          `json:"files_inspected"`
	PendingTools   []ToolCall        `json:"pending_tools,omitempty"`
	TokensIn       int64             `json:"tokens_in"`
	TokensOut      int64             `json:"tokens_out"`
	SwitchTrail    []string          `json:"switch_trail"` // model FullIDs used, in order
	Meta           map[string]string `json:"meta,omitempty"`
	Created        time.Time         `json:"created"`
	Updated        time.Time         `json:"updated"`
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

// Snapshot returns a deep copy for tests / safe handoff.
func (s *Session) Snapshot() *Session {
	cp := *s
	cp.Messages = append([]Message(nil), s.Messages...)
	cp.Plan = append([]PlanStep(nil), s.Plan...)
	cp.FilesInspected = append([]string(nil), s.FilesInspected...)
	cp.PendingTools = append([]ToolCall(nil), s.PendingTools...)
	cp.SwitchTrail = append([]string(nil), s.SwitchTrail...)
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
