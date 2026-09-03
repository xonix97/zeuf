package tui

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]|\x1b\\][^\x07]*\x07")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func testModel() Model {
	m := NewFull(make(chan Event, 64), nil, nil)
	m.width, m.height = 100, 40
	m.vp = viewport.New(98, 30)
	return m
}

func TestStreamEchoDedupe(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "token", Text: "hello "})
	m.handleEvent(Event{Kind: "token", Text: "world"})
	m.handleEvent(Event{Kind: "text", Text: "hello world"})
	m.handleEvent(Event{Kind: "done"})
	n := 0
	for _, b := range m.blocks {
		if b.kind == "assistant" {
			n++
			if !strings.Contains(b.text, "hello world") {
				t.Errorf("assistant block = %q", b.text)
			}
			if b.cooked == "" {
				t.Error("finalized block should be markdown-cooked")
			}
		}
	}
	if n != 1 {
		t.Errorf("got %d assistant blocks, want 1 (stream + echo must fold)", n)
	}
}

func TestStreamPrefixExtend(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "token", Text: "hello"})
	m.handleEvent(Event{Kind: "text", Text: "hello world!"})
	if len(m.blocks) != 1 || m.blocks[0].text != "hello world!" {
		t.Errorf("blocks = %+v", m.blocks)
	}
}

func TestStreamSuffixEchoDropped(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "token", Text: "prefix hello"})
	m.handleEvent(Event{Kind: "text", Text: "hello"})
	if len(m.blocks) != 1 || m.blocks[0].text != "prefix hello" {
		t.Errorf("blocks = %+v", m.blocks)
	}
}

func TestUnrelatedTextAppends(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "token", Text: "abc"})
	m.handleEvent(Event{Kind: "text", Text: "something else entirely"})
	n := 0
	for _, b := range m.blocks {
		if b.kind == "assistant" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("got %d assistant blocks, want 2", n)
	}
}

func TestNonStreamedTextSingle(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "text", Text: "direct answer"})
	if len(m.blocks) != 1 || m.blocks[0].kind != "assistant" {
		t.Errorf("blocks = %+v", m.blocks)
	}
}

func TestToolStepLifecycle(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "tool-start", Tool: "read", Args: `{"path":"src/a.go"}`})
	if len(m.blocks) != 1 || m.blocks[0].toolDone {
		t.Fatalf("pending step missing: %+v", m.blocks)
	}
	if !strings.Contains(m.blocks[0].toolSummary, "src/a.go") {
		t.Errorf("summary = %q", m.blocks[0].toolSummary)
	}
	m.handleEvent(Event{Kind: "tool-end", Tool: "read", Text: "1: package main", Ok: true})
	b := m.blocks[0]
	if !b.toolDone || !b.toolOk || b.toolOut != "1: package main" {
		t.Errorf("completed step wrong: %+v", b)
	}
	view := m.renderBlocks(100)
	if !strings.Contains(view, "✓") || !strings.Contains(view, "Read src/a.go") {
		t.Errorf("rendered step missing check/summary:\n%s", view)
	}
}

func TestToolFailRendersCross(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "tool-start", Tool: "bash", Args: `{"command":"exit 3"}`})
	m.handleEvent(Event{Kind: "tool-end", Tool: "bash", Text: "exit error", Ok: false})
	if m.blocks[0].toolOk {
		t.Error("failed step must not be ok")
	}
	if view := m.renderBlocks(100); !strings.Contains(view, "✗") {
		t.Errorf("missing cross:\n%s", view)
	}
}

func TestPlanUpsert(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "plan", Text: "1/3", Detail: "1:repro\n0:fix\n0:verify"})
	m.handleEvent(Event{Kind: "plan", Text: "2/3", Detail: "1:repro\n1:fix\n0:verify"})
	n := 0
	for _, b := range m.blocks {
		if b.kind == "plan" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one live plan block, got %d", n)
	}
	view := m.renderBlocks(100)
	for _, want := range []string{"✓ repro", "✓ fix", "● verify"} {
		if !strings.Contains(view, want) {
			t.Errorf("plan missing %q:\n%s", want, view)
		}
	}
	m.handleEvent(Event{Kind: "plan", Text: "", Detail: ""})
	for _, b := range m.blocks {
		if b.kind == "plan" {
			t.Error("empty plan should remove the block")
		}
	}
}

