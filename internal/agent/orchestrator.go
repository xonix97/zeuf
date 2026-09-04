package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
	"zeuf/internal/router"
)

// Orchestrator coordinates the end-to-end autonomous engineering workflow:
// INTAKE -> DISCOVERY -> PLANNING -> SCHEDULING -> EXECUTION -> VERIFICATION -> REPLAN -> COMPLETION
type Orchestrator struct {
	Router          *router.Router
	Tools           *ct.Registry
	Planner         *Planner
	SchedulerConfig SchedulerConfig
	Hub             *Hub
	Emit            func(Event)
	Trace           *ExecutionTrace
	StateMachine    *StateMachine
	Graph           *TaskGraph
	Evidence        *Evidence
	StreamTokens    bool
	MaxIters        int

	subMu sync.Mutex
	subs  []SubInfo
}

// NewOrchestrator creates an orchestrator over a router and tool registry.
func NewOrchestrator(r *router.Router, tools *ct.Registry) *Orchestrator {
	orch := &Orchestrator{
		Router:          r,
		Tools:           tools,
		Planner:         NewPlanner(r),
		SchedulerConfig: DefaultSchedulerConfig(),
		Trace:           NewExecutionTrace(),
		StreamTokens:    true,
		MaxIters:        25,
	}
	orch.StateMachine = NewStateMachine(func(st StateTransition) {
		orch.Trace.Record("state_transition", st.To, "", "", "", 0, fmt.Sprintf("%s -> %s (%s)", st.From, st.To, st.Reason))
		orch.emit(Event{
			Type:  EvPhase,
			Phase: string(st.To),
			Text:  st.Reason,
		})
	})
	return orch
}

func (o *Orchestrator) emit(ev Event) {
	if o.Emit != nil {
		o.Emit(ev)
	}
}

// SnapshotSubs returns subagent execution records.
func (o *Orchestrator) SnapshotSubs() []SubInfo {
	o.subMu.Lock()
	defer o.subMu.Unlock()
	return append([]SubInfo(nil), o.subs...)
}

func (o *Orchestrator) recordSub(s SubInfo) {
	o.subMu.Lock()
	defer o.subMu.Unlock()
	o.subs = append(o.subs, s)
}

