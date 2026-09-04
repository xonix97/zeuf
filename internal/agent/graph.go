package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"zeuf/internal/core"
)

// TaskStatus represents the lifecycle state of a task in the graph.
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskReady     TaskStatus = "ready"
	TaskRunning   TaskStatus = "running"
	TaskBlocked   TaskStatus = "blocked"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// Task represents a single unit of work in the orchestration task graph.
type Task struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Status        TaskStatus `json:"status"`
	Dependencies  []string   `json:"dependencies"`
	AssignedAgent string     `json:"assigned_agent"` // explorer, implementer, tester, reviewer, researcher, orchestrator
	RequiredTools []string   `json:"required_tools,omitempty"`
	AffectedPaths []string   `json:"affected_paths,omitempty"`
	Result        string     `json:"result,omitempty"`
	Verification  string     `json:"verification,omitempty"`
	AttemptCount  int        `json:"attempt_count"`
	MaxAttempts   int        `json:"max_attempts"`
	Error         string     `json:"error,omitempty"`
}

// TaskGraph represents a directed acyclic graph of tasks.
type TaskGraph struct {
	mu    sync.RWMutex
	Goal  string           `json:"goal"`
	Tasks map[string]*Task `json:"tasks"`
	Order []string         `json:"order"` // insertion order of task IDs
}

// NewTaskGraph creates an empty task graph with a high-level goal.
func NewTaskGraph(goal string) *TaskGraph {
	return &TaskGraph{
		Goal:  goal,
		Tasks: make(map[string]*Task),
		Order: make([]string, 0),
	}
}

// AddTask adds a task to the graph. Returns an error if the ID already exists.
func (g *TaskGraph) AddTask(t *Task) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if t == nil || t.ID == "" {
		return fmt.Errorf("task and task ID cannot be empty")
	}
	if _, exists := g.Tasks[t.ID]; exists {
		return fmt.Errorf("task with ID %q already exists", t.ID)
	}
	if t.Status == "" {
		t.Status = TaskPending
	}
	if t.MaxAttempts <= 0 {
		t.MaxAttempts = 3
	}
	g.Tasks[t.ID] = t
	g.Order = append(g.Order, t.ID)
	return nil
}

// GetTask returns a task by ID.
func (g *TaskGraph) GetTask(id string) (*Task, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	t, ok := g.Tasks[id]
	return t, ok
}

// TasksList returns all tasks in insertion order.
func (g *TaskGraph) TasksList() []*Task {
	g.mu.RLock()
	defer g.mu.RUnlock()
	res := make([]*Task, 0, len(g.Order))
	for _, id := range g.Order {
		if t, ok := g.Tasks[id]; ok {
			res = append(res, t)
		}
	}
	return res
}

// Validate checks that the graph has no cycles and all dependencies exist.
func (g *TaskGraph) Validate() error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.Tasks) == 0 {
		return nil
	}

	// 1. Verify all dependencies exist.
	inDegree := make(map[string]int)
	adj := make(map[string][]string)

	for id := range g.Tasks {
		inDegree[id] = 0
		adj[id] = nil
	}

	for id, t := range g.Tasks {
		for _, dep := range t.Dependencies {
			if _, exists := g.Tasks[dep]; !exists {
				return fmt.Errorf("task %q depends on non-existent task %q", id, dep)
			}
			if dep == id {
				return fmt.Errorf("task %q cannot depend on itself (self-cycle)", id)
			}
			adj[dep] = append(adj[dep], id)
			inDegree[id]++
		}
	}

	// 2. Cycle detection via Kahn's algorithm (topological sort).
	queue := make([]string, 0)
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	visitedCount := 0
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		visitedCount++

		for _, neighbor := range adj[curr] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if visitedCount != len(g.Tasks) {
		return fmt.Errorf("dependency cycle detected in task graph")
	}

	return nil
}

// ReadyTasks returns all tasks whose dependencies are fully completed and are ready to run.
func (g *TaskGraph) ReadyTasks() []*Task {
	g.mu.Lock()
	defer g.mu.Unlock()

	var ready []*Task
	for _, id := range g.Order {
		t := g.Tasks[id]
		if t.Status != TaskPending && t.Status != TaskReady {
			continue
		}

		depsMet := true
		depFailed := false
		for _, depID := range t.Dependencies {
			dep, ok := g.Tasks[depID]
			if !ok || dep.Status != TaskCompleted {
				depsMet = false
			}
			if ok && (dep.Status == TaskFailed || dep.Status == TaskCancelled) {
				depFailed = true
			}
		}

		if depFailed {
			t.Status = TaskBlocked
			t.Error = "prerequisite task failed or cancelled"
			continue
		}

		if depsMet {
			t.Status = TaskReady
			ready = append(ready, t)
		} else {
			t.Status = TaskPending
		}
	}
	return ready
}

