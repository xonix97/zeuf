package providers

import (
	"strings"
	"testing"
)

func TestActionDirective(t *testing.T) {
	d := ActionDirective("/repo")
	for _, want := range []string{
		"working directory: /repo",
		"ACT on the workspace",
		"do not merely print code",
		"summarize briefly",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("directive missing %q", want)
		}
	}
	if strings.Contains(d, "no live tools") {
		t.Error("directive must not disclaim tools")
	}
	// No workdir, no dangling parenthesis.
	d = ActionDirective("")
	if strings.Contains(d, "()") || strings.Contains(d, "working directory:") {
		t.Errorf("empty workdir badly rendered: %q", d)
	}
}
