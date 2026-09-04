package agent

import (
	"fmt"
	"strings"

	"zeuf/internal/core"
)

// ErrMaxAttemptsExceeded indicates that the repair budget for a task has run out.
var ErrMaxAttemptsExceeded = fmt.Errorf("maximum repair attempts reached")

// CreateRepairTask creates a targeted, bounded repair task when a task fails verification.
func CreateRepairTask(failedTask *Task, vr *core.VerificationResult, filesTouched []string) (*Task, error) {
	if failedTask == nil {
		return nil, fmt.Errorf("failed task cannot be nil")
	}

	maxAttempts := failedTask.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	if failedTask.AttemptCount >= maxAttempts {
		return nil, fmt.Errorf("%w for task %s (%d/%d attempts)", ErrMaxAttemptsExceeded, failedTask.ID, failedTask.AttemptCount, maxAttempts)
	}

	repairNumber := failedTask.AttemptCount + 1
	repairID := fmt.Sprintf("%s-R%d", failedTask.ID, repairNumber)

	var desc strings.Builder
	fmt.Fprintf(&desc, "Repair task for %s (attempt %d/%d).\n", failedTask.ID, repairNumber, maxAttempts)
	fmt.Fprintf(&desc, "Original objective: %s\n", failedTask.Description)
	if vr != nil {
		fmt.Fprintf(&desc, "Failing verification command: %s (exit code %d)\n", vr.Command, vr.ExitCode)
		if vr.FailureDiagnosis != "" {
			fmt.Fprintf(&desc, "Failure diagnosis:\n%s\n", vr.FailureDiagnosis)
		} else if vr.Stderr != "" {
			fmt.Fprintf(&desc, "Error output:\n%s\n", vr.Stderr)
		}
	}
	if len(filesTouched) > 0 {
		fmt.Fprintf(&desc, "Files touched in previous attempt: %s\n", strings.Join(filesTouched, ", "))
	}
	desc.WriteString("Instruction: Fix the specific test/build failure without breaking other functionality. Apply minimal surgical edits.")

	allPaths := append([]string(nil), failedTask.AffectedPaths...)
	for _, f := range filesTouched {
		found := false
		for _, p := range allPaths {
			if p == f {
				found = true
				break
			}
		}
		if !found {
			allPaths = append(allPaths, f)
		}
	}

	verificationCmd := failedTask.Verification
	if vr != nil && vr.Command != "" {
		verificationCmd = vr.Command
	}

	repairTask := &Task{
		ID:            repairID,
		Title:         fmt.Sprintf("Fix %s: %s", failedTask.ID, failedTask.Title),
		Description:   desc.String(),
		Status:        TaskReady,
		Dependencies:  nil, // Can run immediately as a repair
		AssignedAgent: "implementer",
		RequiredTools: []string{"read", "write", "edit", "bash"},
		AffectedPaths: allPaths,
		Verification:  verificationCmd,
		AttemptCount:  0,
		MaxAttempts:   maxAttempts - failedTask.AttemptCount,
	}

	return repairTask, nil
}

// IngestRepairTask inserts a repair task into the graph and updates downstream dependencies.
func IngestRepairTask(graph *TaskGraph, failedTask *Task, repairTask *Task) error {
	if graph == nil || failedTask == nil || repairTask == nil {
		return fmt.Errorf("arguments cannot be nil")
	}

	if err := graph.AddTask(repairTask); err != nil {
		return err
	}

	// Update tasks that depended on the failed task to now depend on the repair task
	graph.mu.Lock()
	defer graph.mu.Unlock()

	for _, t := range graph.Tasks {
		if t.ID == repairTask.ID || t.ID == failedTask.ID {
			continue
		}
		for i, dep := range t.Dependencies {
			if dep == failedTask.ID {
				t.Dependencies[i] = repairTask.ID
			}
		}
	}

	return nil
}
