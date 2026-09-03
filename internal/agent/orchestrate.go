package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
	"zeuf/internal/router"
)

// maxDelegateDepth caps subagent nesting: the orchestrator may delegate,
// subagents may not (they return to the orchestrator instead).
const maxDelegateDepth = 1

// delegateToolDef describes the orchestrator's subagent tool.
const delegateToolDef = `{"type":"object","properties":{"task":{"type":"string"},"context":{"type":"string"}},"required":["task"]}`

// DelegateTool builds the `delegate` tool: hand a self-contained subtask to
// a subagent sharing the router and policy. It returns the subagent's
// final summary, which the orchestrator folds into its own context.
// Independent subtasks can be delegated in parallel via multiple calls.
func DelegateTool(r *router.Router, tools *ct.Registry, prefs router.Prefs, depth int, emit func(Event)) ct.Tool {
	return ct.Tool{
		Name:        "delegate",
		Description: "Delegate a self-contained subtask to a subagent (research, exploration, isolated edits). Returns its final summary. Prefer this for parallelizable work; keep the overall plan yourself.",
		Parameters:  delegateToolDef,
		Run: func(ctx context.Context, argsJSON string) (ct.Result, error) {
			if depth >= maxDelegateDepth {
				return ct.Result{Content: "delegation depth exceeded; do the subtask yourself", IsError: true}, nil
			}
			var a struct {
				Task    string `json:"task"`
				Context string `json:"context"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &a); err != nil || a.Task == "" {
				return ct.Result{Content: "delegate needs a task string", IsError: true}, nil
			}
			sub := New(r, tools)
			sub.MaxIters = 8
			sub.StreamTokens = false
			sub.Emit = emit // nested progress stays visible
			sess := NewSession("sub-"+a.Task[:min(12, len(a.Task))], a.Task, tools)
			sess.ExcludeTools = map[string]bool{"delegate": true}
			if a.Context != "" {
				sess.AppendUser("Background context:\n" + a.Context)
			}
			sess.AppendUser(a.Task)
			out, err := sub.runDepth(ctx, sess, prefs, depth+1)
			if err != nil {
				return ct.Result{Content: fmt.Sprintf("subagent failed: %v", core.Redact(err.Error())), IsError: true}, nil
			}
			if out == "" {
				out = "(subagent produced no summary)"
			}
			return ct.Result{Content: "Subagent result:\n" + out}, nil
		},
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