func TestToolSummary(t *testing.T) {
	cases := map[string][2]string{
		"read":     {`{"path":"x.go"}`, "Read x.go"},
		"edit":     {`{"path":"x.go","old_string":"a","new_string":"b"}`, "Edit x.go"},
		"bash":     {`{"command":"go test ./..."}`, "Bash go test ./..."},
		"grep":     {`{"pattern":"TODO","path":"src"}`, "Search TODO in src"},
		"glob":     {`{"pattern":"**/*.go"}`, "Glob **/*.go"},
		"git":      {`{"args":["status","--short"]}`, "Git status --short"},
		"delegate": {`{"task":"explore auth"}`, "Delegate explore auth"},
		"plan":     {`{"op":"add","title":"repro"}`, "Plan add repro"},
	}
	for tool, tc := range cases {
		if got := toolSummary(tool, tc[0]); got != tc[1] {
			t.Errorf("%s: got %q want %q", tool, got, tc[1])
		}
	}
}

func TestSessionHeader(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "session", Detail: "/home/u/proj|main|*"})
	if m.status.Branch != "main" || m.status.Dirty != "*" {
		t.Errorf("status = %+v", m.status)
	}
	if h := m.headerView(); !strings.Contains(h, "main") || !strings.Contains(h, "/home/u/proj") {
		t.Errorf("header = %q", h)
	}
}

func TestWelcomeShownBeforeActivity(t *testing.T) {
	m := testModel()
	if !m.showWelcome() {
		t.Error("fresh model should show welcome")
	}
	m.handleEvent(Event{Kind: "text", Text: "hi"})
	if m.showWelcome() {
		t.Error("welcome must hide once conversation starts")
	}
}

func TestSelectorAndStatusChrome(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "session", Detail: "/home/u/proj|main|"})
	m.handleEvent(Event{Kind: "status", Text: "mimo|opencode|Connected|MiMo V2"})
	sel := stripANSI(m.selectorView())
	for _, want := range []string{"Agent", "Orchestrator", "Model", "MiMo V2", "opencode", "ctrl+p"} {
		if !strings.Contains(sel, want) {
			t.Errorf("selector missing %q: %q", want, sel)
		}
	}
	bar := stripANSI(m.statusView())
	for _, want := range []string{"ctrl+p models", "? help", "ctrl+c quit", "/home/u/proj", "v" + Version} {
		if !strings.Contains(bar, want) {
			t.Errorf("status bar missing %q: %q", want, bar)
		}
	}
	if m.status.Display != "MiMo V2" {
		t.Errorf("display = %q", m.status.Display)
	}
	// Legacy 3-field status still parses.
	m.handleEvent(Event{Kind: "status", Text: "x|y|Connected"})
	if m.status.State != "Connected" {
		t.Error("3-field status broke")
	}
}

func TestShortPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home")
	}
	if got := shortPath(home + "/zeuf"); got != "~/zeuf" {
		t.Errorf("shortPath = %q", got)
	}
	if got := shortPath("/tmp/x"); got != "/tmp/x" {
		t.Errorf("shortPath = %q", got)
	}
}

func TestExampleTipRotation(t *testing.T) {
	m := testModel()
	m.width, m.height = 100, 40
	first := m.input.Placeholder
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.input.Placeholder == first {
		t.Error("placeholder example should rotate after submit")
	}
}

func TestThinkingBlock(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "reasoning", Text: "first "})
	m.handleEvent(Event{Kind: "reasoning", Text: "second"})
	n := 0
	for _, b := range m.blocks {
		if b.kind == "thinking" {
			n++
			if b.text != "first second" {
				t.Errorf("thinking = %q", b.text)
			}
		}
	}
	if n != 1 {
		t.Fatalf("want one thinking block, got %d", n)
	}
	if view := stripANSI(m.renderBlocks(100)); !strings.Contains(view, "◌") || !strings.Contains(view, "first second") {
		t.Errorf("thinking not rendered:\n%s", view)
	}
}

