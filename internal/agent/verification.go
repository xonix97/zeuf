package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
)

// RunVerification executes a verification command and produces a structured result.
func RunVerification(ctx context.Context, tools *ct.Registry, taskID, command string) (*core.VerificationResult, error) {
	cmdClean := strings.TrimSpace(command)
	if cmdClean == "" {
		// No command specified; check git status/diff as implicit verification
		res, err := tools.Execute(ctx, "git", `{"args":["diff","--stat"]}`)
		if err != nil {
			return &core.VerificationResult{
				TaskID:   taskID,
				Command:  "git diff --stat",
				Passed:   false,
				Stderr:   err.Error(),
				Duration: 0,
			}, nil
		}
		return &core.VerificationResult{
			TaskID:   taskID,
			Command:  "git diff --stat",
			Passed:   true,
			Stdout:   res.Content,
			Duration: 0,
		}, nil
	}

	start := time.Now()
	res, err := tools.Execute(ctx, "bash", fmt.Sprintf(`{"command":%q,"timeout_ms":120000}`, cmdClean))
	duration := time.Since(start)

	vr := &core.VerificationResult{
		TaskID:   taskID,
		Command:  cmdClean,
		Duration: duration,
	}

	if err != nil {
		vr.Passed = false
		vr.Stderr = err.Error()
		vr.ExitCode = 1
		vr.FailureDiagnosis = "verification execution error: " + err.Error()
		return vr, nil
	}

	if res.IsError {
		vr.Passed = false
		vr.ExitCode = 1
		vr.Stderr = res.Content
		vr.FailureDiagnosis = extractFailureDiagnosis(res.Content)
		return vr, nil
	}

	vr.Passed = true
	vr.ExitCode = 0
	vr.Stdout = res.Content
	return vr, nil
}

// extractFailureDiagnosis parses compiler/test output for the core error message.
func extractFailureDiagnosis(output string) string {
	lines := strings.Split(output, "\n")
	var failures []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		// Go test failures: "--- FAIL: TestXYZ", "FAIL", panic
		if strings.HasPrefix(trimmed, "--- FAIL:") || strings.Contains(lower, "panic:") ||
			strings.HasPrefix(lower, "error:") || strings.Contains(lower, "syntax error") ||
			strings.Contains(lower, "undefined:") || strings.Contains(lower, "failed") {
			failures = append(failures, trimmed)
			if len(failures) >= 5 {
				break
			}
		}
	}

	if len(failures) > 0 {
		return strings.Join(failures, "\n")
	}

	// Fallback to tail of output
	if len(lines) > 6 {
		return strings.Join(lines[len(lines)-6:], "\n")
	}
	return output
}
