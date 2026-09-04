package agent

import (
	"context"
	"errors"
	"testing"

	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
)

func TestRunVerificationSuccess(t *testing.T) {
	dir := t.TempDir()
	tools := ct.NewRegistry(dir, ct.Policy{AutoApprove: true})

	vr, err := RunVerification(context.Background(), tools, "T1", "echo all-passed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !vr.Passed {
		t.Errorf("expected verification to pass, got failed (exit %d)", vr.ExitCode)
	}
	if vr.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", vr.ExitCode)
	}
}

func TestRunVerificationFailure(t *testing.T) {
	dir := t.TempDir()
	tools := ct.NewRegistry(dir, ct.Policy{AutoApprove: true})

	vr, err := RunVerification(context.Background(), tools, "T1", "bash -c 'echo \"--- FAIL: TestAuth\" >&2; exit 1'")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vr.Passed {
		t.Error("expected verification to fail")
	}
	if vr.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", vr.ExitCode)
	}
	if vr.FailureDiagnosis == "" {
		t.Error("expected failure diagnosis to capture failure line")
	}
}

func TestCreateRepairTask(t *testing.T) {
	failed := &Task{
		ID:            "T2",
		Title:         "Implement auth client",
		Description:   "Add timeout handling",
		AttemptCount:  1,
		MaxAttempts:   3,
		AffectedPaths: []string{"internal/auth/client.go"},
		Verification:  "go test ./internal/auth",
	}

	vr := &core.VerificationResult{
		TaskID:           "T2",
		Command:          "go test ./internal/auth",
		ExitCode:         1,
		Stderr:           "--- FAIL: TestClientTimeout (5.01s)",
		FailureDiagnosis: "--- FAIL: TestClientTimeout (5.01s)",
		Passed:           false,
	}

	repair, err := CreateRepairTask(failed, vr, []string{"internal/auth/client.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repair.ID != "T2-R2" {
		t.Errorf("repair ID = %q, want T2-R2", repair.ID)
	}
	if repair.AssignedAgent != "implementer" {
		t.Errorf("assigned agent = %q, want implementer", repair.AssignedAgent)
	}
	if repair.Verification != "go test ./internal/auth" {
		t.Errorf("verification = %q", repair.Verification)
	}
}

func TestRepairAttemptExhaustion(t *testing.T) {
	failed := &Task{
		ID:           "T3",
		Title:        "Hard problem",
		AttemptCount: 3,
		MaxAttempts:  3,
	}

	_, err := CreateRepairTask(failed, nil, nil)
	if err == nil {
		t.Fatal("expected error on attempt limit exhaustion")
	}
	if !errors.Is(err, ErrMaxAttemptsExceeded) {
		t.Errorf("expected ErrMaxAttemptsExceeded, got %v", err)
	}
}

func TestIngestRepairTaskRewiresDependencies(t *testing.T) {
	g := NewTaskGraph("repair test")
	g.AddTask(&Task{ID: "T1", Title: "Prereq", Status: TaskCompleted})
	t2 := &Task{ID: "T2", Title: "Failed task", Dependencies: []string{"T1"}, Status: TaskFailed, AttemptCount: 1, MaxAttempts: 3}
	g.AddTask(t2)
	g.AddTask(&Task{ID: "T3", Title: "Downstream task", Dependencies: []string{"T2"}, Status: TaskPending})

	repair, err := CreateRepairTask(t2, nil, nil)
	if err != nil {
		t.Fatalf("failed to create repair task: %v", err)
	}

	if err := IngestRepairTask(g, t2, repair); err != nil {
		t.Fatalf("failed to ingest repair task: %v", err)
	}

	// Downstream T3 should now depend on repair task (T2-R2)
	t3, _ := g.GetTask("T3")
	if len(t3.Dependencies) != 1 || t3.Dependencies[0] != repair.ID {
		t.Fatalf("T3 dependencies not rewired to %s, got %v", repair.ID, t3.Dependencies)
	}

	// Graph must remain valid DAG
	if err := g.Validate(); err != nil {
		t.Fatalf("graph invalid after repair ingestion: %v", err)
	}
}
