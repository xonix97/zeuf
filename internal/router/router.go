package router

import (
	"context"
	"time"

	"zeuf/internal/core"
)

// SwitchInfo notifies the UI about an automatic model change.
type SwitchInfo struct {
	From   string // previous model FullID ("" on first pick)
	To     string // new model FullID
	Reason string // human-readable, redacted cause
	Index  int    // 1-based attempt number
}

// Router selects models and executes turns with automatic fallback.
type Router struct {
	Reg    *Registry
	Scorer Scorer

	// OnSwitch, when set, is called before each fallback attempt so the
	// UI can show a subtle "continuing with <model>" notice.
	OnSwitch func(SwitchInfo)

	// Backoff bounds the pause between fallback attempts.
	Backoff time.Duration
}

// New builds a router over reg.
func New(reg *Registry) *Router {
	return &Router{Reg: reg, Scorer: DefaultScorer, Backoff: 2 * time.Second}
}

// Ranked returns the current ranking for req/prefs (best first).
func (r *Router) Ranked(req TaskReq, prefs Prefs) []Scored {
	return Rank(r.Reg.Models(), req, prefs, r.Reg.Tracker(), r.Scorer)
}

// Do executes fn against the best-ranked model, falling back through the
// ranking on retryable provider failures. Non-retryable errors return
// immediately. The session is owned by the caller and never reset here —
// fallback only changes which backend executes the next attempt.
func (r *Router) Do(
	ctx context.Context,
	req core.ChatRequest,
	task TaskReq,
	prefs Prefs,
	fn func(e Entry, req core.ChatRequest) (*core.ChatResponse, error),
) (*core.ChatResponse, Entry, error) {
	ranked := Rank(r.Reg.Models(), task, prefs, r.Reg.Tracker(), r.Scorer)
	if len(ranked) == 0 {
		return nil, Entry{}, &core.ProviderError{Code: core.ErrUnknown, Message: "no compatible models available (all filtered or cooling down)"}
	}
	maxAttempts := prefs.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 4
	}
	if !prefs.FallbackEnabled {
		maxAttempts = 1
	}
	if maxAttempts > len(ranked) {
		maxAttempts = len(ranked)
	}
	var prev string
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		cand := ranked[i]
		if i > 0 && r.OnSwitch != nil {
			r.OnSwitch(SwitchInfo{From: prev, To: cand.Entry.Model.FullID(), Reason: redactErr(lastErr), Index: i + 1})
		}
		if i > 0 && r.Backoff > 0 {
			select {
			case <-ctx.Done():
				return nil, Entry{}, ctx.Err()
			case <-time.After(r.Backoff):
			}
		}
		key := cand.Entry.Backend.Name() + "/" + cand.Entry.Model.ID
		start := time.Now()
		attempt := req
		attempt.Model = cand.Entry.Model.ID
		resp, err := fn(cand.Entry, attempt)
		if err == nil {
			r.Reg.Tracker().RecordSuccess(key, time.Since(start))
			return resp, cand.Entry, nil
		}
		r.Reg.Tracker().RecordFailure(key, err)
		lastErr = err
		prev = cand.Entry.Model.FullID()
		if !core.RetryableAcrossModels(err) {
			return nil, Entry{}, err
		}
		if ctx.Err() != nil {
			return nil, Entry{}, ctx.Err()
		}
	}
	if lastErr == nil {
		lastErr = &core.ProviderError{Code: core.ErrUnknown, Message: "all models failed"}
	}
	return nil, Entry{}, lastErr
}

// DoStream is Do for streaming turns.
func (r *Router) DoStream(
	ctx context.Context,
	req core.ChatRequest,
	task TaskReq,
	prefs Prefs,
	fn func(e Entry, req core.ChatRequest) (<-chan core.StreamEvent, error),
	consume func(ch <-chan core.StreamEvent) (*core.ChatResponse, error),
) (*core.ChatResponse, Entry, error) {
	ranked := Rank(r.Reg.Models(), task, prefs, r.Reg.Tracker(), r.Scorer)
	if len(ranked) == 0 {
		return nil, Entry{}, &core.ProviderError{Code: core.ErrUnknown, Message: "no compatible models available (all filtered or cooling down)"}
	}
	maxAttempts := prefs.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 4
	}
	if !prefs.FallbackEnabled {
		maxAttempts = 1
	}
	if maxAttempts > len(ranked) {
		maxAttempts = len(ranked)
	}
	var prev string
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		cand := ranked[i]
		if i > 0 && r.OnSwitch != nil {
			r.OnSwitch(SwitchInfo{From: prev, To: cand.Entry.Model.FullID(), Reason: redactErr(lastErr), Index: i + 1})
		}
		if i > 0 && r.Backoff > 0 {
			select {
			case <-ctx.Done():
				return nil, Entry{}, ctx.Err()
			case <-time.After(r.Backoff):
			}
		}
		key := cand.Entry.Backend.Name() + "/" + cand.Entry.Model.ID
		start := time.Now()
		attempt := req
		attempt.Model = cand.Entry.Model.ID
		ch, err := fn(cand.Entry, attempt)
		if err != nil {
			r.Reg.Tracker().RecordFailure(key, err)
			lastErr = err
			prev = cand.Entry.Model.FullID()
			if !core.RetryableAcrossModels(err) {
				return nil, Entry{}, err
			}
			continue
		}
		resp, err := consume(ch)
		if err != nil {
			r.Reg.Tracker().RecordFailure(key, err)
			lastErr = err
			prev = cand.Entry.Model.FullID()
			if !core.RetryableAcrossModels(err) {
				return nil, Entry{}, err
			}
			continue
		}
		r.Reg.Tracker().RecordSuccess(key, time.Since(start))
		return resp, cand.Entry, nil
	}
	if lastErr == nil {
		lastErr = &core.ProviderError{Code: core.ErrUnknown, Message: "all models failed"}
	}
	return nil, Entry{}, lastErr
}

func redactErr(err error) string {
	if err == nil {
		return ""
	}
	return core.Redact(err.Error())
}
