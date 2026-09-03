package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	ct "zeuf/internal/core/tools"
)

const fixtureScript = `#!/bin/sh
echo "starting up (log chatter, not protocol)"
while IFS= read -r line; do
  id=$(printf '%s' "$line" | grep -o '"id":[0-9]*' | head -n1 | cut -d: -f2)
  [ -z "$id" ] && id=0
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"fix","version":"1"}}}\n' "$id";;
    *'"method":"tools/list"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"echo","description":"Echo back","inputSchema":{"type":"object","properties":{"text":{"type":"string"}}}}]}}\n' "$id";;
    *'"method":"tools/call"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"ECHO:hi"}],"isError":false}}\n' "$id";;
  esac
done
`

func fixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "mcpsrv.sh")
	if err := os.WriteFile(p, []byte(fixtureScript), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStartAndTools(t *testing.T) {
	s, err := Start(context.Background(), "fix", ServerConfig{Command: fixture(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.tools) != 1 || s.tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", s.tools)
	}
	out, isErr, err := s.CallTool(context.Background(), "echo", `{"text":"hi"}`)
	if err != nil || isErr || out != "ECHO:hi" {
		t.Errorf("call = %q %v %v", out, isErr, err)
	}
}

func TestBadCommand(t *testing.T) {
	if _, err := Start(context.Background(), "nope", ServerConfig{Command: "/nonexistent/mcp-xyz"}); err == nil {
		t.Error("expected start failure")
	}
	if _, err := Start(context.Background(), "empty", ServerConfig{}); err == nil {
		t.Error("expected config failure")
	}
}

func TestToolName(t *testing.T) {
	if got := ToolName("My Server", "Read File!"); got != "mcp_my_server_read_file" {
		t.Errorf("name = %q", got)
	}
}

func TestManagerMixedAndAsTools(t *testing.T) {
	m := NewManager()
	m.Start(context.Background(), map[string]ServerConfig{
		"good": {Command: fixture(t)},
		"bad":  {Command: "/nonexistent/mcp-xyz"},
	})
	defer m.Close()
	st := m.Status()
	if len(st) != 2 {
		t.Fatalf("status = %+v", st)
	}
	reg := ct.NewRegistry(t.TempDir(), ct.Policy{AutoApprove: true})
	if n := m.AsTools(reg); n != 1 {
		t.Fatalf("registered %d tools", n)
	}
	tool, ok := reg.Get("mcp_good_echo")
	if !ok {
		t.Fatal("namespaced tool missing")
	}
	res, err := tool.Run(context.Background(), `{"text":"hi"}`)
	if err != nil || res.IsError || res.Content != "ECHO:hi" {
		t.Errorf("run = %+v %v", res, err)
	}
}
