// Package agent implements Zeuf's coding-agent runtime: the iterative
// loop of model turn → structured tool execution → next turn. It owns
// context, planning state, permissions and conversation state. Model
// selection and fallback come from the router; when the backend changes
// mid-task the session is preserved and rebuilt for the next model.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
	"zeuf/internal/router"
)

// SystemPrompt lives in prompt.go: the full operating doctrine.
// Event is a UI-facing happenstance of the loop.
type Event struct {
	Type     EventType
	Text     string
	Tool     string
	Model    string
	Usage    core.Usage
	Switched *router.SwitchInfo
	// Ok reports tool success for EvToolEnd.
	Ok bool
	// Depth marks subagent-nested events (0 = orchestrator itself).
	Depth int
}

// EventType enumerates agent events.
type EventType string

const (
	EvToken     EventType = "token"
	EvReasoning EventType = "reasoning"
	EvToolStart EventType = "tool_start"
	EvToolEnd   EventType = "tool_end"
	EvAssistant EventType = "assistant"
	EvSwitch    EventType = "model_switch"
	EvUsage     EventType = "usage"
	EvError     EventType = "error"
	EvDone      EventType = "done"
)

// Agent drives turns over a router and a tool registry.
type Agent struct {
	Router   *router.Router
	Tools    *ct.Registry
	MaxIters int
	Emit     func(Event)

	// StreamTokens enables token streaming for native backends.
	StreamTokens bool
	// Hub routes approval requests to the UI. Required in TUI mode.
	Hub *Hub
	// Depth is the delegation depth (0 = orchestrator). Subagents are
	// built without the delegate tool.
	Depth int
	// Prefs snapshot used for delegated subagents.
	Prefs router.Prefs

	subMu sync.Mutex
	subs  []SubInfo
}

// SubInfo records one finished subagent for /agents and review.
type SubInfo struct {
	Task    string
	Summary string
	Model   string
	Ms      int64
	Ok      bool
}

// SnapshotSubs returns the subagent records so far.
func (a *Agent) SnapshotSubs() []SubInfo {
	a.subMu.Lock()
	defer a.subMu.Unlock()
	return append([]SubInfo(nil), a.subs...)
}

func (a *Agent) recordSub(s SubInfo) {
	a.subMu.Lock()
	defer a.subMu.Unlock()
	a.subs = append(a.subs, s)
}

// New builds an agent.
func New(r *router.Router, tools *ct.Registry) *Agent {
	return &Agent{Router: r, Tools: tools, MaxIters: 25, StreamTokens: true}
}

func (a *Agent) emit(ev Event) {
	if a.Emit != nil {
		a.Emit(ev)
	}
}

// RunTurn executes one user task against sess, preserving sess across any
// number of model switches. It returns the assistant's final text.
func (a *Agent) RunTurn(ctx context.Context, sess *Session2, prefs router.Prefs) (string, error) {
	a.Prefs = prefs
	if _, ok := a.Tools.Get("delegate"); !ok && a.Depth < maxDelegateDepth {
		a.Tools.AddTool(DelegateTool(a, prefs))
	}
	if a.Hub != nil {
		a.Tools.Policy.Approver = a.Hub.Ask
	}
	return a.runDepth(ctx, sess, prefs, a.Depth)
}

