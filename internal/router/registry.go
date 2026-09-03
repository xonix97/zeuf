// Package router implements Zeuf's model registry, smart routing and
// automatic fallback. Routing never picks the first available model: task
// requirements filter the pool, a pluggable scorer ranks survivors, and
// the executor walks the ranking with retry budgets, cooldowns and
// backoff. The session is untouched by switching — only the backend
// changes.
package router

import (
	"context"
	"sort"
	"sync"
	"time"

	"zeuf/internal/core"
	"zeuf/internal/providers"
)

// Entry binds a discovered model to the adapter that serves it.
type Entry struct {
	Model   core.ModelInfo
	Backend providers.Adapter
}

// Registry holds backends, discovered models and health state.
type Registry struct {
	mu       sync.Mutex
	backends map[string]providers.Adapter
	models   []Entry
	health   *Tracker
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{backends: map[string]providers.Adapter{}, health: NewTracker()}
}

// Register adds a backend adapter.
func (r *Registry) Register(b providers.Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends[b.Name()] = b
}

// Backends returns registered backend names.
func (r *Registry) Backends() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.backends))
	for n := range r.backends {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Refresh rediscovers models from every backend. A backend that fails to
// list models keeps its previously known models but is marked in health.
func (r *Registry) Refresh(ctx context.Context) {
	r.mu.Lock()
	backends := make([]providers.Adapter, 0, len(r.backends))
	for _, b := range r.backends {
		backends = append(backends, b)
	}
	r.mu.Unlock()

	type result struct {
		b  providers.Adapter
		ms []core.ModelInfo
	}
	ch := make(chan result, len(backends))
	for _, b := range backends {
		go func(b providers.Adapter) {
			cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			ms, err := b.ListModels(cctx)
			if err != nil {
				r.health.RecordFailure(b.Name(), err)
				ch <- result{b: b}
				return
			}
			ch <- result{b: b, ms: ms}
		}(b)
	}
	fresh := map[string][]Entry{}
	for range backends {
		res := <-ch
		if len(res.ms) == 0 {
			continue
		}
		for _, m := range res.ms {
			fresh[res.b.Name()] = append(fresh[res.b.Name()], Entry{Model: m, Backend: res.b})
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []Entry
	for _, es := range fresh {
		all = append(all, es...)
	}
	if len(all) > 0 {
		r.models = all
	}
}

// Models returns a snapshot of known models with live availability applied.
func (r *Registry) Models() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Entry, len(r.models))
	copy(out, r.models)
	for i := range out {
		if st := r.health.State(out[i].Backend.Name() + "/" + out[i].Model.ID); st != nil && time.Now().Before(st.CooldownUntil) {
			out[i].Model.Availability = st.Availability
			out[i].Model.LastError = st.LastError
		}
	}
	return out
}

// SetModels overrides discovery (used by tests and pinned configs).
func (r *Registry) SetModels(es []Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models = append([]Entry(nil), es...)
}

// FreeOnly returns the entries affirmatively known to be free (explicit
// free flag or known zero cost from the backend). Paid and unknown-cost
// models are excluded — never guessed from names.
func FreeOnly(es []Entry) []Entry {
	out := make([]Entry, 0, len(es))
	for _, e := range es {
		if e.Model.IsFree {
			out = append(out, e)
		}
	}
	return out
}

// Tracker exposes health for tests/UIs.
func (r *Registry) Tracker() *Tracker { return r.health }
