package router

import (
	"sync"
	"time"

	"zeuf/internal/core"
)

// ModelState tracks recent health per model key ("backend/modelID").
type ModelState struct {
	Key              string
	ConsecutiveFails int
	CooldownUntil    time.Time
	LatencyEMA       time.Duration
	Availability     core.Availability
	LastError        string
	LastCheck        time.Time
}

// Tracker records successes, failures and latency for routing decisions.
type Tracker struct {
	mu     sync.Mutex
	states map[string]*ModelState
}

// NewTracker builds a tracker.
func NewTracker() *Tracker { return &Tracker{states: map[string]*ModelState{}} }

func (t *Tracker) state(key string) *ModelState {
	st, ok := t.states[key]
	if !ok {
		st = &ModelState{Key: key, Availability: core.AvailAvailable}
		t.states[key] = st
	}
	return st
}

// State returns a copy of the state for key, or nil if unseen.
func (t *Tracker) State(key string) *ModelState {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.states[key]
	if !ok {
		return nil
	}
	cp := *st
	return &cp
}

// RecordSuccess clears failure streaks and folds latency into an EMA.
func (t *Tracker) RecordSuccess(key string, latency time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.state(key)
	st.ConsecutiveFails = 0
	st.CooldownUntil = time.Time{}
	st.Availability = core.AvailAvailable
	st.LastError = ""
	st.LastCheck = time.Now()
	if latency > 0 {
		if st.LatencyEMA == 0 {
			st.LatencyEMA = latency
		} else {
			st.LatencyEMA = (st.LatencyEMA + latency) / 2
		}
	}
}

// RecordFailure applies a cooldown scaled by error kind and streak:
// rate limits back off briefly, quota exhaustion parks the model longer,
// auth/network/overload sit in between. Exponential growth is capped.
func (t *Tracker) RecordFailure(key string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.state(key)
	st.ConsecutiveFails++
	st.LastCheck = time.Now()
	base := 30 * time.Second
	avail := core.AvailUnknown
	if pe, ok := err.(*core.ProviderError); ok && pe != nil {
		st.LastError = string(pe.Code)
		switch pe.Code {
		case core.ErrRateLimited:
			base = 45 * time.Second
			avail = core.AvailRateLimited
		case core.ErrQuotaOut:
			base = 10 * time.Minute
			avail = core.AvailQuotaOut
		case core.ErrAuth:
			base = 5 * time.Minute
			avail = core.AvailAuthError
		case core.ErrNetwork, core.ErrOverloaded:
			base = 60 * time.Second
			avail = core.AvailOffline
		case core.ErrUnsupported:
			base = 30 * time.Minute
			avail = core.AvailUnknown
		}
		if pe.RetryAfter > 0 && pe.RetryAfter < 15*time.Minute {
			base = pe.RetryAfter
		}
	} else if err != nil {
		st.LastError = "unknown: " + err.Error()
	}
	// Exponential backoff with cap: base * 2^(fails-1), max 30m.
	backoff := base
	for i := 1; i < st.ConsecutiveFails && backoff < 30*time.Minute; i++ {
		backoff *= 2
		if backoff > 30*time.Minute {
			backoff = 30 * time.Minute
		}
	}
	st.CooldownUntil = time.Now().Add(backoff)
	st.Availability = avail
}