// Execute orchestrates one high-level software engineering task to completion.
func (o *Orchestrator) Execute(ctx context.Context, sess *Session2, prefs router.Prefs) (string, error) {
	taskText := strings.TrimSpace(sess.Task())
	if taskText == "" {
		return "", fmt.Errorf("task description is empty")
	}

	if o.Hub != nil && o.Tools != nil {
		o.Tools.Policy.Approver = o.Hub.Ask
	}

	// -------------------------------------------------------------------------
	// 1. INTAKE
	// -------------------------------------------------------------------------
	o.StateMachine.TransitionTo(StateIntake, "task received: "+taskText)
	o.Trace.Record("task_intake", StateIntake, "", "", "", 0, taskText)

	// Checkpoint workspace state for rewind
	o.Tools.BeginCheckpoint(taskText)
	defer func() {
		if cp := o.Tools.FinishCheckpoint(); cp != nil {
			sess.Session.AddCheckpoint(*cp)
		}
	}()

	// -------------------------------------------------------------------------
	// 2. DISCOVERY
	// -------------------------------------------------------------------------
	o.StateMachine.TransitionTo(StateDiscovery, "gathering repository evidence")
	evStart := time.Now()
	ev, err := Discover(ctx, o.Tools.Workdir, taskText, o.Tools)
	if err != nil {
		ev = &Evidence{Workdir: o.Tools.Workdir}
	}
	o.Evidence = ev
	sess.Session.PreExistingDirty = ev.PreExistingDirty
	o.Trace.Record("discovery_completed", StateDiscovery, "", "", "", time.Since(evStart), ev.Summary)

	// -------------------------------------------------------------------------
	// 3. PLANNING
	// -------------------------------------------------------------------------
	o.StateMachine.TransitionTo(StatePlanning, "generating structured task graph")
	planStart := time.Now()
	graph, err := o.Planner.Plan(ctx, PlanInput{
		Task:     taskText,
		Evidence: ev,
		Prefs:    prefs,
	})
	if err != nil || graph == nil {
		graph = buildDeterministicPlan(taskText, ev)
	}
	o.Graph = graph
	o.syncGraphToSession(sess, graph)
	o.Trace.Record("plan_created", StatePlanning, "", "", "", time.Since(planStart), fmt.Sprintf("%d tasks", len(graph.Tasks)))
	o.emit(Event{
		Type:  EvGraph,
		Graph: graph,
		Text:  graph.Format(),
	})

	// -------------------------------------------------------------------------
	// 4. SCHEDULING & EXECUTION
	// -------------------------------------------------------------------------
	o.StateMachine.TransitionTo(StateScheduling, "dispatching tasks via scheduler")

	executor := func(taskCtx context.Context, task *Task) (string, error) {
		o.emit(Event{
			Type:   EvPhase,
			Phase:  string(StateExecution),
			TaskID: task.ID,
			Text:   fmt.Sprintf("Starting [%s] %s (%s)", task.ID, task.Title, task.AssignedAgent),
		})
		o.Trace.Record("task_start", StateExecution, task.ID, "", "", 0, task.Title)

		start := time.Now()
		result, runErr := o.executeTask(taskCtx, task, sess, prefs)
		dur := time.Since(start)

		if runErr != nil {
			o.Trace.Record("task_failed", StateExecution, task.ID, "", "", dur, runErr.Error())
		} else {
			o.Trace.Record("task_completed", StateExecution, task.ID, "", "", dur, "success")
		}

		o.syncGraphToSession(sess, o.Graph)
		o.emit(Event{
			Type:  EvGraph,
			Graph: o.Graph,
			Text:  o.Graph.Format(),
		})
		return result, runErr
	}

	scheduler := NewScheduler(o.SchedulerConfig, o.Graph, executor)
	scheduler.OnTaskStatusChange = func(t *Task) {
		o.syncGraphToSession(sess, o.Graph)
		o.emit(Event{
			Type:   EvGraph,
			Graph:  o.Graph,
			TaskID: t.ID,
			Text:   fmt.Sprintf("[%s] %s -> %s", t.ID, t.Title, t.Status),
		})
	}

	schedErr := scheduler.Run(ctx)
	if schedErr != nil && !o.Graph.HasFailures() {
		// Cancelled or stalled
		return "", schedErr
	}

	// -------------------------------------------------------------------------
	// 5. VERIFICATION & SELF-REPAIR
	// -------------------------------------------------------------------------
	o.StateMachine.TransitionTo(StateVerification, "running verification checks")

	var verificationResults []core.VerificationResult
	testCommands := o.collectVerificationCommands(ev)

	for _, cmd := range testCommands {
		o.emit(Event{
			Type: EvVerifyStart,
			Text: cmd,
		})
		o.Trace.Record("verification_start", StateVerification, "", "", "", 0, cmd)

		vr, vErr := RunVerification(ctx, o.Tools, "global-verify", cmd)
		if vErr != nil {
			vr = &core.VerificationResult{
				Command: cmd,
				Passed:  false,
				Stderr:  vErr.Error(),
			}
		}
		verificationResults = append(verificationResults, *vr)
		sess.Session.AddVerification(*vr)
		o.Trace.Record("verification_end", StateVerification, "", "", "", vr.Duration, fmt.Sprintf("cmd: %s passed: %v", cmd, vr.Passed))

		o.emit(Event{
			Type:      EvVerifyEnd,
			Text:      cmd,
			Ok:        vr.Passed,
			Duration:  vr.Duration,
			Diagnosis: vr.FailureDiagnosis,
		})

		// Self-repair loop on verification failure
		if !vr.Passed {
			repairSuccess := o.attemptRepair(ctx, sess, prefs, vr)
			if !repairSuccess {
				// Record failed verification honestly
				o.Trace.Record("repair_failed", StateVerification, "", "", "", 0, "repair attempts exhausted or failed")
			} else {
				// Re-verify after repair
				vr2, _ := RunVerification(ctx, o.Tools, "re-verify", cmd)
				if vr2 != nil {
					verificationResults = append(verificationResults, *vr2)
					sess.Session.AddVerification(*vr2)
				}
			}
		}
	}

	// -------------------------------------------------------------------------
	// 6. COMPLETION & FINAL RESPONSE SYNTHESIS
	// -------------------------------------------------------------------------
	o.StateMachine.TransitionTo(StateCompletion, "synthesizing final response")

	diffStat, modifiedFiles := o.computeGitDiff(ctx)
	for _, mf := range modifiedFiles {
		sess.Session.NoteModifiedFile(mf)
	}

	finalReport := o.synthesizeFinalResponse(sess, o.Graph, verificationResults, diffStat, modifiedFiles)

	sess.CommitAssistant(finalReport, nil)
	o.emit(Event{
		Type:     EvDiff,
		DiffStat: diffStat,
	})
	o.emit(Event{
		Type: EvAssistant,
		Text: finalReport,
	})
	o.emit(Event{
		Type: EvDone,
	})
	o.Trace.Record("orchestration_completed", StateCompletion, "", "", "", 0, "final report generated")

	return finalReport, nil
}

