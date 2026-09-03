package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testReg(t *testing.T, auto bool) *Registry {
	t.Helper()
	return NewRegistry(t.TempDir(), Policy{AutoApprove: auto})
}

func TestReadWriteEditRoundtrip(t *testing.T) {
	ctx := context.Background()
	r := testReg(t, true)
	if _, err := r.Execute(ctx, "write", `{"path":"a.txt","content":"hello"}`); err != nil {
		t.Fatal(err)
	}
	res, _ := r.Execute(ctx, "read", `{"path":"a.txt"}`)
	if !strings.Contains(res.Content, "hello") {
		t.Errorf("read = %q", res.Content)
	}
	if _, err := r.Execute(ctx, "edit", `{"path":"a.txt","old_string":"hello","new_string":"world"}`); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(r.Workdir, "a.txt"))
	if string(data) != "world" {
		t.Errorf("edit result = %q", data)
	}
	res, _ = r.Execute(ctx, "edit", `{"path":"a.txt","old_string":"missing","new_string":"x"}`)
	if !res.IsError {
		t.Error("editing a missing old_string should be an error result")
	}
}

func TestWriteNeedsApprovalWhenNotAuto(t *testing.T) {
	ctx := context.Background()
	denied := NewRegistry(t.TempDir(), Policy{Approver: func(a, d string) bool { return false }})
	if res, _ := denied.Execute(ctx, "write", `{"path":"a.txt","content":"x"}`); !res.IsError {
		t.Error("write without approval should fail")
	}
	allowed := NewRegistry(t.TempDir(), Policy{Approver: func(a, d string) bool { return true }})
	if res, _ := allowed.Execute(ctx, "write", `{"path":"a.txt","content":"x"}`); res.IsError {
		t.Errorf("approved write failed: %s", res.Content)
	}
}

func TestDestructiveBashBlocked(t *testing.T) {
	ctx := context.Background()
	r := NewRegistry(t.TempDir(), Policy{AutoApprove: true, Approver: func(a, d string) bool { return false }})
	if res, _ := r.Execute(ctx, "bash", `{"command":"rm -rf / "}`); !res.IsError {
		t.Error("destructive command must be denied even in auto mode")
	}
	r2 := testReg(t, true)
	res, _ := r2.Execute(ctx, "bash", `{"command":"echo hi"}`)
	if res.IsError || !strings.Contains(res.Content, "hi") {
		t.Errorf("safe bash failed: %+v", res)
	}
}

func TestOutputTruncation(t *testing.T) {
	ctx := context.Background()
	r := testReg(t, true)
	big := strings.Repeat("x", 40*1024)
	if _, err := r.Execute(ctx, "write", `{"path":"big.txt","content":"`+big+`"}`); err != nil {
		t.Fatal(err)
	}
	res, _ := r.Execute(ctx, "read", `{"path":"big.txt"}`)
	if !res.Truncated {
		t.Error("large output should be truncated")
	}
}

func TestUnknownTool(t *testing.T) {
	res, _ := testReg(t, true).Execute(context.Background(), "nope", `{}`)
	if !res.IsError {
		t.Error("unknown tool should be an error result")
	}
}

func TestPlanTool(t *testing.T) {
	ctx := context.Background()
	r := testReg(t, true)
	if _, err := r.Execute(ctx, "plan", `{"op":"add","title":"repro"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(ctx, "plan", `{"op":"add","title":"fix"}`); err != nil {
		t.Fatal(err)
	}
	res, _ := r.Execute(ctx, "plan", `{"op":"done","index":0}`)
	if res.IsError {
		t.Fatal(res.Content)
	}
	steps := r.PlanSteps()
	if len(steps) != 2 || !steps[0].Done || steps[1].Done {
		t.Errorf("plan state wrong: %+v", steps)
	}
}

func TestGitStatus(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	r := NewRegistry(dir, Policy{AutoApprove: true})
	if res, _ := r.Execute(ctx, "git", `{"args":["init"]}`); res.IsError {
		t.Skipf("git not available: %s", res.Content)
	}
	res, _ := r.Execute(ctx, "git", `{"args":["status"]}`)
	if res.IsError {
		t.Errorf("git status failed: %s", res.Content)
	}
}
