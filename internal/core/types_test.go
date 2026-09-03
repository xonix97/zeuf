package core

import (
	"strings"
	"testing"
)

func TestClassifyMessage(t *testing.T) {
	cases := map[string]ErrorCode{
		"rate limit exceeded, retry in 42s":  ErrRateLimited,
		"HTTP 429 too many requests":         ErrRateLimited,
		"insufficient_quota: out of credits": ErrQuotaOut,
		"quota exhausted for today":          ErrQuotaOut,
		"unauthorized: invalid api key":      ErrAuth,
		"401 login required":                 ErrAuth,
		"model not found: foo":               ErrUnsupported,
		"unsupported request":                ErrUnsupported,
		"upstream overloaded, try again":     ErrOverloaded,
		"connection refused":                 ErrNetwork,
		"something completely novel":         ErrUnknown,
	}
	for msg, want := range cases {
		if got := ClassifyMessage(msg); got != want {
			t.Errorf("ClassifyMessage(%q) = %s, want %s", msg, got, want)
		}
	}
}

func TestClassifyHTTPStatus(t *testing.T) {
	if got := ClassifyHTTPStatus(429, `{"error":"insufficient_quota"}`); got != ErrQuotaOut {
		t.Errorf("429+quota = %s, want quota_exhausted", got)
	}
	if got := ClassifyHTTPStatus(429, "slow down"); got != ErrRateLimited {
		t.Errorf("429 = %s, want rate_limited", got)
	}
	if got := ClassifyHTTPStatus(401, ""); got != ErrAuth {
		t.Errorf("401 = %s", got)
	}
	if got := ClassifyHTTPStatus(503, ""); got != ErrOverloaded {
		t.Errorf("503 = %s", got)
	}
}

func TestRetryableAcrossModels(t *testing.T) {
	for _, c := range []ErrorCode{ErrRateLimited, ErrQuotaOut, ErrAuth, ErrNetwork, ErrOverloaded, ErrUnsupported} {
		if !RetryableAcrossModels(&ProviderError{Code: c}) {
			t.Errorf("code %s should be fallback-eligible", c)
		}
	}
	if RetryableAcrossModels(&ProviderError{Code: ErrUnknown}) {
		t.Error("unknown code should not trigger fallback")
	}
}

func TestRedact(t *testing.T) {
	in := "Authorization: Bearer sk-abc123XYZ456\nOPENAI_API_KEY=sk-secret-value-here\nmodel: mimo is fine\npassword: hunter2secret"
	out := Redact(in)
	if strings.Contains(out, "sk-abc123XYZ456") || strings.Contains(out, "sk-secret-value-here") || strings.Contains(out, "hunter2secret") {
		t.Errorf("credentials leaked through Redact: %q", out)
	}
	if !strings.Contains(out, "mimo is fine") {
		t.Errorf("non-secret text was mangled: %q", out)
	}
}

func TestFullID(t *testing.T) {
	m := ModelInfo{ID: "x-free", Provider: "opencode"}
	if m.FullID() != "opencode/x-free" {
		t.Errorf("FullID = %q", m.FullID())
	}
	if UnknownScores().Coding >= 0 {
		t.Error("unknown scores must be negative (displayed as Unknown, never faked)")
	}
}