func (o *Orchestrator) executeTask(ctx context.Context, task *Task, sess *Session2, prefs router.Prefs) (string, error) {
	// Delegation Decision
	role := SpecialistRole(task.AssignedAgent)
	if role == RoleExplorer || role == RoleTester || role == RoleReviewer || role == RoleResearcher || (role == RoleImplementer && len(task.AffectedPaths) > 0) {
		// Run via specialist subagent
		brief := DelegationBrief{
			TaskID:                  task.ID,
			Role:                    role,
			Objective:               task.Title,
			Context:                 task.Description,
			Scope:                   task.AffectedPaths,
			AllowedTools:            task.RequiredTools,
			VerificationRequirement: task.Verification,
		}

		o.emit(Event{
			Type:   EvSubStart,
			TaskID: task.ID,
			Role:   string(role),
			Text:   task.Title,
			Depth:  1,
		})

		subRes, err := RunSpecialistTurn(ctx, brief, o.Router, o.Tools, prefs, o.Emit)
		subInfo := SubInfo{
			Task: task.Title,
			Ok:   err == nil,
		}
		if subRes != nil {
			subInfo.Summary = subRes.Summary
			subInfo.Ms = subRes.Duration.Milliseconds()
			for _, p := range subRes.FilesTouched {
				sess.Session.NoteModifiedFile(p)
			}
		}
		o.recordSub(subInfo)

		o.emit(Event{
			Type:     EvSubEnd,
			TaskID:   task.ID,
			Role:     string(role),
			Text:     subInfo.Summary,
			Ok:       err == nil,
			Duration: time.Duration(subInfo.Ms) * time.Millisecond,
			Depth:    1,
		})

		if err != nil {
			return "", err
		}
		return subRes.Summary, nil
	}

	// Direct execution by the orchestrator loop
	return o.executeDirect(ctx, task, sess, prefs)
}

func (o *Orchestrator) executeDirect(ctx context.Context, task *Task, sess *Session2, prefs router.Prefs) (string, error) {
	taskPrompt := fmt.Sprintf("Execute Task [%s]: %s\nDetails: %s", task.ID, task.Title, task.Description)
	sess.AppendUser(taskPrompt)

	maxIters := 5
	var last string

	for iter := 0; iter < maxIters; iter++ {
		if err := ctx.Err(); err != nil {
			return last, err
		}

		resp, entry, err := o.Router.Do(ctx, sess.ChatRequest(), router.ClassifyTask(taskPrompt), prefs, func(e router.Entry, req core.ChatRequest) (*core.ChatResponse, error) {
			return e.Backend.Chat(ctx, req)
		})
		if err != nil {
			return last, err
		}

		sess.NoteModel(entry.Model.FullID())
		sess.AddUsage(resp.Usage)
		o.emit(Event{Type: EvUsage, Usage: resp.Usage, Model: entry.Model.FullID()})

		if resp.Content != "" {
			last = resp.Content
			o.emit(Event{Type: EvAssistant, Text: resp.Content, Model: entry.Model.FullID()})
		}

		if len(resp.ToolCalls) == 0 {
			return last, nil
		}

		sess.CommitAssistant(resp.Content, resp.ToolCalls)

		for _, tc := range resp.ToolCalls {
			o.emit(Event{Type: EvToolStart, Tool: tc.Name, Text: tc.Arguments, Model: entry.Model.FullID()})
			res, rerr := o.Tools.Execute(ctx, tc.Name, tc.Arguments)
			content := res.Content
			ok := rerr == nil && !res.IsError
			if rerr != nil {
				content = "tool error: " + rerr.Error()
			}
			sess.CommitTool(tc.ID, tc.Name, content)
			sess.RememberToolUse(tc.Name, tc.Arguments)
			o.emit(Event{Type: EvToolEnd, Tool: tc.Name, Text: content, Ok: ok})

			if tc.Name == "write" || tc.Name == "edit" {
				sess.Session.NoteModifiedFile(tc.Arguments)
			}
		}
	}
	return last, nil
}

