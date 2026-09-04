package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSchedulerParallelExecution(t *testing.T) {
	g := NewTaskGraph("parallel test")
	g.AddTask(&Task{ID: "A", Title: "Task A", AssignedAgent: "explorer"})
	g.AddTask(&Task{ID: "B", Title: "Task B", AssignedAgent: "explorer"})
	g.AddTask(&Task{ID: "C", Title: "Task C", AssignedAgent: "explorer"})

	var peakConcurrent atomic.Int32
	var currentConcurrent atomic.Int32

	exec := func(ctx context.Context, task *Task) (string, error) {
		cur := currentConcurrent.Add(1)
		for {
			old := peakConcurrent.Load()
			if cur <= old || peakConcurrent.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		currentConcurrent.Add(-1)
		return "done " + task.ID, nil
	}

	sched := NewScheduler(SchedulerConfig{MaxConcurrency: 3, DefaultTimeout: 2 * time.Second}, g, exec)
	if err := sched.Run(context.Background()); err != nil {
		t.Fatalf("scheduler run failed: %v", err)
	}

	if peak := peakConcurrent.Load(); peak < 2 {
		t.Errorf("expected parallel execution (peak >= 2), got peak %d", peak)
	}
	if !g.IsComplete() {
		t.Fatal("graph should be complete")
	}
}

func TestSchedulerDependencyEnforcement(t *testing.T) {
	g := NewTaskGraph("dep test")
	g.AddTask(&Task{ID: "A", Title: "Prereq", AssignedAgent: "explorer"})
	g.AddTask(&Task{ID: "B", Title: "Dependent", AssignedAgent: "implementer", Dependencies: []string{"A"}})

	var aFinished atomic.Bool
	var bStartedAfterA atomic.Bool

	exec := func(ctx context.Context, task *Task) (string, error) {
		if task.ID == "A" {
			time.Sleep(30 * time.Millisecond)
			aFinished.Store(true)
			return "ok A", nil
		}
		if task.ID == "B" {
			if aFinished.Load() {
				bStartedAfterA.Store(true)
			}
			return "ok B", nil
		}
		return "", nil
	}

	sched := NewScheduler(DefaultSchedulerConfig(), g, exec)
	if err := sched.Run(context.Background()); err != nil {
		t.Fatalf("scheduler run failed: %v", err)
	}

	if !bStartedAfterA.Load() {
		t.Fatal("B must strictly execute after A has completed")
	}
}

func TestSchedulerConflictSerialization(t *testing.T) {
	g := NewTaskGraph("conflict test")
	// Both tasks are independent (no dependencies), but mutate the SAME file
	g.AddTask(&Task{
		ID:            "T1",
		Title:         "Edit auth",
		AssignedAgent: "implementer",
		AffectedPaths: []string{"internal/auth/client.go"},
	})
	g.AddTask(&Task{
		ID:            "T2",
		Title:         "Edit auth 2",
		AssignedAgent: "implementer",
		AffectedPaths: []string{"internal/auth/client.go"},
	})

	var mu sync.Mutex
	activeRunning := 0
	maxRunningSimultaneously := 0

	exec := func(ctx context.Context, task *Task) (string, error) {
		mu.Lock()
		activeRunning++
		if activeRunning > maxRunningSimultaneously {
			maxRunningSimultaneously = activeRunning
		}
		mu.Unlock()

		time.Sleep(40 * time.Millisecond)

		mu.Lock()
		activeRunning--
		mu.Unlock()
		return "ok", nil
	}

	sched := NewScheduler(SchedulerConfig{MaxConcurrency: 5, DefaultTimeout: 2 * time.Second}, g, exec)
	if err := sched.Run(context.Background()); err != nil {
		t.Fatalf("scheduler failed: %v", err)
	}

	if maxRunningSimultaneously > 1 {
		t.Fatalf("conflicting tasks touching same file ran concurrently! (max = %d)", maxRunningSimultaneously)
	}
}

func TestSchedulerCancellation(t *testing.T) {
	g := NewTaskGraph("cancel test")
	g.AddTask(&Task{ID: "T1", Title: "Long task", AssignedAgent: "implementer"})

	ctx, cancel := context.WithCancel(context.Background())

	exec := func(ctx context.Context, task *Task) (string, error) {
		cancel() // trigger cancellation while running
		time.Sleep(100 * time.Millisecond)
		return "ok", nil
	}

	sched := NewScheduler(DefaultSchedulerConfig(), g, exec)
	err := sched.Run(ctx)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}
