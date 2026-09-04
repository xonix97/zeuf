package agent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SchedulerConfig controls concurrency and task timeouts.
type SchedulerConfig struct {
	MaxConcurrency int
	DefaultTimeout time.Duration
}

// DefaultSchedulerConfig returns standard scheduler parameters.
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		MaxConcurrency: 3,
		DefaultTimeout: 2 * time.Minute,
	}
}

// TaskExecutor executes a single task.
type TaskExecutor func(ctx context.Context, task *Task) (result string, err error)

// Scheduler executes ready tasks according to graph dependencies, concurrency bounds,
// and file-conflict serialization.
type Scheduler struct {
	Config             SchedulerConfig
	Graph              *TaskGraph
	Executor           TaskExecutor
	OnTaskStatusChange func(task *Task)

	mu     sync.Mutex
	active map[string]*Task
}

// NewScheduler builds a scheduler over the given graph and executor.
func NewScheduler(cfg SchedulerConfig, graph *TaskGraph, exec TaskExecutor) *Scheduler {
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 3
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 2 * time.Minute
	}
	return &Scheduler{
		Config:   cfg,
		Graph:    graph,
		Executor: exec,
		active:   make(map[string]*Task),
	}
}

type taskCompletion struct {
	task   *Task
	result string
	err    error
}

// Run executes the task graph to completion or until failure / context cancellation.
func (s *Scheduler) Run(ctx context.Context) error {
	if s.Graph == nil {
		return fmt.Errorf("task graph is nil")
	}
	if err := s.Graph.Validate(); err != nil {
		return fmt.Errorf("invalid task graph: %w", err)
	}

	doneChan := make(chan taskCompletion, s.Config.MaxConcurrency*2)

	for {
		if err := ctx.Err(); err != nil {
			s.cancelRemaining(err.Error())
			return err
		}

		s.mu.Lock()
		if s.Graph.IsComplete() {
			s.mu.Unlock()
			return nil
		}

		// Collect ready tasks from the graph
		readyTasks := s.Graph.ReadyTasks()

		// Filter tasks by active file conflicts and concurrency limit
		var toLaunch []*Task
		activeList := s.activeListLocked()

		for _, candidate := range readyTasks {
			if len(activeList)+len(toLaunch) >= s.Config.MaxConcurrency {
				break
			}
			// Check for file conflict against already running active tasks
			// and against tasks queued to launch in this batch
			allActive := append(activeList, toLaunch...)
			if s.Graph.DetectConflict(candidate, allActive) {
				// Conflicting: serialize it (will be picked up once conflicting task completes)
				continue
			}
			toLaunch = append(toLaunch, candidate)
		}

		// Launch ready, non-conflicting tasks
		for _, t := range toLaunch {
			t.Status = TaskRunning
			s.active[t.ID] = t
			if s.OnTaskStatusChange != nil {
				s.OnTaskStatusChange(t)
			}

			taskCopy := t
			go func() {
				timeout := s.Config.DefaultTimeout
				taskCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()

				start := time.Now()
				res, err := s.Executor(taskCtx, taskCopy)
				_ = start

				select {
				case doneChan <- taskCompletion{task: taskCopy, result: res, err: err}:
				case <-ctx.Done():
				}
			}()
		}

		activeCount := len(s.active)
		s.mu.Unlock()

		// Deadlock detection: no active tasks running, none launched, but graph is not complete
		if activeCount == 0 && len(toLaunch) == 0 {
			s.mu.Lock()
			if s.Graph.IsComplete() {
				s.mu.Unlock()
				return nil
			}
			// All remaining tasks are blocked by prerequisite failures or cyclic issues
			blocked := s.Graph.BlockedTasks()
			for _, b := range blocked {
				s.Graph.UpdateStatus(b.ID, TaskBlocked, "", "dependencies cannot be satisfied")
				if s.OnTaskStatusChange != nil {
					s.OnTaskStatusChange(b)
				}
			}
			s.mu.Unlock()
			return fmt.Errorf("scheduler stalled: remaining tasks are blocked or prerequisites failed")
		}

		// Wait for at least one active task to complete
		select {
		case <-ctx.Done():
			s.cancelRemaining(ctx.Err().Error())
			return ctx.Err()
		case tc := <-doneChan:
			s.handleTaskDone(tc)
		}
	}
}

func (s *Scheduler) handleTaskDone(tc taskCompletion) {
	s.mu.Lock()
	delete(s.active, tc.task.ID)

	tc.task.AttemptCount++
	if tc.err == nil {
		s.Graph.UpdateStatus(tc.task.ID, TaskCompleted, tc.result, "")
	} else {
		s.Graph.UpdateStatus(tc.task.ID, TaskFailed, tc.result, tc.err.Error())
	}

	if s.OnTaskStatusChange != nil {
		s.OnTaskStatusChange(tc.task)
	}
	s.mu.Unlock()
}

func (s *Scheduler) cancelRemaining(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.Graph.TasksList() {
		if t.Status == TaskPending || t.Status == TaskReady || t.Status == TaskRunning {
			t.Status = TaskCancelled
			t.Error = reason
			if s.OnTaskStatusChange != nil {
				s.OnTaskStatusChange(t)
			}
		}
	}
}

func (s *Scheduler) activeListLocked() []*Task {
	res := make([]*Task, 0, len(s.active))
	for _, t := range s.active {
		res = append(res, t)
	}
	return res
}
