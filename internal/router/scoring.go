package router

import (
	"strings"
	"time"

	"zeuf/internal/core"
)

// TaskReq captures what a task needs from a model.
type TaskReq struct {
	NeedTools    bool
	MinContext   int
	PreferCoding bool
	PreferReason bool
	Hint         string
}

// ClassifyTask derives requirements from the task text. Keyword based and
// deliberately conservative: unknown tasks get no preference, never a
// fabricated capability filter.
func ClassifyTask(task string) TaskReq {
	l := strings.ToLower(task)
	req := TaskReq{Hint: task}
	codeWords := []string{"code", "edit", "fix", "bug", "refactor", "implement", "function", "compile", "test", "debug", "repo", "file"}
	for _, w := range codeWords {
		if strings.Contains(l, w) {
			req.PreferCoding = true
			req.NeedTools = true
			break
		}
	}
	reasonWords := []string{"prove", "reason", "plan", "design", "architect", "analyze", "compare", "why"}
	for _, w := range reasonWords {
		if strings.Contains(l, w) {
			req.PreferReason = true
			break
		}
	}
	if strings.Contains(l, "large repo") || strings.Contains(l, "whole codebase") || strings.Contains(l, "monorepo") {
		req.MinContext = 100000
	}
	return req
}

// Mode selects the routing strategy.
type Mode string

const (
	ModeAuto     Mode = "auto"
	ModeBalanced Mode = "balanced"
	ModeFastest  Mode = "fastest"
	ModeQuality  Mode = "quality"
)

// Prefs are user routing preferences.
type Prefs struct {
	Mode               Mode     `json:"mode"`
	PinnedModel        string   `json:"pinned_model,omitempty"` // "provider/id" or "id"
	PreferredProviders []string `json:"preferred_providers,omitempty"`
	DisabledProviders  []string `json:"disabled_providers,omitempty"`
	FallbackEnabled    bool     `json:"fallback_enabled"`
	MaxAttempts        int      `json:"max_attempts,omitempty"`
}

// DefaultPrefs returns safe defaults (fallback on, 4 attempts).
func DefaultPrefs() Prefs {
	return Prefs{Mode: ModeAuto, FallbackEnabled: true, MaxAttempts: 4}
}

// Weights for the default scorer. Exported so custom scorers and future
// ranking updates can reuse or replace them.
type Weights struct {
	Coding    float64
	Reasoning float64
	Quality   float64
	Speed     float64
	Freshness float64 // bonus for healthy/no recent failures
}

// WeightsFor maps a mode to weights.
func WeightsFor(m Mode) Weights {
	switch m {
	case ModeFastest:
		return Weights{Coding: 0.15, Reasoning: 0.1, Quality: 0.1, Speed: 0.55, Freshness: 0.1}
	case ModeQuality:
		return Weights{Coding: 0.3, Reasoning: 0.3, Quality: 0.3, Speed: 0.0, Freshness: 0.1}
	case ModeBalanced:
		return Weights{Coding: 0.25, Reasoning: 0.2, Quality: 0.2, Speed: 0.25, Freshness: 0.1}
	default: // auto: task decides; defaults lean coding-capable + available
		return Weights{Coding: 0.3, Reasoning: 0.2, Quality: 0.15, Speed: 0.15, Freshness: 0.2}
	}
}

// Scorer ranks a candidate. Higher is better. Returning false excludes it.
type Scorer func(e Entry, req TaskReq, prefs Prefs, t *Tracker) (float64, bool)

// DefaultScorer filters incompatible models (disabled provider, missing
// tools, too-small context, cooling down) and scores the rest with the
// mode weights. Unknown score dimensions contribute a neutral 0.5 —
// explicitly not a fabricated measurement.
func DefaultScorer(e Entry, req TaskReq, prefs Prefs, t *Tracker) (float64, bool) {
	for _, d := range prefs.DisabledProviders {
		if e.Backend.Name() == d || e.Model.Provider == d {
			return 0, false
		}
	}
	if req.NeedTools && !e.Model.Caps.SupportsTools && e.Model.Caps.ContextLength != 0 {
		// Delegated gateways run tools server-side even when the raw model
		// capability is unexposed; only filter when we positively know.
		return 0, false
	}
	if req.MinContext > 0 && e.Model.Caps.ContextLength > 0 && e.Model.Caps.ContextLength < req.MinContext {
		return 0, false
	}
	if st := t.State(e.Backend.Name() + "/" + e.Model.ID); st != nil {
		if time.Now().Before(st.CooldownUntil) {
			return 0, false
		}
	}
	w := WeightsFor(prefs.Mode)
	if prefs.Mode == ModeAuto {
		if req.PreferCoding {
			w.Coding += 0.1
		}
		if req.PreferReason {
			w.Reasoning += 0.1
		}
	}
	neutral := func(v float64) float64 {
		if v < 0 || v > 1 {
			return 0.5
		}
		return v
	}
	score := w.Coding*neutral(e.Model.Scores.Coding) +
		w.Reasoning*neutral(e.Model.Scores.Reasoning) +
		w.Quality*neutral(e.Model.Scores.Quality) +
		w.Speed*neutral(e.Model.Scores.Latency) +
		w.Freshness*freshness(e, t)
	for _, p := range prefs.PreferredProviders {
		if e.Backend.Name() == p || e.Model.Provider == p {
			score += 0.05
		}
	}
	return score, true
}

func freshness(e Entry, t *Tracker) float64 {
	st := t.State(e.Backend.Name() + "/" + e.Model.ID)
	if st == nil || st.ConsecutiveFails == 0 {
		return 1
	}
	if st.ConsecutiveFails == 1 {
		return 0.6
	}
	return 0.3
}

// Scored is a ranked candidate.
type Scored struct {
	Entry Entry
	Score float64
}

// Rank filters and sorts candidates (best first). A pinned model, when
// present in the pool, wins regardless of score.
func Rank(models []Entry, req TaskReq, prefs Prefs, t *Tracker, scorer Scorer) []Scored {
	if scorer == nil {
		scorer = DefaultScorer
	}
	if prefs.PinnedModel != "" {
		for _, e := range models {
			if e.Model.FullID() == prefs.PinnedModel || e.Model.ID == prefs.PinnedModel {
				return []Scored{{Entry: e, Score: 1 << 62}}
			}
		}
	}
	// Never send a turn to a model observed as unusable (auth failure,
	// quota out, offline, rate limited) while usable ones exist. As a last
	// resort the dead ones are still attempted — a clear error beats
	// silence, and state may have recovered.
	models = usableModels(models)
	var out []Scored
	for _, e := range models {
		s, ok := scorer(e, req, prefs, t)
		if ok {
			out = append(out, Scored{Entry: e, Score: s})
		}
	}
	sortByScore(out)
	return out
}

// usableModels prefers models not observed as dead. Availability here is
// live (the registry folds tracker state into it).
func usableModels(models []Entry) []Entry {
	var ok []Entry
	for _, e := range models {
		switch e.Model.Availability {
		case core.AvailAvailable, core.AvailUnknown, "":
			ok = append(ok, e)
		}
	}
	if len(ok) > 0 {
		return ok
	}
	return models
}

func sortByScore(s []Scored) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Score > s[j-1].Score; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
