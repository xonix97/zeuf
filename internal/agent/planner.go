package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"zeuf/internal/core"
	"zeuf/internal/router"
)

// Planner builds validated TaskGraphs from tasks and repository evidence.
type Planner struct {
	Router *router.Router
}

// NewPlanner creates a planner over the given model router.
func NewPlanner(r *router.Router) *Planner {
	return &Planner{Router: r}
}

// PlanInput is passed into the planner.
type PlanInput struct {
	Task     string
	Evidence *Evidence
	Prefs    router.Prefs
}

// rawPlanOutput matches the JSON output from the model.
type rawPlanOutput struct {
	Goal  string        `json:"goal"`
	Tasks []rawPlanTask `json:"tasks"`
}

type rawPlanTask struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Dependencies  []string `json:"dependencies"`
	AssignedAgent string   `json:"assigned_agent"`
	RequiredTools []string `json:"required_tools"`
	AffectedPaths []string `json:"affected_paths"`
	Verification  string   `json:"verification"`
}

// Plan creates a validated task graph for the given user task.
func (p *Planner) Plan(ctx context.Context, in PlanInput) (*TaskGraph, error) {
	taskTrim := strings.TrimSpace(in.Task)
	if taskTrim == "" {
		return nil, fmt.Errorf("task is empty")
	}

	// 1. Check for simple / fast-path task
	if isSimpleTask(taskTrim) {
		return buildFastPathGraph(taskTrim, in.Evidence), nil
	}

	// 2. Structured planning query with model
	if p.Router != nil {
		g, err := p.queryModelPlan(ctx, in)
		if err == nil && g != nil && len(g.Tasks) > 0 {
			if valErr := g.Validate(); valErr == nil {
				return g, nil
			}
		}
	}

	// 3. Deterministic structured fallback plan
	return buildDeterministicPlan(taskTrim, in.Evidence), nil
}

func isSimpleTask(task string) bool {
	lower := strings.ToLower(task)
	// Query / explanation
	if strings.HasPrefix(lower, "what ") || strings.HasPrefix(lower, "how ") ||
		strings.HasPrefix(lower, "explain ") || strings.HasPrefix(lower, "where ") ||
		strings.HasPrefix(lower, "why ") {
		return true
	}
	// Short inspection / quick single-file fix
	words := strings.Fields(task)
	if len(words) <= 5 && (strings.Contains(lower, "typo") || strings.Contains(lower, "rename") || strings.Contains(lower, "check")) {
		return true
	}
	return false
}

func buildFastPathGraph(task string, ev *Evidence) *TaskGraph {
	g := NewTaskGraph(task)
	testCmd := ""
	if ev != nil {
		testCmd = ev.TestCommand
	}

	// T1: Inspect / locate
	g.AddTask(&Task{
		ID:            "T1",
		Title:         "Inspect repository and target context",
		Description:   fmt.Sprintf("Locate and read relevant files for: %s", task),
		AssignedAgent: "explorer",
		RequiredTools: []string{"read", "grep", "glob"},
		Status:        TaskReady,
	})

	// T2: Execute change or answer
	g.AddTask(&Task{
		ID:            "T2",
		Title:         "Implement solution",
		Description:   task,
		AssignedAgent: "implementer",
		Dependencies:  []string{"T1"},
		Status:        TaskPending,
		Verification:  testCmd,
	})

	// T3: Verify if test command available
	if testCmd != "" {
		g.AddTask(&Task{
			ID:            "T3",
			Title:         "Run verification tests",
			Description:   fmt.Sprintf("Verify change with: %s", testCmd),
			AssignedAgent: "tester",
			Dependencies:  []string{"T2"},
			Status:        TaskPending,
			Verification:  testCmd,
		})
	}
	return g
}

func buildDeterministicPlan(task string, ev *Evidence) *TaskGraph {
	g := NewTaskGraph(task)
	testCmd := ""
	var affectedPaths []string
	if ev != nil {
		testCmd = ev.TestCommand
		affectedPaths = ev.RelevantFiles
	}

	// T1: Investigate
	g.AddTask(&Task{
		ID:            "T1",
		Title:         "Investigate repository and dependencies",
		Description:   fmt.Sprintf("Inspect code and reproduce issue for: %s", task),
		AssignedAgent: "explorer",
		RequiredTools: []string{"read", "grep", "glob", "git"},
		AffectedPaths: affectedPaths,
		Status:        TaskReady,
	})

	// T2: Implement changes
	g.AddTask(&Task{
		ID:            "T2",
		Title:         "Implement required changes",
		Description:   fmt.Sprintf("Apply minimal, surgical edits to satisfy: %s", task),
		AssignedAgent: "implementer",
		Dependencies:  []string{"T1"},
		RequiredTools: []string{"read", "write", "edit", "bash"},
		AffectedPaths: affectedPaths,
		Status:        TaskPending,
		Verification:  testCmd,
	})

	// T3: Verification & test
	g.AddTask(&Task{
		ID:            "T3",
		Title:         "Verify changes",
		Description:   "Run test suite and verify no regressions",
		AssignedAgent: "tester",
		Dependencies:  []string{"T2"},
		RequiredTools: []string{"bash"},
		Status:        TaskPending,
		Verification:  testCmd,
	})
	return g
}

