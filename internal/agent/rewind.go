package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zeuf/internal/core"
	ct "zeuf/internal/core/tools"
)

// Rewind restores files touched by the last n checkpoints (default 1),
// newest first. It asks approval first: restoring destroys newer work.
// Returns human-readable restored/skipped lines.
func Rewind(sess *Session2, tools *ct.Registry, n int) ([]string, error) {
	cps := sess.Session.Checkpoints
	if len(cps) == 0 {
		return nil, fmt.Errorf("no checkpoints yet — nothing to rewind")
	}
	if n <= 0 {
		n = 1
	}
	if n > len(cps) {
		n = len(cps)
	}
	target := cps[len(cps)-n:]
	// Plan restores newest-first, each path once.
	type job struct {
		fv    core.FileVersion
		label string
	}
	var jobs []job
	seen := map[string]bool{}
	for i := len(target) - 1; i >= 0; i-- {
		for _, fv := range target[i].Files {
			if seen[fv.Path] {
				continue
			}
			seen[fv.Path] = true
			jobs = append(jobs, job{fv: fv, label: target[i].Label})
		}
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("checkpoints hold no files")
	}
	var desc strings.Builder
	fmt.Fprintf(&desc, "restore %d file(s) to before %q?", len(jobs), target[0].Label)
	for _, j := range jobs {
		fmt.Fprintf(&desc, "\n  %s", j.fv.Path)
	}
	if !tools.RequestApproval("rewind files", desc.String()) {
		return nil, fmt.Errorf("rewind denied by approval policy")
	}
	var out []string
	for _, j := range jobs {
		fv := j.fv
		if fv.TooLarge {
			out = append(out, fmt.Sprintf("skip %s (too large to snapshot)", fv.Path))
			continue
		}
		if !fv.Existed {
			if err := os.Remove(fv.Path); err != nil && !os.IsNotExist(err) {
				out = append(out, fmt.Sprintf("FAIL %s: %v", fv.Path, err))
				continue
			}
			out = append(out, fmt.Sprintf("removed %s (was created)", fv.Path))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fv.Path), 0o755); err != nil {
			out = append(out, fmt.Sprintf("FAIL %s: %v", fv.Path, err))
			continue
		}
		if err := os.WriteFile(fv.Path, []byte(fv.Before), 0o644); err != nil {
			out = append(out, fmt.Sprintf("FAIL %s: %v", fv.Path, err))
			continue
		}
		out = append(out, fmt.Sprintf("restored %s", fv.Path))
	}
	// Drop the consumed checkpoints so rewind doesn't re-apply.
	sess.Session.Checkpoints = cps[:len(cps)-n]
	return out, nil
}
