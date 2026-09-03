package agent

import (
	"fmt"
	"strings"
	"testing"
)

// The action-first doctrine is the behavioral core: the agent must reason
// its way to acting (never keyword-branching in code), so pin the doctrine
// text itself.
func TestActionFirstDoctrine(t *testing.T) {
	for _, want := range []string{
		"Action-first doctrine",
		"Can I accomplish this request by acting on the workspace",
		"Implement, don't recite",
		"here is the code you can add",
		"never needs magic words",
		"Judge from intent, never from trigger words",
		"proceed normally",
		"Confirm only clearly destructive",
		"Do not call tools to look busy",
		"Report work, don't dump it",
	} {
		if !strings.Contains(SystemPrompt, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}

func TestPromptSectionsOrdered(t *testing.T) {
	var nums []string
	for _, ln := range strings.Split(SystemPrompt, "\n") {
		if strings.HasPrefix(ln, "# ") {
			nums = append(nums, ln)
		}
	}
	if len(nums) < 12 {
		t.Fatalf("expected >=12 sections, got %d", len(nums))
	}
	prev := 0
	for _, h := range nums {
		var n int
		if _, err := fmt.Sscanf(h, "# %d.", &n); err != nil || n != prev+1 {
			t.Fatalf("sections out of order at %q", h)
		}
		prev = n
	}
}
