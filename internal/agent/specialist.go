package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
	"zeuf/internal/router"
)

// SpecialistRole defines the bounded role of a subagent.
type SpecialistRole string

const (
	RoleExplorer    SpecialistRole = "explorer"
	RoleImplementer SpecialistRole = "implementer"
	RoleTester      SpecialistRole = "tester"
	RoleReviewer    SpecialistRole = "reviewer"
	RoleResearcher  SpecialistRole = "researcher"
)

// DelegationBrief is the strict, bounded work order passed to a specialist subagent.
type DelegationBrief struct {
	TaskID                  string         `json:"task_id"`
	Role                    SpecialistRole `json:"role"`
	Objective               string         `json:"objective"`
	Scope                   []string       `json:"scope"`
	Context                 string         `json:"context"`
	Constraints             []string       `json:"constraints"`
	AllowedTools            []string       `json:"allowed_tools"`
	ExpectedOutput          string         `json:"expected_output"`
	VerificationRequirement string         `json:"verification_requirement"`
}

// SpecialistResult represents the bounded outcome returned by a subagent.
type SpecialistResult struct {
	TaskID             string         `json:"task_id"`
	Role               SpecialistRole `json:"role"`
	Summary            string         `json:"summary"`
	FilesTouched       []string       `json:"files_touched,omitempty"`
	VerificationPassed bool           `json:"verification_passed"`
	Duration           time.Duration  `json:"duration"`
	ToolCallsCount     int            `json:"tool_calls_count"`
	Error              string         `json:"error,omitempty"`
}

// AllowedToolsForRole defines default allowed tools per specialist role.
func AllowedToolsForRole(role SpecialistRole) []string {
	switch role {
	case RoleExplorer:
		// Read-only investigation: no mutating tools
		return []string{"read", "grep", "glob", "git"}
	case RoleImplementer:
		// Code change: read, write, edit, and bash for builds/tests
		return []string{"read", "write", "edit", "bash", "grep", "glob", "git"}
	case RoleTester:
		// Test & reproduction: bash, read, grep
		return []string{"bash", "read", "grep", "glob"}
	case RoleReviewer:
		// Read-only inspection of diffs: git, read
		return []string{"git", "read", "grep"}
	case RoleResearcher:
		return []string{"read", "grep", "glob"}
	default:
		return []string{"read", "grep", "glob"}
	}
}

// RolePrompt returns the targeted doctrine for a specialist role.
func RolePrompt(role SpecialistRole) string {
	switch role {
	case RoleExplorer:
		return `You are Zeuf's EXPLORER subagent.
Your mission is bounded: investigate code, architecture, or reproduction steps.
CONSTRAINTS:
1. You are strictly READ-ONLY. Never attempt to edit, create, or delete files.
2. Locate relevant files and understand how components interact.
3. Keep tool calls tight and economical.
4. Return a concise, factual summary of findings and exact file paths with line numbers.`
	case RoleImplementer:
		return `You are Zeuf's IMPLEMENTER subagent.
Your mission is bounded: make surgical, minimal code edits to accomplish your objective.
CONSTRAINTS:
1. Touch ONLY files within your assigned scope. Do not refactor unrelated code.
2. Read files before editing to confirm context.
3. Match existing code style, conventions, and error handling.
4. Return a summary of what changed and why.`
	case RoleTester:
		return `You are Zeuf's TESTER subagent.
Your mission is bounded: execute tests, reproduction commands, and diagnostics.
CONSTRAINTS:
1. Run the narrowest relevant tests first, then full suites if requested.
2. Report exact commands, exit codes, and failure stack traces.
3. Do not modify source code. Report truthful, unembellished test results.`
	case RoleReviewer:
		return `You are Zeuf's REVIEWER subagent.
Your mission is bounded: review diffs and assess code quality, regressions, and security.
CONSTRAINTS:
1. Inspect git diff and modified files.
2. Check for stubs, debug prints, unintended file changes, or edge case gaps.
3. Return a concise review summary with specific line references.`
	case RoleResearcher:
		return `You are Zeuf's RESEARCHER subagent.
Your mission is bounded: investigate external library behaviors, APIs, or documentation.
CONSTRAINTS:
1. Provide accurate, verified facts.
2. Return concise findings directly relevant to the task.`
	default:
		return "You are a Zeuf specialist subagent. Complete your bounded task precisely and truthfully."
	}
}

// ScopeRegistry creates a scoped tool registry for a specialist subagent.
// Depth=1 is enforced: subagents never have access to `delegate`.
func ScopeRegistry(parent *ct.Registry, allowedTools []string, role SpecialistRole) *ct.Registry {
	scoped := ct.NewRegistry(parent.Workdir, parent.Policy)

	// Filter available tools to allowedTools
	allowedMap := make(map[string]bool)
	for _, t := range allowedTools {
		allowedMap[t] = true
	}

	for _, name := range scoped.Names() {
		// Explorer is strictly read-only: deny mutating tools even if requested
		if role == RoleExplorer && (name == "write" || name == "edit") {
			scoped.RemoveTool(name)
			continue
		}
		if !allowedMap[name] {
			scoped.RemoveTool(name)
		}
	}
	// Subagents can never delegate (depth limit 1)
	scoped.RemoveTool("delegate")

	return scoped
}

