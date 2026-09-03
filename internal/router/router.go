package router

import (
	"context"
	"fmt"
	"strings"
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
	var trail []string
	authDead := map[string]bool{}
	tried := 0
	idx := 0
	lastBackend, lastTransient := "", false
	for tried < maxAttempts {
		sel := diversify(ranked, idx, lastBackend, lastTransient)
		if sel < 0 {
			break
		}
		ranked[sel], ranked[idx] = ranked[idx], ranked[sel]
		cand := ranked[idx]
		idx++
		// An auth failure poisons the whole backend for this turn:
		// different credentials won't appear for its next model.
		if authDead[cand.Entry.Backend.Name()] {
			continue
		}
		if tried > 0 && r.OnSwitch != nil {
			r.OnSwitch(SwitchInfo{From: prev, To: cand.Entry.Model.FullID(), Reason: redactErr(lastErr), Index: tried + 1})
		}
		if tried > 0 && r.Backoff > 0 {
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
		tried++
		lastBackend, lastTransient = cand.Entry.Backend.Name(), false
		if err == nil {
			r.Reg.Tracker().RecordSuccess(key, time.Since(start))
			return resp, cand.Entry, nil
		}
		r.Reg.Tracker().RecordFailure(key, err)
		lastErr = err
		prev = cand.Entry.Model.FullID()
		trail = append(trail, cand.Entry.Model.FullID()+" ("+string(errCode(err))+")")
		if isAuthErr(err) {
			authDead[cand.Entry.Backend.Name()] = true
		}
		if isTransientErr(err) {
			lastTransient = true
		}
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
	return nil, Entry{}, withTrail(lastErr, trail)
}

// withTrail appends the per-attempt record so failures name every model
// tried instead of only the last one.
func withTrail(err error, trail []string) error {
	if len(trail) == 0 {
		return err
	}
	if pe, ok := err.(*core.ProviderError); ok && pe != nil {
		cp := *pe
		cp.Message = fmt.Sprintf("%s [tried: %s]", pe.Message, strings.Join(trail, ", "))
		return &cp
	}
	return fmt.Errorf("%w [tried: %s]", err, strings.Join(trail, ", "))
}

func errCode(err error) core.ErrorCode {
	if pe, ok := err.(*core.ProviderError); ok && pe != nil {
		return pe.Code
	}
	return core.ErrUnknown
}

func isAuthErr(err error) bool {
	return errCode(err) == core.ErrAuth
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
	var trail []string
	authDead := map[string]bool{}
	tried := 0
	idx := 0
	lastBackend, lastTransient := "", false
	for tried < maxAttempts {
		sel := diversify(ranked, idx, lastBackend, lastTransient)
		if sel < 0 {
			break
		}
		ranked[sel], ranked[idx] = ranked[idx], ranked[sel]
		cand := ranked[idx]
		idx++
		if authDead[cand.Entry.Backend.Name()] {
			continue
		}
		if tried > 0 && r.OnSwitch != nil {
			r.OnSwitch(SwitchInfo{From: prev, To: cand.Entry.Model.FullID(), Reason: redactErr(lastErr), Index: tried + 1})
		}
		if tried > 0 && r.Backoff > 0 {
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
			tried++
			lastBackend, lastTransient = cand.Entry.Backend.Name(), isTransientErr(err)
			r.Reg.Tracker().RecordFailure(key, err)
			lastErr = err
			prev = cand.Entry.Model.FullID()
			trail = append(trail, cand.Entry.Model.FullID()+" ("+string(errCode(err))+")")
			if isAuthErr(err) {
				authDead[cand.Entry.Backend.Name()] = true
			}
			if !core.RetryableAcrossModels(err) {
				return nil, Entry{}, err
			}
			continue
		}
		resp, err := consume(ch)
		tried++
		lastBackend, lastTransient = cand.Entry.Backend.Name(), false
		if err != nil {
			r.Reg.Tracker().RecordFailure(key, err)
			lastErr = err
			prev = cand.Entry.Model.FullID()
			trail = append(trail, cand.Entry.Model.FullID()+" ("+string(errCode(err))+")")
			if isAuthErr(err) {
				authDead[cand.Entry.Backend.Name()] = true
			}
			if isTransientErr(err) {
				lastTransient = true
			}
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
	return nil, Entry{}, withTrail(lastErr, trail)
}

// diversify picks the next candidate index: after a transient backend
// failure (overload/rate-limit/network — often family-wide), the best
// remaining model from a different backend goes first so one flaky
// family can't consume the whole attempt budget. Happy-path order is
// untouched: this only engages once something already failed.
func diversify(ranked []Scored, from int, failedBackend string, transient bool) int {
	if from >= len(ranked) {
		return -1
	}
	if transient && failedBackend != "" {
		for j := from; j < len(ranked); j++ {
			if ranked[j].Entry.Backend.Name() != failedBackend {
				return j
			}
		}
	}
	return from
}

// isTransientErr reports failures that often affect a backend's whole
// family at once (as opposed to per-model quota or dead credentials).
func isTransientErr(err error) bool {
	switch errCode(err) {
	case core.ErrOverloaded, core.ErrRateLimited, core.ErrNetwork:
		return true
	default:
		return false
	}
}

func redactErr(err error) string {
	if err == nil {
		return ""
	}
	return core.Redact(err.Error())
}