var jsonBlockRe = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

func (p *Planner) queryModelPlan(ctx context.Context, in PlanInput) (*TaskGraph, error) {
	evidenceText := ""
	if in.Evidence != nil && in.Evidence.Summary != "" {
		evidenceText = "\nRepository Evidence:\n" + in.Evidence.Summary
	}

	prompt := fmt.Sprintf(`You are Zeuf's planning engine. Decompose the following software-engineering task into a structured, executable task graph.

Task: %s%s

Rules:
1. Decompose into 2-5 concrete, verifiable tasks.
2. Assign each task a specialist role:
   - "explorer": Read-only code and architecture investigation (no file writes).
   - "implementer": Bounded code modifications (write, edit).
   - "tester": Running test suites, reproductions, and benchmarks.
   - "reviewer": Diff inspection and sanity checks.
   - "researcher": External library or API behavior investigation.
3. Independent tasks (e.g. parallel investigations) MUST NOT have dependencies on each other.
4. Dependent tasks (e.g. implementation after investigation) MUST explicitly declare prerequisites in "dependencies".
5. Specify "affected_paths" for any files or directories a task reads or writes.
6. Provide a concrete "verification" command (e.g. "go test ./...") whenever possible.

You MUST reply with ONLY valid JSON matching this schema:
{
  "goal": "string",
  "tasks": [
    {
      "id": "T1",
      "title": "short descriptive title",
      "description": "detailed instructions",
      "dependencies": [],
      "assigned_agent": "explorer|implementer|tester|reviewer|researcher",
      "required_tools": ["read", "grep"],
      "affected_paths": ["path/to/component"],
      "verification": "command or check"
    }
  ]
}`, in.Task, evidenceText)

	req := core.ChatRequest{
		Messages: []core.Message{
			{Role: core.RoleSystem, Content: "You are an expert software engineering planner. You output strictly machine-readable JSON task graphs without conversational filler."},
			{Role: core.RoleUser, Content: prompt},
		},
		Temperature: 0.1,
	}

	taskReq := router.TaskReq{NeedTools: false, PreferReason: true}
	resp, _, err := p.Router.Do(ctx, req, taskReq, in.Prefs, func(e router.Entry, r core.ChatRequest) (*core.ChatResponse, error) {
		return e.Backend.Chat(ctx, r)
	})
	if err != nil {
		return nil, err
	}

	return parsePlanJSON(resp.Content, in.Task)
}

func parsePlanJSON(content string, defaultGoal string) (*TaskGraph, error) {
	jsonText := strings.TrimSpace(content)
	if match := jsonBlockRe.FindStringSubmatch(jsonText); len(match) > 1 {
		jsonText = strings.TrimSpace(match[1])
	} else {
		// Try to find { ... }
		start := strings.Index(jsonText, "{")
		end := strings.LastIndex(jsonText, "}")
		if start >= 0 && end > start {
			jsonText = jsonText[start : end+1]
		}
	}

	var raw rawPlanOutput
	if err := json.Unmarshal([]byte(jsonText), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse plan JSON: %w", err)
	}

	if len(raw.Tasks) == 0 {
		return nil, fmt.Errorf("plan JSON returned zero tasks")
	}

	goal := raw.Goal
	if goal == "" {
		goal = defaultGoal
	}

	g := NewTaskGraph(goal)
	for _, rt := range raw.Tasks {
		if rt.ID == "" {
			continue
		}
		agentRole := rt.AssignedAgent
		if agentRole == "" {
			agentRole = "implementer"
		}
		g.AddTask(&Task{
			ID:            rt.ID,
			Title:         rt.Title,
			Description:   rt.Description,
			Dependencies:  rt.Dependencies,
			AssignedAgent: agentRole,
			RequiredTools: rt.RequiredTools,
			AffectedPaths: rt.AffectedPaths,
			Verification:  rt.Verification,
		})
	}

	if err := g.Validate(); err != nil {
		return nil, err
	}
	return g, nil
}