// FormatBrief formats the brief into a clear prompt for the subagent.
func FormatBrief(b DelegationBrief) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# DELEGATION BRIEF: [%s] %s\n\n", b.TaskID, b.Objective)
	if len(b.Scope) > 0 {
		fmt.Fprintf(&sb, "Target Scope: %s\n", strings.Join(b.Scope, ", "))
	}
	if b.Context != "" {
		fmt.Fprintf(&sb, "\nContext:\n%s\n", b.Context)
	}
	if len(b.Constraints) > 0 {
		fmt.Fprintf(&sb, "\nConstraints:\n")
		for _, c := range b.Constraints {
			fmt.Fprintf(&sb, "- %s\n", c)
		}
	}
	if b.VerificationRequirement != "" {
		fmt.Fprintf(&sb, "\nVerification Requirement: %s\n", b.VerificationRequirement)
	}
	if b.ExpectedOutput != "" {
		fmt.Fprintf(&sb, "\nExpected Output: %s\n", b.ExpectedOutput)
	}
	return sb.String()
}

// RunSpecialistTurn executes a bounded specialist turn with scoped tools and instructions.
func RunSpecialistTurn(
	ctx context.Context,
	brief DelegationBrief,
	r *router.Router,
	tools *ct.Registry,
	prefs router.Prefs,
	emit func(Event),
) (*SpecialistResult, error) {
	start := time.Now()
	res := &SpecialistResult{
		TaskID:   brief.TaskID,
		Role:     brief.Role,
		Duration: 0,
	}

	if tools == nil || r == nil {
		return nil, fmt.Errorf("tools and router must not be nil")
	}

	allowedTools := brief.AllowedTools
	if len(allowedTools) == 0 {
		allowedTools = AllowedToolsForRole(brief.Role)
	}

	scopedTools := ScopeRegistry(tools, allowedTools, brief.Role)
	sess := core.NewSession("sub-"+brief.TaskID, brief.Objective, RolePrompt(brief.Role))
	sess.AppendUser(FormatBrief(brief))

	maxIters := 8
	var lastContent string
	var filesTouched []string
	toolCallsCount := 0

	for iter := 0; iter < maxIters; iter++ {
		if err := ctx.Err(); err != nil {
			res.Error = err.Error()
			res.Duration = time.Since(start)
			return res, err
		}

		// Prepare tool definitions from scopedTools (no delegate tool!)
		var toolDefs []core.ToolDef
		for _, td := range scopedTools.ToolDefs() {
			if td.Name == "delegate" {
				continue // Depth 1: delegate is strictly forbidden
			}
			toolDefs = append(toolDefs, core.ToolDef{
				Name:        td.Name,
				Description: td.Description,
				Parameters:  td.Parameters,
			})
		}

		chatReq := core.ChatRequest{
			Messages: sess.Messages,
			Tools:    toolDefs,
		}
		taskReq := router.TaskReq{
			NeedTools:    len(toolDefs) > 0,
			PreferCoding: brief.Role == RoleImplementer,
			PreferReason: brief.Role == RoleExplorer || brief.Role == RoleResearcher,
		}

		resp, entry, err := r.Do(ctx, chatReq, taskReq, prefs, func(e router.Entry, req core.ChatRequest) (*core.ChatResponse, error) {
			return e.Backend.Chat(ctx, req)
		})
		if err != nil {
			res.Error = err.Error()
			res.Duration = time.Since(start)
			return res, err
		}

		if resp.Content != "" {
			lastContent = resp.Content
			if emit != nil {
				emit(Event{Type: EvAssistant, Text: resp.Content, Model: entry.Model.FullID(), Depth: 1})
			}
		}

		// If no tools requested, specialist produced its final response
		if len(resp.ToolCalls) == 0 {
			res.Summary = lastContent
			res.FilesTouched = filesTouched
			res.ToolCallsCount = toolCallsCount
			res.Duration = time.Since(start)
			return res, nil
		}

		sess.AppendAssistant(resp.Content, resp.ToolCalls)

		// Execute tool calls sequentially or in parallel
		for _, tc := range resp.ToolCalls {
			toolCallsCount++
			if emit != nil {
				emit(Event{Type: EvToolStart, Tool: tc.Name, Text: tc.Arguments, Model: entry.Model.FullID(), Depth: 1})
			}

			// Security constraint: explorer can never mutate
			if brief.Role == RoleExplorer && (tc.Name == "write" || tc.Name == "edit") {
				sess.AppendTool(tc.ID, tc.Name, "error: explorer role is read-only and cannot modify files", true)
				if emit != nil {
					emit(Event{Type: EvToolEnd, Tool: tc.Name, Text: "denied (read-only)", Ok: false, Depth: 1})
				}
				continue
			}

			toolRes, toolErr := scopedTools.Execute(ctx, tc.Name, tc.Arguments)
			content := toolRes.Content
			ok := toolErr == nil && !toolRes.IsError
			if toolErr != nil {
				content = "tool error: " + toolErr.Error()
			}

			sess.AppendTool(tc.ID, tc.Name, content, !ok)
			if emit != nil {
				emit(Event{Type: EvToolEnd, Tool: tc.Name, Text: content, Ok: ok, Depth: 1})
			}

			// Track files touched if mutating
			if tc.Name == "write" || tc.Name == "edit" {
				var p struct {
					Path string `json:"path"`
				}
				p.Path = tc.Arguments
				if p.Path != "" {
					filesTouched = append(filesTouched, p.Path)
				}
			}
		}
	}

	res.Summary = lastContent
	res.FilesTouched = filesTouched
	res.ToolCallsCount = toolCallsCount
	res.Duration = time.Since(start)
	return res, nil
}

