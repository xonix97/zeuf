package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"zeuf/internal/core"
)

// ExecutionTrace manages a thread-safe log of orchestration events.
type ExecutionTrace struct {
	mu     sync.RWMutex
	events []core.TraceEvent
}

// NewExecutionTrace creates an empty trace collector.
func NewExecutionTrace() *ExecutionTrace {
	return &ExecutionTrace{
		events: make([]core.TraceEvent, 0),
	}
}

// Record appends a trace event with automatic redaction of secrets.
func (et *ExecutionTrace) Record(kind string, state State, taskID, model, tool string, d time.Duration, details string) {
	et.mu.Lock()
	defer et.mu.Unlock()

	ev := core.TraceEvent{
		Timestamp: time.Now(),
		Kind:      kind,
		State:     string(state),
		TaskID:    taskID,
		Model:     model,
		Tool:      tool,
		Duration:  d,
		Details:   core.Redact(details),
	}
	et.events = append(et.events, ev)
}

// Events returns a copy of all recorded events.
func (et *ExecutionTrace) Events() []core.TraceEvent {
	et.mu.RLock()
	defer et.mu.RUnlock()
	return append([]core.TraceEvent(nil), et.events...)
}

// Format renders a clean summary table of the trace for debugging and introspection.
func (et *ExecutionTrace) Format() string {
	et.mu.RLock()
	defer et.mu.RUnlock()

	if len(et.events) == 0 {
		return "Trace: (empty)"
	}

	var b strings.Builder
	b.WriteString("EXECUTION TRACE:\n")
	for _, ev := range et.events {
		dur := ""
		if ev.Duration > 0 {
			dur = fmt.Sprintf(" (%s)", ev.Duration.Round(time.Millisecond))
		}
		target := ""
		if ev.TaskID != "" {
			target += fmt.Sprintf(" [task:%s]", ev.TaskID)
		}
		if ev.Model != "" {
			target += fmt.Sprintf(" [model:%s]", ev.Model)
		}
		if ev.Tool != "" {
			target += fmt.Sprintf(" [tool:%s]", ev.Tool)
		}
		detail := ""
		if ev.Details != "" {
			detail = " - " + ev.Details
		}
		fmt.Fprintf(&b, "%s | %-16s | %-12s%s%s%s\n",
			ev.Timestamp.Format("15:04:05.000"),
			ev.Kind,
			ev.State,
			target,
			dur,
			detail,
		)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ToJSON serializes the trace to JSON.
func (et *ExecutionTrace) ToJSON() ([]byte, error) {
	et.mu.RLock()
	defer et.mu.RUnlock()
	return json.Marshal(et.events)
}
