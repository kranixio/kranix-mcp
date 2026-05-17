package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// Task represents a scheduled task
type Task struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	CronExpr    string                 `json:"cron_expr"`
	Enabled     bool                   `json:"enabled"`
	ToolName    string                 `json:"tool_name"`
	Inputs      map[string]interface{} `json:"inputs"`
	LastRun     time.Time              `json:"last_run,omitempty"`
	NextRun     time.Time              `json:"next_run,omitempty"`
	RunCount    int                    `json:"run_count"`
	CreatedAt   time.Time              `json:"created_at"`
	CreatedBy   string                 `json:"created_by"` // Agent ID that created this task
}

// TaskExecutionResult represents the result of a task execution
type TaskExecutionResult struct {
	TaskID      string    `json:"task_id"`
	ExecutedAt  time.Time `json:"executed_at"`
	Success     bool      `json:"success"`
	Output      string    `json:"output"`
	Error       string    `json:"error,omitempty"`
	DurationMs  int64     `json:"duration_ms"`
}

// Scheduler manages scheduled tasks
type Scheduler struct {
	mu            sync.RWMutex
	tasks         map[string]*Task
	results       []TaskExecutionResult
	toolExecutor  ToolExecutor
	checkInterval time.Duration
	maxResults    int
	ctx           context.Context
	cancel        context.CancelFunc
}

// ToolExecutor is the interface for executing tools
type ToolExecutor interface {
	ExecuteTool(ctx context.Context, toolName string, inputs map[string]interface{}) (string, error)
}

// Config for the scheduler
type Config struct {
	CheckInterval time.Duration `json:"check_interval"`
	MaxResults    int           `json:"max_results"`
}

// New creates a new scheduler
func New(cfg *Config, executor ToolExecutor) *Scheduler {
	if cfg == nil {
		cfg = &Config{
			CheckInterval: 1 * time.Minute,
			MaxResults:    1000,
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Scheduler{
		tasks:         make(map[string]*Task),
		results:       make([]TaskExecutionResult, 0, cfg.MaxResults),
		toolExecutor:  executor,
		checkInterval: cfg.CheckInterval,
		maxResults:    cfg.MaxResults,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start begins the scheduler's background loop
func (s *Scheduler) Start() {
	ticker := time.NewTicker(s.checkInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				s.checkAndRunTasks()
			case <-s.ctx.Done():
				ticker.Stop()
				return
			}
		}
	}()
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	s.cancel()
}

// checkAndRunTasks checks if any tasks need to run and executes them
func (s *Scheduler) checkAndRunTasks() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, task := range s.tasks {
		if !task.Enabled {
			continue
		}

		if now.After(task.NextRun) || now.Equal(task.NextRun) {
			// Execute the task
			go s.executeTask(task)
		}
	}
}

// executeTask runs a single task
func (s *Scheduler) executeTask(task *Task) {
	startTime := time.Now()

	// Update last run time
	s.mu.Lock()
	task.LastRun = startTime
	task.RunCount++
	// Calculate next run time (simple implementation: add 1 hour for now)
	// In production, use a proper cron parser
	task.NextRun = startTime.Add(1 * time.Hour)
	s.mu.Unlock()

	// Execute the tool
	output, err := s.toolExecutor.ExecuteTool(context.Background(), task.ToolName, task.Inputs)
	duration := time.Since(startTime)

	result := TaskExecutionResult{
		TaskID:     task.ID,
		ExecutedAt: startTime,
		Success:    err == nil,
		Output:     output,
		DurationMs: duration.Milliseconds(),
	}

	if err != nil {
		result.Error = err.Error()
	}

	// Store result
	s.mu.Lock()
	s.results = append(s.results, result)
	if len(s.results) > s.maxResults {
		s.results = s.results[len(s.results)-s.maxResults:]
	}
	s.mu.Unlock()

	log.Printf("[SCHEDULER] Task %s (%s) executed: success=%v, duration=%v", task.ID, task.Name, result.Success, duration)
}

// AddTask adds a new scheduled task
func (s *Scheduler) AddTask(task *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[task.ID]; exists {
		return fmt.Errorf("task with ID %s already exists", task.ID)
	}

	task.CreatedAt = time.Now()
	// Set next run time (simple implementation: add 1 hour for now)
	// In production, use a proper cron parser
	task.NextRun = time.Now().Add(1 * time.Hour)

	s.tasks[task.ID] = task
	return nil
}

// UpdateTask updates an existing task
func (s *Scheduler) UpdateTask(task *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.tasks[task.ID]
	if !exists {
		return fmt.Errorf("task with ID %s not found", task.ID)
	}

	// Preserve certain fields
	task.CreatedAt = existing.CreatedAt
	task.CreatedBy = existing.CreatedBy
	task.RunCount = existing.RunCount

	// Recalculate next run if cron expression changed
	if task.CronExpr != existing.CronExpr {
		task.NextRun = time.Now().Add(1 * time.Hour) // Simple implementation
	}

	s.tasks[task.ID] = task
	return nil
}

// DeleteTask deletes a task
func (s *Scheduler) DeleteTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[taskID]; !exists {
		return fmt.Errorf("task with ID %s not found", taskID)
	}

	delete(s.tasks, taskID)
	return nil
}

// GetTask retrieves a task by ID
func (s *Scheduler) GetTask(taskID string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return nil, fmt.Errorf("task with ID %s not found", taskID)
	}

	// Return a copy
	copy := *task
	return &copy, nil
}

// ListTasks returns all tasks
func (s *Scheduler) ListTasks() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		copy := *task
		tasks = append(tasks, &copy)
	}
	return tasks
}

// EnableTask enables a task
func (s *Scheduler) EnableTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return fmt.Errorf("task with ID %s not found", taskID)
	}

	task.Enabled = true
	return nil
}

// DisableTask disables a task
func (s *Scheduler) DisableTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return fmt.Errorf("task with ID %s not found", taskID)
	}

	task.Enabled = false
	return nil
}

// GetTaskResults returns execution results for a task
func (s *Scheduler) GetTaskResults(taskID string, limit int) []TaskExecutionResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []TaskExecutionResult
	for i := len(s.results) - 1; i >= 0; i-- {
		if s.results[i].TaskID == taskID {
			results = append(results, s.results[i])
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}
	return results
}

// GetAllResults returns all execution results
func (s *Scheduler) GetAllResults(limit int) []TaskExecutionResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > len(s.results) {
		limit = len(s.results)
	}

	results := make([]TaskExecutionResult, limit)
	copy(results, s.results[len(s.results)-limit:])
	return results
}

// ClearResults clears execution results
func (s *Scheduler) ClearResults() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results = make([]TaskExecutionResult, 0, s.maxResults)
}

// GetStats returns scheduler statistics
func (s *Scheduler) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	enabledCount := 0
	totalRuns := 0
	for _, task := range s.tasks {
		if task.Enabled {
			enabledCount++
		}
		totalRuns += task.RunCount
	}

	return map[string]interface{}{
		"total_tasks":      len(s.tasks),
		"enabled_tasks":    enabledCount,
		"disabled_tasks":   len(s.tasks) - enabledCount,
		"total_executions": totalRuns,
		"results_stored":   len(s.results),
	}
}

// ExportTasks exports all tasks as JSON
func (s *Scheduler) ExportTasks() (string, error) {
	tasks := s.ListTasks()
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ImportTasks imports tasks from JSON
func (s *Scheduler) ImportTasks(jsonData string) error {
	var tasks []*Task
	if err := json.Unmarshal([]byte(jsonData), &tasks); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, task := range tasks {
		s.tasks[task.ID] = task
	}
	return nil
}