// BlockedTasks returns all pending tasks blocked on dependencies.
func (g *TaskGraph) BlockedTasks() []*Task {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var blocked []*Task
	for _, id := range g.Order {
		t := g.Tasks[id]
		if t.Status == TaskBlocked || t.Status == TaskPending {
			for _, depID := range t.Dependencies {
				dep, ok := g.Tasks[depID]
				if !ok || dep.Status != TaskCompleted {
					blocked = append(blocked, t)
					break
				}
			}
		}
	}
	return blocked
}

// DetectConflict returns true if task's affected paths overlap with any active task.
// Read-only agents (e.g. explorer) do not conflict unless an active task is mutating those paths.
func (g *TaskGraph) DetectConflict(task *Task, active []*Task) bool {
	if len(task.AffectedPaths) == 0 || len(active) == 0 {
		return false
	}

	taskMutates := isMutatingRole(task.AssignedAgent)

	for _, a := range active {
		if a.ID == task.ID {
			continue
		}
		activeMutates := isMutatingRole(a.AssignedAgent)
		// If neither mutates, concurrent read-only access is safe.
		if !taskMutates && !activeMutates {
			continue
		}

		for _, p1 := range task.AffectedPaths {
			clean1 := filepath.Clean(p1)
			for _, p2 := range a.AffectedPaths {
				clean2 := filepath.Clean(p2)
				if pathsOverlap(clean1, clean2) {
					return true
				}
			}
		}
	}
	return false
}

func isMutatingRole(role string) bool {
	r := strings.ToLower(role)
	return r == "implementer" || r == "orchestrator" || r == ""
}

func pathsOverlap(p1, p2 string) bool {
	if p1 == p2 {
		return true
	}
	// Check if one is subpath of another
	rel1, err1 := filepath.Rel(p1, p2)
	if err1 == nil && !strings.HasPrefix(rel1, "..") {
		return true
	}
	rel2, err2 := filepath.Rel(p2, p1)
	if err2 == nil && !strings.HasPrefix(rel2, "..") {
		return true
	}
	return false
}

// UpdateStatus updates a task's status and optional result or error.
func (g *TaskGraph) UpdateStatus(id string, status TaskStatus, result, errMsg string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if t, ok := g.Tasks[id]; ok {
		t.Status = status
		if result != "" {
			t.Result = result
		}
		if errMsg != "" {
			t.Error = errMsg
		}
	}
}

// IsComplete reports whether every task in the graph has reached a terminal status.
func (g *TaskGraph) IsComplete() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.Tasks) == 0 {
		return true
	}
	for _, t := range g.Tasks {
		switch t.Status {
		case TaskCompleted, TaskFailed, TaskCancelled:
			// Terminal
		default:
			return false
		}
	}
	return true
}

// HasFailures reports whether any task failed.
func (g *TaskGraph) HasFailures() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	for _, t := range g.Tasks {
		if t.Status == TaskFailed {
			return true
		}
	}
	return false
}

// ToPlanSteps converts the graph into PlanSteps for session synchronization.
func (g *TaskGraph) ToPlanSteps() []core.PlanStep {
	g.mu.RLock()
	defer g.mu.RUnlock()

	steps := make([]core.PlanStep, 0, len(g.Order))
	for _, id := range g.Order {
		t := g.Tasks[id]
		detail := ""
		if len(t.Dependencies) > 0 {
			detail = "deps: " + strings.Join(t.Dependencies, ", ")
		}
		if t.AssignedAgent != "" {
			if detail != "" {
				detail += " · "
			}
			detail += "agent: " + t.AssignedAgent
		}
		steps = append(steps, core.PlanStep{
			Title:  fmt.Sprintf("[%s] %s", t.ID, t.Title),
			Detail: detail,
			Done:   t.Status == TaskCompleted,
		})
	}
	return steps
}

// Format returns a formatted summary of the task graph for CLI/TUI display.
func (g *TaskGraph) Format() string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var b strings.Builder
	if g.Goal != "" {
		b.WriteString("Goal: " + g.Goal + "\n")
	}
	for _, id := range g.Order {
		t := g.Tasks[id]
		mark := "○"
		switch t.Status {
		case TaskCompleted:
			mark = "✓"
		case TaskRunning:
			mark = "●"
		case TaskFailed:
			mark = "✗"
		case TaskBlocked:
			mark = "⊘"
		}
		deps := ""
		if len(t.Dependencies) > 0 {
			deps = " (deps: " + strings.Join(t.Dependencies, ", ") + ")"
		}
		agentInfo := ""
		if t.AssignedAgent != "" {
			agentInfo = " [" + t.AssignedAgent + "]"
		}
		fmt.Fprintf(&b, "%s %s: %s%s%s\n", mark, t.ID, t.Title, agentInfo, deps)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ToJSON serializes the task graph.
func (g *TaskGraph) ToJSON() ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return json.Marshal(g)
}

// FromJSON deserializes a task graph from JSON.
func FromJSON(data []byte) (*TaskGraph, error) {
	var g TaskGraph
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, err
	}
	if g.Tasks == nil {
		g.Tasks = make(map[string]*Task)
	}
	return &g, nil
}