// runDepth is the iterative orchestration loop.
func (a *Agent) runDepth(ctx context.Context, sess *Session2, prefs router.Prefs, depth int) (string, error) {
	task := router.ClassifyTask(sess.Task())
	maxIters := a.MaxIters
	if maxIters <= 0 {
		maxIters = 25
	}
	var last string
	for iter := 0; iter < maxIters; iter++ {
		if err := ctx.Err(); err != nil {
			return last, err
		}
		resp, entry, err := a.turn(ctx, sess, task, prefs)
		if err != nil {
			a.emit(Event{Type: EvError, Text: core.Redact(err.Error())})
			return last, err
		}
		sess.NoteModel(entry.Model.FullID())
		sess.AddUsage(resp.Usage)
		a.emit(Event{Type: EvUsage, Usage: resp.Usage, Model: entry.Model.FullID()})
		if resp.Content != "" {
			last = resp.Content
		}
		if len(resp.ToolCalls) == 0 {
			sess.CommitAssistant(resp.Content, nil)
			a.emit(Event{Type: EvAssistant, Text: resp.Content, Model: entry.Model.FullID()})
			a.emit(Event{Type: EvDone, Model: entry.Model.FullID()})
			return last, nil
		}
		// Tool loop for native backends. Independent calls from one block
		// execute in parallel; results commit in order.
		sess.CommitAssistant(resp.Content, resp.ToolCalls)
		type tres struct {
			idx int
			res string
		}
		out := make([]tres, len(resp.ToolCalls))
		var wg sync.WaitGroup
		for i, tc := range resp.ToolCalls {
			wg.Add(1)
			go func() {
				defer wg.Done()
				a.emit(Event{Type: EvToolStart, Tool: tc.Name, Text: tc.Arguments, Model: entry.Model.FullID()})
				res, rerr := a.Tools.Execute(ctx, tc.Name, tc.Arguments)
				content := res.Content
				ok := rerr == nil && !res.IsError
				if rerr != nil {
					content = "tool framework error: " + rerr.Error()
				}
				out[i] = tres{idx: i, res: content}
				a.emit(Event{Type: EvToolEnd, Tool: tc.Name, Text: content, Ok: ok})
			}()
		}
		wg.Wait()
		for i, tc := range resp.ToolCalls {
			sess.CommitTool(tc.ID, tc.Name, out[i].res)
			sess.RememberToolUse(tc.Name, tc.Arguments)
		}
		a.syncPlan(sess)
	}
	return last, fmt.Errorf("max iterations (%d) reached without a final answer", maxIters)
}

// turn runs a single model inference with fallback, streaming tokens when
// the backend supports it. Delegated backends get the full transcript so
// they continue the same session; native backends get structured messages
// plus live tools.
func (a *Agent) turn(ctx context.Context, sess *Session2, task router.TaskReq, prefs router.Prefs) (*core.ChatResponse, router.Entry, error) {
	if a.StreamTokens {
		resp, entry, err := a.Router.DoStream(ctx,
			sess.ChatRequest(),
			task, prefs,
			func(e router.Entry, req core.ChatRequest) (<-chan core.StreamEvent, error) {
				return e.Backend.Stream(ctx, req)
			},
			func(ch <-chan core.StreamEvent) (*core.ChatResponse, error) {
				return a.consume(ctx, ch, sess)
			},
		)
		if err == nil {
			return resp, entry, nil
		}
		// Streaming unsupported or failed pre-flight: fall back to Chat.
		if perr, ok := err.(*core.ProviderError); ok && perr.Code == core.ErrUnsupported {
			return a.Router.Do(ctx, sess.ChatRequest(), task, prefs,
				func(e router.Entry, req core.ChatRequest) (*core.ChatResponse, error) {
					return e.Backend.Chat(ctx, req)
				})
		}
		return resp, entry, err
	}
	return a.Router.Do(ctx, sess.ChatRequest(), task, prefs,
		func(e router.Entry, req core.ChatRequest) (*core.ChatResponse, error) {
			return e.Backend.Chat(ctx, req)
		})
}

// consume folds a stream into a response, emitting token/tool events and
// surfacing mid-stream provider errors (which the router then falls back on).
func (a *Agent) consume(ctx context.Context, ch <-chan core.StreamEvent, sess *Session2) (*core.ChatResponse, error) {
	out := &core.ChatResponse{}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				return out, nil
			}
			switch ev.Type {
			case core.EventToken:
				out.Content += ev.Delta
				a.emit(Event{Type: EvToken, Text: ev.Delta})
			case core.EventReasoning:
				a.emit(Event{Type: EvReasoning, Text: ev.Delta})
			case core.EventToolProgress:
				// Delegated server-side tool use: display only, never
				// executed locally (it already ran behind the gateway).
				if ev.Done {
					a.emit(Event{Type: EvToolEnd, Tool: ev.Tool, Text: ev.Delta, Ok: ev.Ok})
				} else {
					a.emit(Event{Type: EvToolStart, Tool: ev.Tool, Text: ev.Delta})
				}
			case core.EventTool:
				out.ToolCalls = append(out.ToolCalls, ev.ToolCalls...)
			case core.EventDone:
				out.Usage = ev.Usage
				return out, nil
			case core.EventError:
				if ev.Err != nil {
					return nil, ev.Err
				}
				return nil, &core.ProviderError{Code: core.ErrUnknown, Message: "stream failed"}
			case core.EventInfo:
				if ev.Delta != "" {
					a.emit(Event{Type: EvAssistant, Text: ev.Delta})
				}
			}
		}
	}
}