func (o *Orchestrator) attemptRepair(ctx context.Context, sess *Session2, prefs router.Prefs, vr *core.VerificationResult) bool {
	o.StateMachine.TransitionTo(StateReplan, "creating bounded repair task for failure")

	// Find the failing task in the graph
	var targetTask *Task
	for _, t := range o.Graph.TasksList() {
		if t.Status == TaskCompleted || t.Status == TaskFailed {
			targetTask = t
		}
	}
	if targetTask == nil {
		return false
	}

	repairTask, err := CreateRepairTask(targetTask, vr, sess.Session.ModifiedFiles)
	if err != nil {
		o.emit(Event{
			Type: EvError,
			Text: fmt.Sprintf("Repair limit: %v", err),
		})
		return false
	}

	o.emit(Event{
		Type:   EvPhase,
		Phase:  string(StateReplan),
		TaskID: repairTask.ID,
		Text:   fmt.Sprintf("Created repair task [%s] to fix verification failure", repairTask.ID),
	})

	if err := IngestRepairTask(o.Graph, targetTask, repairTask); err != nil {
		return false
	}

	o.syncGraphToSession(sess, o.Graph)

	// Execute repair task
	_, repErr := o.executeTask(ctx, repairTask, sess, prefs)
	if repErr != nil {
		repairTask.Status = TaskFailed
		return false
	}
	repairTask.Status = TaskCompleted
	return true
}

func (o *Orchestrator) collectVerificationCommands(ev *Evidence) []string {
	var cmds []string
	seen := make(map[string]bool)

	// Add task-specific verification commands
	for _, t := range o.Graph.TasksList() {
		clean := strings.TrimSpace(t.Verification)
		if clean != "" && !seen[clean] {
			seen[clean] = true
			cmds = append(cmds, clean)
		}
	}

	// Add repository-level test command if discovered
	if ev != nil && ev.TestCommand != "" && !seen[ev.TestCommand] {
		seen[ev.TestCommand] = true
		cmds = append(cmds, ev.TestCommand)
	}

	return cmds
}

func (o *Orchestrator) computeGitDiff(ctx context.Context) (diffStat string, modified []string) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cctx, "git", "-C", o.Tools.Workdir, "diff", "--stat").Output()
	if err == nil {
		diffStat = strings.TrimSpace(string(out))
	}

	statusOut, err := exec.CommandContext(cctx, "git", "-C", o.Tools.Workdir, "status", "--porcelain").Output()
	if err == nil {
		for _, ln := range strings.Split(strings.TrimSpace(string(statusOut)), "\n") {
			parts := strings.Fields(ln)
			if len(parts) >= 2 {
				modified = append(modified, parts[1])
			}
		}
	}
	return diffStat, modified
}

func (o *Orchestrator) synthesizeFinalResponse(
	sess *Session2,
	graph *TaskGraph,
	verifications []core.VerificationResult,
	diffStat string,
	modifiedFiles []string,
) string {
	var b strings.Builder
	latestStatus := make(map[string]bool)
	for _, v := range verifications {
		latestStatus[v.Command] = v.Passed
	}
	allPassed := true
	for _, passed := range latestStatus {
		if !passed {
			allPassed = false
			break
		}
	}

	if allPassed && !graph.HasFailures() {
		fmt.Fprintf(&b, "### Task Outcome: Verified Complete\n\n")
	} else {
		fmt.Fprintf(&b, "### Task Outcome: Incomplete / Verification Issues\n\n")
	}

	// Task summary
	b.WriteString("#### Execution Plan\n")
	for _, id := range graph.Order {
		t := graph.Tasks[id]
		mark := "✓"
		if t.Status != TaskCompleted {
			mark = "✗"
		}
		agentName := ""
		if t.AssignedAgent != "" {
			agentName = fmt.Sprintf(" *(%s)*", t.AssignedAgent)
		}
		fmt.Fprintf(&b, "- %s **[%s]** %s%s\n", mark, t.ID, t.Title, agentName)
	}
	b.WriteString("\n")

	// Verification section
	if len(verifications) > 0 {
		b.WriteString("#### Verification Results\n")
		for _, v := range verifications {
			mark := "✓ PASSED"
			if !v.Passed {
				mark = fmt.Sprintf("✗ FAILED (exit code %d)", v.ExitCode)
			}
			fmt.Fprintf(&b, "- `%s` — %s (%s)\n", v.Command, mark, v.Duration.Round(time.Millisecond))
			if !v.Passed && v.FailureDiagnosis != "" {
				fmt.Fprintf(&b, "  ```\n  %s\n  ```\n", v.FailureDiagnosis)
			}
		}
		b.WriteString("\n")
	}

	// Modified files & diffstat
	if diffStat != "" {
		b.WriteString("#### Repository Changes\n")
		b.WriteString("```text\n")
		b.WriteString(diffStat + "\n")
		b.WriteString("```\n")
	} else if len(modifiedFiles) > 0 {
		b.WriteString("#### Modified Files\n")
		for _, f := range modifiedFiles {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (o *Orchestrator) syncGraphToSession(sess *Session2, graph *TaskGraph) {
	if graph == nil || sess == nil {
		return
	}
	sess.SetPlan(graph.ToPlanSteps())
	if data, err := graph.ToJSON(); err == nil {
		sess.Session.SetTaskGraphData(data)
	}
}
