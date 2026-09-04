package agent

import (
	"testing"
)

func TestTaskGraphCycleDetection(t *testing.T) {
	g := NewTaskGraph("test cycle")
	g.AddTask(&Task{ID: "A", Title: "Task A", Dependencies: []string{"B"}})
	g.AddTask(&Task{ID: "B", Title: "Task B", Dependencies: []string{"A"}})

	if err := g.Validate(); err == nil {
		t.Fatal("expected cycle detection error between A and B, got nil")
	}

	// Self-cycle
	g2 := NewTaskGraph("test self cycle")
	g2.AddTask(&Task{ID: "A", Title: "Task A", Dependencies: []string{"A"}})
	if err := g2.Validate(); err == nil {
		t.Fatal("expected self-cycle detection error for A, got nil")
	}

	// Missing dependency
	g3 := NewTaskGraph("test missing dep")
	g3.AddTask(&Task{ID: "A", Title: "Task A", Dependencies: []string{"NONEXISTENT"}})
	if err := g3.Validate(); err == nil {
		t.Fatal("expected missing dependency error, got nil")
	}
}

func TestTaskGraphDependencyFlow(t *testing.T) {
	g := NewTaskGraph("valid DAG")
	g.AddTask(&Task{ID: "A", Title: "Inspect auth", AssignedAgent: "explorer"})
	g.AddTask(&Task{ID: "B", Title: "Inspect router", AssignedAgent: "explorer"})
	g.AddTask(&Task{ID: "C", Title: "Inspect UI", AssignedAgent: "explorer"})
	g.AddTask(&Task{ID: "D", Title: "Implement auth", AssignedAgent: "implementer", Dependencies: []string{"A"}})
	g.AddTask(&Task{ID: "E", Title: "Implement router", AssignedAgent: "implementer", Dependencies: []string{"B"}})
	g.AddTask(&Task{ID: "F", Title: "Implement UI", AssignedAgent: "implementer", Dependencies: []string{"C"}})
	g.AddTask(&Task{ID: "G", Title: "Integration test", AssignedAgent: "tester", Dependencies: []string{"D", "E", "F"}})

	if err := g.Validate(); err != nil {
		t.Fatalf("valid DAG failed validation: %v", err)
	}

	// Initial ready tasks: A, B, C can execute concurrently
	ready := g.ReadyTasks()
	if len(ready) != 3 {
		t.Fatalf("expected 3 ready tasks (A, B, C), got %d", len(ready))
	}
	readyMap := map[string]bool{}
	for _, task := range ready {
		readyMap[task.ID] = true
	}
	if !readyMap["A"] || !readyMap["B"] || !readyMap["C"] {
		t.Fatalf("expected A, B, C to be ready, got %v", readyMap)
	}

	// Mark A completed -> D should become ready
	g.UpdateStatus("A", TaskCompleted, "auth inspected", "")
	ready = g.ReadyTasks()
	readyMap = map[string]bool{}
	for _, task := range ready {
		readyMap[task.ID] = true
	}
	if !readyMap["D"] {
		t.Fatalf("expected D to become ready after A completed, got %v", readyMap)
	}
	if readyMap["G"] {
		t.Fatal("G must not be ready yet")
	}

	// Mark B, C, D, E, F completed -> G should become ready
	g.UpdateStatus("B", TaskCompleted, "router inspected", "")
	g.UpdateStatus("C", TaskCompleted, "UI inspected", "")
	g.UpdateStatus("D", TaskCompleted, "auth implemented", "")
	g.UpdateStatus("E", TaskCompleted, "router implemented", "")
	g.UpdateStatus("F", TaskCompleted, "UI implemented", "")

	ready = g.ReadyTasks()
	if len(ready) != 1 || ready[0].ID != "G" {
		t.Fatalf("expected only G to be ready, got %v", ready)
	}

	g.UpdateStatus("G", TaskCompleted, "integration test passed", "")
	if !g.IsComplete() {
		t.Fatal("graph should be complete after all tasks succeed")
	}
	if g.HasFailures() {
		t.Fatal("graph should have no failures")
	}
}

func TestTaskGraphConflictDetection(t *testing.T) {
	g := NewTaskGraph("conflict test")
	t1 := &Task{ID: "T1", Title: "Edit auth", AssignedAgent: "implementer", AffectedPaths: []string{"internal/auth/client.go"}}
	t2 := &Task{ID: "T2", Title: "Edit same auth", AssignedAgent: "implementer", AffectedPaths: []string{"internal/auth/client.go"}}
	t3 := &Task{ID: "T3", Title: "Edit router", AssignedAgent: "implementer", AffectedPaths: []string{"internal/router/router.go"}}
	t4 := &Task{ID: "T4", Title: "Read auth", AssignedAgent: "explorer", AffectedPaths: []string{"internal/auth/client.go"}}
	t5 := &Task{ID: "T5", Title: "Read auth 2", AssignedAgent: "explorer", AffectedPaths: []string{"internal/auth/client.go"}}

	// T1 conflicts with T2 (both mutate internal/auth/client.go)
	if !g.DetectConflict(t2, []*Task{t1}) {
		t.Fatal("expected conflict between T1 and T2 touching same file")
	}

	// T1 does not conflict with T3 (different paths)
	if g.DetectConflict(t3, []*Task{t1}) {
		t.Fatal("expected no conflict between T1 and T3")
	}

	// T4 and T5 are both read-only explorers -> no conflict
	if g.DetectConflict(t5, []*Task{t4}) {
		t.Fatal("two explorers on same path should not conflict")
	}

	// T1 mutates while T4 reads same path -> conflict!
	if !g.DetectConflict(t4, []*Task{t1}) {
		t.Fatal("explorer reading while implementer mutates should conflict")
	}
}

func TestTaskGraphJSON(t *testing.T) {
	g := NewTaskGraph("json test")
	g.AddTask(&Task{ID: "T1", Title: "Task 1", Dependencies: nil})
	g.AddTask(&Task{ID: "T2", Title: "Task 2", Dependencies: []string{"T1"}})

	data, err := g.ToJSON()
	if err != nil {
		t.Fatalf("failed to serialize to json: %v", err)
	}

	g2, err := FromJSON(data)
	if err != nil {
		t.Fatalf("failed to deserialize: %v", err)
	}

	if len(g2.Tasks) != 2 || g2.Goal != "json test" {
		t.Fatalf("deserialized graph mismatch: %+v", g2)
	}
}
