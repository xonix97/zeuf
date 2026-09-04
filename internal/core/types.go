// Package core defines the provider-agnostic types that the whole of Zeuf
// (agent loop, router, providers, UI) shares. No provider-specific logic
// lives here or in the agent runtime.
package core

import (
	"fmt"
	"strings"
	"time"
)

// ---- Conversation ----------------------------------------------------------

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall is a structured request from the model to invoke a tool.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON object, unparsed at this layer
}

// Message is one provider-agnostic conversation turn.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolDef describes a tool offered to the model.
type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  string `json:"parameters"` // JSON Schema
}

// ChatRequest is a single inference turn, normalized across providers.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
}

// Usage carries token/context information when the provider exposes it.
type Usage struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning,omitempty"`
}

// ChatResponse is the normalized non-streaming result.
type ChatResponse struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Usage     Usage      `json:"usage"`
	Model     string     `json:"model"`
	Provider  string     `json:"provider"`
}

// ---- Streaming -------------------------------------------------------------

type StreamEventType string

const (
	EventToken        StreamEventType = "token"
	EventReasoning    StreamEventType = "reasoning"
	EventTool         StreamEventType = "tool_call"
	EventToolProgress StreamEventType = "tool_progress"
	EventDone         StreamEventType = "done"
	EventError        StreamEventType = "error"
	EventInfo         StreamEventType = "info"
)

// StreamEvent is one normalized streaming unit.
type StreamEvent struct {
	Type      StreamEventType `json:"type"`
	Delta     string          `json:"delta,omitempty"`
	ToolCalls []ToolCall      `json:"tool_calls,omitempty"`
	Usage     Usage           `json:"usage,omitempty"`
	// Tool carries the tool name for tool_progress events.
	Tool string `json:"tool,omitempty"`
	// Done marks a tool_progress completion (false = just started).
	Done bool `json:"done,omitempty"`
	// Ok reports delegated tool success for tool_progress completions.
	Ok  bool  `json:"ok,omitempty"`
	Err error `json:"-"`
}

// ---- Execution, Verification & Observability -------------------------------

// ToolStatus classifies tool outcomes strictly.
type ToolStatus string

const (
	ToolStatusSuccess            ToolStatus = "success"
	ToolStatusFailure            ToolStatus = "failure"
	ToolStatusPermissionRequired ToolStatus = "permission_required"
	ToolStatusTimeout            ToolStatus = "timeout"
	ToolStatusCancelled          ToolStatus = "cancelled"
)

// VerificationResult is the structured, factual record of a verification step.
type VerificationResult struct {
	TaskID           string        `json:"task_id"`
	Command          string        `json:"command"`
	ExitCode         int           `json:"exit_code"`
	Duration         time.Duration `json:"duration"`
	Stdout           string        `json:"stdout"`
	Stderr           string        `json:"stderr"`
	Passed           bool          `json:"passed"`
	FailureDiagnosis string        `json:"failure_diagnosis,omitempty"`
}

