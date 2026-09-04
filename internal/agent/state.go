package agent

import (
	"fmt"
	"sync"
	"time"
)

// State represents an explicit phase in the orchestration lifecycle.
type State string

const (
	StateIntake       State = "INTAKE"
	StateDiscovery    State = "DISCOVERY"
	StatePlanning     State = "PLANNING"
	StateScheduling   State = "SCHEDULING"
	StateExecution    State = "EXECUTION"
	StateVerification State = "VERIFICATION"
	StateReplan       State = "REPLAN"
	StateCompletion   State = "COMPLETION"
)

// StateTransition records one state change for observability.
type StateTransition struct {
	From      State     `json:"from"`
	To        State     `json:"to"`
	Reason    string    `json:"reason"`
	Timestamp time.Time `json:"timestamp"`
}

// StateMachine manages the lifecycle transitions of the orchestrator.
type StateMachine struct {
	mu           sync.RWMutex
	current      State
	history      []StateTransition
	onTransition func(StateTransition)
}

// NewStateMachine initializes a state machine at StateIntake.
func NewStateMachine(onTransition func(StateTransition)) *StateMachine {
	return &StateMachine{
		current:      StateIntake,
		onTransition: onTransition,
	}
}

// Current returns the current state.
func (sm *StateMachine) Current() State {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.current
}

// History returns a copy of all transitions.
func (sm *StateMachine) History() []StateTransition {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return append([]StateTransition(nil), sm.history...)
}

// TransitionTo validates and performs a state transition.
func (sm *StateMachine) TransitionTo(next State, reason string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	from := sm.current
	if from == next {
		return nil
	}

	if !isValidTransition(from, next) {
		return fmt.Errorf("invalid state transition from %s to %s (reason: %s)", from, next, reason)
	}

	tr := StateTransition{
		From:      from,
		To:        next,
		Reason:    reason,
		Timestamp: time.Now(),
	}

	sm.current = next
	sm.history = append(sm.history, tr)

	if sm.onTransition != nil {
		sm.onTransition(tr)
	}
	return nil
}

func isValidTransition(from, to State) bool {
	switch from {
	case StateIntake:
		return to == StateDiscovery || to == StatePlanning || to == StateCompletion
	case StateDiscovery:
		return to == StatePlanning || to == StateCompletion
	case StatePlanning:
		return to == StateScheduling || to == StateExecution || to == StateCompletion
	case StateScheduling:
		return to == StateExecution || to == StateVerification || to == StateCompletion
	case StateExecution:
		return to == StateVerification || to == StateScheduling || to == StateReplan || to == StateCompletion
	case StateVerification:
		return to == StateCompletion || to == StateReplan || to == StateScheduling
	case StateReplan:
		return to == StateScheduling || to == StateExecution || to == StatePlanning || to == StateCompletion
	case StateCompletion:
		// Terminal, but can re-intake for follow-up turns in interactive sessions
		return to == StateIntake
	default:
		return false
	}
}