// syncPlan copies plan-tool state into the session plan.
func (a *Agent) syncPlan(sess *Session2) {
	steps := a.Tools.PlanSteps()
	plan := make([]core.PlanStep, 0, len(steps))
	for _, s := range steps {
		plan = append(plan, core.PlanStep{Title: s.Title, Detail: s.Detail, Done: s.Done})
	}
	sess.SetPlan(plan)
}

// ---- Session adapter -------------------------------------------------------
// Session2 wraps core.Session with agent conveniences (tool defs + request
// building). It embeds *core.Session so all state stays in one place and
// survives model switches untouched.

// Session2 is the agent-facing session handle.
type Session2 struct {
	*core.Session
	tools *ct.Registry
	// ExcludeTools hides registry tools from this session's requests
	// (subagents never see `delegate`, capping nesting at depth 1).
	ExcludeTools map[string]bool
}

// NewSession creates a session and binds tool definitions.
func NewSession(id, task string, tools *ct.Registry) *Session2 {
	return &Session2{Session: core.NewSession(id, task, SystemPrompt), tools: tools}
}

func toCoreDefs(tools *ct.Registry) []core.ToolDef {
	if tools == nil {
		return nil
	}
	var out []core.ToolDef
	for _, d := range tools.ToolDefs() {
		out = append(out, core.ToolDef{Name: d.Name, Description: d.Description, Parameters: d.Parameters})
	}
	return out
}

// Task returns the task text.
func (s *Session2) Task() string { return s.Session.Task }

// NoteModel records backend use in the switch trail.
func (s *Session2) NoteModel(fullID string) {
	trail := s.Session.SwitchTrail
	if len(trail) == 0 || trail[len(trail)-1] != fullID {
		s.Session.NoteModelSwitch(fullID)
	}
}

// CommitAssistant records the model's turn.
func (s *Session2) CommitAssistant(content string, calls []core.ToolCall) {
	s.Session.AppendAssistant(content, calls)
}

// CommitTool records a tool result.
func (s *Session2) CommitTool(callID, name, content string) {
	s.Session.AppendTool(callID, name, content, false)
}

// SetPlan replaces the plan.
func (s *Session2) SetPlan(p []core.PlanStep) {
	s.Session.Plan = p
}

// RememberToolUse tracks inspected files for context continuity.
func (s *Session2) RememberToolUse(name, argsJSON string) {
	if name != "read" && name != "glob" && name != "grep" {
		return
	}
	var a struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if json.Unmarshal([]byte(argsJSON), &a) == nil {
		if a.Path != "" {
			s.Session.NoteFile(a.Path)
		}
		if a.Pattern != "" {
			s.Session.NoteFile("glob:" + a.Pattern)
		}
	}
}

// ChatRequest builds the provider-agnostic request for the next turn.
// Tool definitions are read live from the registry, so orchestrator
// tools (delegate) registered at turn start are offered immediately.
func (s *Session2) ChatRequest() core.ChatRequest {
	msgs := []core.Message{{Role: core.RoleSystem, Content: s.Session.SystemPrompt}}
	msgs = append(msgs, s.Session.Messages...)
	defs := toCoreDefs(s.tools)
	if len(s.ExcludeTools) > 0 {
		kept := defs[:0]
		for _, d := range defs {
			if !s.ExcludeTools[d.Name] {
				kept = append(kept, d)
			}
		}
		defs = kept
	}
	return core.ChatRequest{Messages: msgs, Tools: defs, MaxTokens: 4096}
}

// Summary returns a short human description for UIs.
func (s *Session2) Summary() string {
	done, total := 0, len(s.Session.Plan)
	for _, p := range s.Session.Plan {
		if p.Done {
			done++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "task: %s | turns: %d | plan: %d/%d | backends: %s",
		trunc(s.Session.Task, 60), len(s.Session.Messages), done, total,
		strings.Join(s.Session.SwitchTrail, " → "))
	return b.String()
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
