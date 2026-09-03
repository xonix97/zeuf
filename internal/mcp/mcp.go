// Package mcp connects Zeuf to Model Context Protocol servers over stdio
// and exposes their tools to the model. The client speaks just enough
// JSON-RPC for the job — initialize handshake, tools/list, tools/call —
// with no third-party dependencies. Server-initiated requests (e.g.
// sampling) are not served; such servers are reported, not hung on.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	ct "zeuf/internal/core/tools"
)

// ServerConfig describes one MCP server (mirrors the config file).
type ServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"`
}

// request is a JSON-RPC envelope.
type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// response is a JSON-RPC reply.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Tool is one discovered server tool.
type Tool struct {
	Name        string
	Description string
	Schema      string
}

// Server is one running stdio backend.
type Server struct {
	name  string
	cmd   *exec.Cmd
	stdin *jsonEncoder
	sc    *bufio.Scanner
	mu    sync.Mutex
	seq   int64
	tools []Tool
	err   error
}

type jsonEncoder struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (e *jsonEncoder) send(v any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.enc.Encode(v)
}

var nonToolChars = regexp.MustCompile(`[^a-z0-9_]+`)

// ToolName namespaces a server tool for the model.
func ToolName(server, tool string) string {
	s := "mcp_" + nonToolChars.ReplaceAllString(strings.ToLower(server+"_"+tool), "_")
	s = strings.Trim(s, "_")
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// Start launches the server and runs the initialize handshake. The process
// lives as long as ctx (handshake calls use their own short timeouts).
func Start(ctx context.Context, name string, cfg ServerConfig) (*Server, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("mcp server %q has no command", name)
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = append(cmd.Environ(), cfg.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	// Bound stderr capture; failures report it.
	cmd.Stderr = &limitedWriter{sb: &stderr, max: 4096}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &Server{name: name, cmd: cmd, stdin: &jsonEncoder{enc: json.NewEncoder(stdin)}}
	s.sc = bufio.NewScanner(stdout)
	s.sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	hctx, hcancel := context.WithTimeout(ctx, 30*time.Second)
	defer hcancel()
	var initResp map[string]any
	if err := s.call(hctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "zeuf", "version": "0.4.0"},
	}, &initResp); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("mcp initialize %q: %w (stderr: %s)", name, err, stderr.String())
	}
	// Best-effort initialized notification (no reply expected).
	_ = s.stdin.send(request{JSONRPC: "2.0", Method: "notifications/initialized"})
	if err := s.refreshTools(hctx); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("mcp tools/list %q: %w", name, err)
	}
	return s, nil
}

// call performs one single-flight request, skipping interleaved
// notifications or server-initiated requests.
func (s *Server) call(ctx context.Context, method string, params any, out any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	id := s.seq
	if err := s.stdin.send(request{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !s.sc.Scan() {
			if err := s.sc.Err(); err != nil {
				return err
			}
			return fmt.Errorf("mcp server %q closed the stream", s.name)
		}
		line := strings.TrimSpace(s.sc.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue // log chatter, not protocol
		}
		var resp response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.ID == nil || *resp.ID != id {
			continue // notification or another turn; not ours
		}
		if resp.Error != nil {
			return fmt.Errorf("mcp error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		if out == nil || len(resp.Result) == 0 {
			return nil
		}
		return json.Unmarshal(resp.Result, out)
	}
}

// refreshTools populates the tool list.
func (s *Server) refreshTools(ctx context.Context) error {
	var v struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := s.call(ctx, "tools/list", map[string]any{}, &v); err != nil {
		return err
	}
	s.tools = nil
	for _, t := range v.Tools {
		if t.Name == "" {
			continue
		}
		schema := string(t.InputSchema)
		if schema == "" {
			schema = `{"type":"object"}`
		}
		s.tools = append(s.tools, Tool{Name: t.Name, Description: t.Description, Schema: schema})
	}
	return nil
}

// CallTool invokes a tool by its server-local name.
func (s *Server) CallTool(ctx context.Context, tool string, argsJSON string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	var args any = map[string]any{}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", false, fmt.Errorf("bad arguments: %w", err)
		}
	}
	var v struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := s.call(ctx, "tools/call", map[string]any{"name": tool, "arguments": args}, &v); err != nil {
		return "", false, err
	}
	var b strings.Builder
	for _, c := range v.Content {
		if c.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(c.Text)
		}
	}
	return b.String(), v.IsError, nil
}

// Close stops the server process.
func (s *Server) Close() error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	return s.cmd.Process.Kill()
}

type limitedWriter struct {
	sb  *strings.Builder
	max int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.sb.Len() < w.max {
		w.sb.Write(p)
	}
	return len(p), nil
}

// ServerStatus reports one backend for /mcp and doctor.
type ServerStatus struct {
	Name  string
	OK    bool
	Tools int
	Err   string
}

// Manager owns configured servers.
type Manager struct {
	mu      sync.Mutex
	servers map[string]*Server
	status  map[string]ServerStatus
}

// NewManager builds an empty manager.
func NewManager() *Manager {
	return &Manager{servers: map[string]*Server{}, status: map[string]ServerStatus{}}
}

// Start launches every configured server; one failure never blocks others.
func (m *Manager) Start(ctx context.Context, cfg map[string]ServerConfig) {
	names := make([]string, 0, len(cfg))
	for n := range cfg {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s, err := Start(ctx, n, cfg[n])
		m.mu.Lock()
		if err != nil {
			m.status[n] = ServerStatus{Name: n, Err: err.Error()}
		} else {
			m.servers[n] = s
			m.status[n] = ServerStatus{Name: n, OK: true, Tools: len(s.tools)}
		}
		m.mu.Unlock()
	}
}

// Status snapshots backend health.
func (m *Manager) Status() []ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ServerStatus, 0, len(m.status))
	for _, st := range m.status {
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// AsTools converts healthy servers' tools for the registry. Names are
// namespaced (mcp_<server>_<tool>); first registration wins on collision.
// MCP actions are third-party and opaque, so they always ask unless the
// policy auto-approves everything.
func (m *Manager) AsTools(reg *ct.Registry) int {
	m.mu.Lock()
	names := make([]string, 0, len(m.servers))
	for n := range m.servers {
		names = append(names, n)
	}
	sort.Strings(names)
	seen := map[string]bool{}
	added := 0
	for _, n := range names {
		s := m.servers[n]
		for _, t := range s.tools {
			toolName := ToolName(n, t.Name)
			if seen[toolName] {
				continue
			}
			if _, exists := reg.Get(toolName); exists {
				continue
			}
			seen[toolName] = true
			desc := t.Description
			if desc == "" {
				desc = t.Name
			}
			srv, tool := s, t.Name
			reg.AddTool(ct.Tool{
				Name:        toolName,
				Description: "MCP " + n + ": " + desc,
				Parameters:  t.Schema,
				Run: func(ctx context.Context, argsJSON string) (ct.Result, error) {
					if !reg.Policy.AutoApprove && !reg.RequestApproval("mcp "+toolName, argsJSON) {
						return ct.Result{Content: "mcp tool denied by approval policy", IsError: true}, nil
					}
					content, isErr, err := srv.CallTool(ctx, tool, argsJSON)
					if err != nil {
						return ct.Result{Content: "mcp error: " + err.Error(), IsError: true}, nil
					}
					if content == "" {
						content = "(empty result)"
					}
					return ct.Result{Content: content, IsError: isErr}, nil
				},
			})
			added++
		}
	}
	m.mu.Unlock()
	return added
}

// Close stops all servers.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.servers {
		_ = s.Close()
	}
	m.servers = map[string]*Server{}
}