// TraceEvent records one lifecycle step for observability and diagnostics.
type TraceEvent struct {
	Timestamp time.Time     `json:"timestamp"`
	Kind      string        `json:"kind"` // "state_transition", "task_status", "model_call", "model_switch", "tool_call", "subagent_lifecycle", "verification", "error", "retry"
	State     string        `json:"state,omitempty"`
	TaskID    string        `json:"task_id,omitempty"`
	Model     string        `json:"model,omitempty"`
	Tool      string        `json:"tool,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
	Details   string        `json:"details,omitempty"`
}

// ---- Model metadata --------------------------------------------------------

// Capabilities describes what a model can do, when the provider exposes it.
type Capabilities struct {
	ContextLength     int  `json:"context_length"`
	MaxOutput         int  `json:"max_output,omitempty"`
	SupportsTools     bool `json:"supports_tools"`
	SupportsStreaming bool `json:"supports_streaming"`
	Vision            bool `json:"vision,omitempty"`
}

// Scores holds ranking inputs. A negative value means "unknown" and must be
// displayed as Unknown — never presented as a measured fact.
type Scores struct {
	Quality   float64 `json:"quality"`
	Coding    float64 `json:"coding"`
	Reasoning float64 `json:"reasoning"`
	Latency   float64 `json:"latency"`
}

// UnknownScores returns a Scores where every dimension is unknown.
func UnknownScores() Scores { return Scores{-1, -1, -1, -1} }

// Known reports whether a dimension holds a real value in [0,1].
func (s Scores) Known(v float64) bool { return v >= 0 && v <= 1 }

// Availability is the last-observed usability of a model.
type Availability string

const (
	AvailAvailable   Availability = "available"
	AvailRateLimited Availability = "rate_limited"
	AvailQuotaOut    Availability = "quota_exhausted"
	AvailAuthError   Availability = "auth_error"
	AvailOffline     Availability = "offline"
	AvailUnknown     Availability = "unknown"
)

// ModelInfo is the registry entry for one model behind any provider.
type ModelInfo struct {
	ID           string       `json:"id"` // provider-scoped, e.g. "mimo-v2.5-free"
	Provider     string       `json:"provider"`
	DisplayName  string       `json:"display_name"`
	Caps         Capabilities `json:"capabilities"`
	Scores       Scores       `json:"scores"`
	Availability Availability `json:"availability"`
	QuotaState   string       `json:"quota_state"` // "unknown" unless provider exposes it
	LastError    string       `json:"last_error,omitempty"`
	// Pricing facts, when the backend exposes them. IsFree is true only on
	// affirmative evidence (explicit free flag or known zero cost) — never
	// guessed from the model name.
	IsFree     bool    `json:"is_free"`
	CostKnown  bool    `json:"cost_known"`
	CostInput  float64 `json:"cost_input,omitempty"`
	CostOutput float64 `json:"cost_output,omitempty"`
}

// FullID returns "provider/model".
func (m ModelInfo) FullID() string { return m.Provider + "/" + m.ID }

// ---- Provider errors -------------------------------------------------------

type ErrorCode string

const (
	ErrRateLimited ErrorCode = "rate_limited"
	ErrQuotaOut    ErrorCode = "quota_exhausted"
	ErrAuth        ErrorCode = "auth_failure"
	ErrNetwork     ErrorCode = "network_error"
	ErrOverloaded  ErrorCode = "provider_overloaded"
	ErrUnsupported ErrorCode = "unsupported_request"
	ErrUnknown     ErrorCode = "unknown"
)

// ProviderError is the normalized failure surfaced by every adapter.
type ProviderError struct {
	Code       ErrorCode     `json:"code"`
	Message    string        `json:"message"`
	Provider   string        `json:"provider,omitempty"`
	Model      string        `json:"model,omitempty"`
	StatusCode int           `json:"status_code,omitempty"`
	RetryAfter time.Duration `json:"-"`
}

func (e *ProviderError) Error() string {
	if e.Provider != "" || e.Model != "" {
		return fmt.Sprintf("%s (provider=%s model=%s code=%s)", e.Message, e.Provider, e.Model, e.Code)
	}
	return fmt.Sprintf("%s (code=%s)", e.Message, e.Code)
}

// RetryableAcrossModels reports whether trying a *different* model is sane.
// Every well-classified provider failure is fallback-eligible; only caller
// cancellation / programming errors are not.
func RetryableAcrossModels(err error) bool {
	pe, ok := err.(*ProviderError)
	if !ok || pe == nil {
		return false
	}
	switch pe.Code {
	case ErrRateLimited, ErrQuotaOut, ErrAuth, ErrNetwork, ErrOverloaded, ErrUnsupported:
		return true
	default:
		return false
	}
}

// ClassifyMessage maps free-form provider/CLI error text to an ErrorCode.
// Used when a backend (e.g. a CLI gateway) does not return machine-readable
// error codes.
func ClassifyMessage(msg string) ErrorCode {
	l := strings.ToLower(msg)
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(l, s) {
				return true
			}
		}
		return false
	}
	switch {
	case has("insufficient_quota", "quota_exhausted", "quota exceeded", "quota exhausted", "credit balance", "out of credits"):
		return ErrQuotaOut
	case has("rate limit", "rate_limit", "rate-limited", "too many requests", "429", "retry in", "retry_after"):
		return ErrRateLimited
	case has("unauthorized", "unauthenticated", "invalid api key", "incorrect api key", "invalid_api_key", "401", "forbidden", "login required", "not logged in", "auth method", "authentication required", "api key required", "ineligible", "no longer supported"):
		return ErrAuth
	case has("model not found", "unknown model", "does not exist", "unsupported", "not supported", "404"):
		return ErrUnsupported
	case has("overloaded", "capacity", "503", "502", "server error", "internal error", "try again"):
		return ErrOverloaded
	case has("timeout", "timed out", "connection refused", "connection reset", "no such host", "network", "econn", "dial tcp"):
		return ErrNetwork
	default:
		return ErrUnknown
	}
}

// ClassifyHTTPStatus maps an HTTP status to an ErrorCode.
func ClassifyHTTPStatus(status int, body string) ErrorCode {
	switch {
	case status == 429:
		if ClassifyMessage(body) == ErrQuotaOut {
			return ErrQuotaOut
		}
		return ErrRateLimited
	case status == 401 || status == 403:
		return ErrAuth
	case status == 404 || status == 422:
		return ErrUnsupported
	case status >= 500:
		return ErrOverloaded
	default:
		return ClassifyMessage(body)
	}
}
