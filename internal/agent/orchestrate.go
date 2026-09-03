package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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
// Subagent events carry Depth so UIs render them nested, and every finish
// is recorded for /agents.
func DelegateTool(parent *Agent, prefs router.Prefs) ct.Tool {
	depth := parent.Depth
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
			start := time.Now()
			sub := New(parent.Router, parent.Tools)
			sub.MaxIters = 8
			sub.StreamTokens = false
			sub.Depth = depth + 1
			sub.Emit = func(ev Event) {
				ev.Depth = depth + 1
				parent.emit(ev) // nested progress stays visible, marked
			}
			sess := NewSession("sub-"+a.Task[:min(12, len(a.Task))], a.Task, parent.Tools)
			sess.ExcludeTools = map[string]bool{"delegate": true}
			if a.Context != "" {
				sess.AppendUser("Background context:\n" + a.Context)
			}
			sess.AppendUser(a.Task)
			out, err := sub.runDepth(ctx, sess, prefs, depth+1)
			info := SubInfo{Task: a.Task, Ms: time.Since(start).Milliseconds(), Ok: err == nil}
			if trail := sess.Session.SwitchTrail; len(trail) > 0 {
				info.Model = trail[len(trail)-1]
			}
			if err != nil {
				info.Summary = "failed: " + core.Redact(err.Error())
				parent.recordSub(info)
				return ct.Result{Content: fmt.Sprintf("subagent failed: %v", core.Redact(err.Error())), IsError: true}, nil
			}
			if out == "" {
				out = "(subagent produced no summary)"
			}
			info.Summary = truncRunesLocal(out, 200)
			parent.recordSub(info)
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

func truncRunesLocal(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