func TestThinkingCap(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "reasoning", Text: strings.Repeat("x", 3000)})
	if got := len([]rune(m.blocks[0].text)); got > 2030 {
		t.Errorf("thinking uncapped: %d runes", got)
	}
	if !strings.Contains(m.blocks[0].text, "truncated") {
		t.Error("cap must say truncated")
	}
	// Further deltas stay dropped.
	m.handleEvent(Event{Kind: "reasoning", Text: "more"})
	if got := len([]rune(m.blocks[0].text)); got > 2030 {
		t.Errorf("cap bypassed: %d", got)
	}
}

func TestUsageBar(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "status", Text: "m|opencode|Connected"})
	m.handleEvent(Event{Kind: "usage", Text: "12.4k/200k", Detail: "6%"})
	if bar := stripANSI(m.statusView()); !strings.Contains(bar, "ctx 12.4k/200k (6%)") {
		t.Errorf("bar = %q", bar)
	}
	m.handleEvent(Event{Kind: "usage", Text: "", Detail: ""})
	if bar := stripANSI(m.statusView()); strings.Contains(bar, "ctx") {
		t.Errorf("unknown usage must hide the meter: %q", bar)
	}
}

func TestWritePreviewLines(t *testing.T) {
	got := writePreviewLines("write", `{"path":"a.txt","content":"l1\nl2\nl3\nl4\nl5\nl6\nl7"}`)
	if len(got) != 7 || !strings.Contains(got[0], "l1") || !strings.Contains(got[6], "…") {
		t.Errorf("write preview = %q", got)
	}
	got = writePreviewLines("edit", `{"path":"a","old_string":"o1\no2","new_string":"n1"}`)
	if len(got) != 3 || got[0] != "- o1" || got[2] != "+ n1" {
		t.Errorf("edit preview = %q", got)
	}
	if writePreviewLines("bash", `{"command":"x"}`) != nil {
		t.Error("other tools need no content preview")
	}
	if writePreviewLines("write", `nope`) != nil {
		t.Error("bad args must yield nil")
	}
}

func TestTaskHeader(t *testing.T) {
	m := testModel()
	m.width = 100
	m.handleEvent(Event{Kind: "task", Text: "fix the thing"})
	m.busy = true
	if h := stripANSI(m.headerView()); !strings.Contains(h, "› fix the thing") {
		t.Errorf("header = %q", h)
	}
	m.busy = false
	if h := stripANSI(m.headerView()); strings.Contains(h, "›") {
		t.Errorf("idle header must not show task: %q", h)
	}
}

func TestNestedIndent(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "tool-start", Tool: "read", Args: `{"path":"a"}`, Depth: 1})
	m.handleEvent(Event{Kind: "tool-end", Tool: "read", Text: "ok", Ok: true, Depth: 1})
	if view := m.renderBlocks(100); !strings.Contains(view, "│") {
		t.Errorf("nested row missing guide:\n%s", view)
	}
}

func TestAgentViewSmoke(t *testing.T) {
	m := testModel()
	m.handleEvent(Event{Kind: "session", Detail: "/home/u/proj|main|*"})
	m.handleEvent(Event{Kind: "status", Text: "spark|opencode|Connected"})
	m.handleEvent(Event{Kind: "text", Text: "Fixing now."})
	m.handleEvent(Event{Kind: "tool-start", Tool: "read", Args: `{"path":"a.go"}`})
	m.handleEvent(Event{Kind: "tool-end", Tool: "read", Text: "1: package main", Ok: true})
	m.handleEvent(Event{Kind: "plan", Text: "1/2", Detail: "1:find bug\n0:fix it"})
	m.handleEvent(Event{Kind: "done"})
	m.fallbacks = 1
	view := m.View()
	for _, want := range []string{"main*", "/home/u/proj", "✓ Read a.go", "✓ find bug", "● fix it", "fix it", "spark", "plan 1/2", "fallbacks 1"} {
		if !strings.Contains(stripANSI(view), want) {
			t.Errorf("view missing %q", want)
		}
	}
}
