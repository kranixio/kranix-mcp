package incident

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Step represents a single step in a runbook
type Step struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Tool        string                 `json:"tool"`        // MCP tool to execute
	Inputs      map[string]interface{} `json:"inputs"`      // Inputs for the tool
	Condition   string                 `json:"condition"`   // Optional condition to execute
	OnFailure   string                 `json:"on_failure"`  // continue | stop | rollback
	Timeout     time.Duration          `json:"timeout"`
}

// Runbook represents an incident response runbook
type Runbook struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`    // e.g., "database", "network", "application"
	Severity    string    `json:"severity"`    // low, medium, high, critical
	Steps       []Step    `json:"steps"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Execution represents a runbook execution
type Execution struct {
	ID          string                 `json:"id"`
	RunbookID   string                 `json:"runbook_id"`
	RunbookName string                 `json:"runbook_name"`
	Status      string                 `json:"status"`       // running, completed, failed, cancelled
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	TriggeredBy string                 `json:"triggered_by"` // Agent ID
	Results     []StepResult           `json:"results"`
	Context     map[string]interface{} `json:"context"`      // Additional context
	Error       string                 `json:"error,omitempty"`
}

// StepResult represents the result of a single step execution
type StepResult struct {
	StepID      string                 `json:"step_id"`
	StepName    string                 `json:"step_name"`
	Status      string                 `json:"status"`      // success, failed, skipped
	Output      string                 `json:"output"`
	Error       string                 `json:"error,omitempty"`
	ExecutedAt  time.Time              `json:"executed_at"`
	Duration    time.Duration          `json:"duration"`
}

// ToolExecutor is the interface for executing MCP tools
type ToolExecutor interface {
	ExecuteTool(ctx context.Context, toolName string, inputs map[string]interface{}) (string, error)
}

// IncidentManager manages runbooks and executions
type IncidentManager struct {
	runbooks    map[string]*Runbook
	executions  map[string]*Execution
	mu          sync.RWMutex
	runbookPath string
	executor    ToolExecutor
}

// Config for incident manager
type Config struct {
	RunbookPath string        `yaml:"runbook_path"`
	AutoLoad    bool          `yaml:"auto_load"`
	Timeout     time.Duration `yaml:"default_timeout"`
}

func New(config *Config, executor ToolExecutor) *IncidentManager {
	if config == nil {
		config = &Config{
			RunbookPath: "./runbooks",
			AutoLoad:    true,
			Timeout:     5 * time.Minute,
		}
	}

	im := &IncidentManager{
		runbooks:   make(map[string]*Runbook),
		executions: make(map[string]*Execution),
		executor:   executor,
		runbookPath: config.RunbookPath,
	}

	if config.AutoLoad {
		im.LoadRunbooks()
	}

	return im
}

// LoadRunbooks loads all runbooks from the configured directory
func (im *IncidentManager) LoadRunbooks() error {
	im.mu.Lock()
	defer im.mu.Unlock()

	if im.runbookPath == "" {
		return fmt.Errorf("runbook path not configured")
	}

	files, err := filepath.Glob(filepath.Join(im.runbookPath, "*.json"))
	if err != nil {
		return fmt.Errorf("failed to glob runbook files: %w", err)
	}

	for _, file := range files {
		runbook, err := im.loadRunbookFile(file)
		if err != nil {
			fmt.Printf("Warning: failed to load runbook %s: %v\n", file, err)
			continue
		}
		im.runbooks[runbook.ID] = runbook
	}

	return nil
}

func (im *IncidentManager) loadRunbookFile(path string) (*Runbook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var runbook Runbook
	if err := json.Unmarshal(data, &runbook); err != nil {
		return nil, err
	}

	if runbook.ID == "" {
		runbook.ID = strings.TrimSuffix(filepath.Base(path), ".json")
	}

	return &runbook, nil
}

// ListRunbooks returns all available runbooks
func (im *IncidentManager) ListRunbooks(category string) ([]*Runbook, error) {
	im.mu.RLock()
	defer im.mu.RUnlock()

	var result []*Runbook
	for _, runbook := range im.runbooks {
		if category != "" && runbook.Category != category {
			continue
		}
		result = append(result, runbook)
	}
	return result, nil
}

// GetRunbook retrieves a specific runbook
func (im *IncidentManager) GetRunbook(runbookID string) (*Runbook, error) {
	im.mu.RLock()
	defer im.mu.RUnlock()

	runbook, ok := im.runbooks[runbookID]
	if !ok {
		return nil, fmt.Errorf("runbook not found: %s", runbookID)
	}
	return runbook, nil
}

// ExecuteRunbook executes a runbook with the given context
func (im *IncidentManager) ExecuteRunbook(ctx context.Context, runbookID string, triggerAgent string, context map[string]interface{}) (*Execution, error) {
	im.mu.Lock()
	defer im.mu.Unlock()

	runbook, ok := im.runbooks[runbookID]
	if !ok {
		return nil, fmt.Errorf("runbook not found: %s", runbookID)
	}

	execution := &Execution{
		ID:          generateExecutionID(),
		RunbookID:   runbookID,
		RunbookName: runbook.Name,
		Status:      "running",
		StartedAt:   time.Now(),
		TriggeredBy: triggerAgent,
		Results:     make([]StepResult, 0),
		Context:     context,
	}

	im.executions[execution.ID] = execution

	// Execute in background
	go im.executeRunbookSteps(ctx, execution, runbook)

	return execution, nil
}

func (im *IncidentManager) executeRunbookSteps(ctx context.Context, execution *Execution, runbook *Runbook) {
	for _, step := range runbook.Steps {
		// Check if execution was cancelled
		im.mu.RLock()
		if execution.Status == "cancelled" {
			im.mu.RUnlock()
			break
		}
		im.mu.RUnlock()

		// Execute step
		result := im.executeStep(ctx, step, execution.Context)

		im.mu.Lock()
		execution.Results = append(execution.Results, result)
		im.mu.Unlock()

		// Handle failure
		if result.Status == "failed" {
			switch step.OnFailure {
			case "stop":
				im.mu.Lock()
				execution.Status = "failed"
				execution.Error = fmt.Sprintf("Step %s failed: %s", step.Name, result.Error)
				now := time.Now()
				execution.CompletedAt = &now
				im.mu.Unlock()
				return
			case "rollback":
				// TODO: Implement rollback logic
				im.mu.Lock()
				execution.Status = "failed"
				execution.Error = fmt.Sprintf("Step %s failed, rollback not yet implemented: %s", step.Name, result.Error)
				now := time.Now()
				execution.CompletedAt = &now
				im.mu.Unlock()
				return
			case "continue":
				// Continue to next step
				continue
			default:
				im.mu.Lock()
				execution.Status = "failed"
				execution.Error = fmt.Sprintf("Step %s failed: %s", step.Name, result.Error)
				now := time.Now()
				execution.CompletedAt = &now
				im.mu.Unlock()
				return
			}
		}
	}

	// Mark as completed
	im.mu.Lock()
	execution.Status = "completed"
	now := time.Now()
	execution.CompletedAt = &now
	im.mu.Unlock()
}

func (im *IncidentManager) executeStep(ctx context.Context, step Step, context map[string]interface{}) StepResult {
	start := time.Now()

	// Merge context into inputs
	inputs := make(map[string]interface{})
	for k, v := range step.Inputs {
		inputs[k] = v
	}
	for k, v := range context {
		// Only add if not already present in step inputs
		if _, exists := inputs[k]; !exists {
			inputs[k] = v
		}
	}

	// Execute the tool
	output, err := im.executor.ExecuteTool(ctx, step.Tool, inputs)
	duration := time.Since(start)

	result := StepResult{
		StepID:     step.ID,
		StepName:   step.Name,
		Output:     output,
		ExecutedAt: start,
		Duration:   duration,
	}

	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
	} else {
		result.Status = "success"
	}

	return result
}

// GetExecution retrieves an execution by ID
func (im *IncidentManager) GetExecution(executionID string) (*Execution, error) {
	im.mu.RLock()
	defer im.mu.RUnlock()

	execution, ok := im.executions[executionID]
	if !ok {
		return nil, fmt.Errorf("execution not found: %s", executionID)
	}
	return execution, nil
}

// ListExecutions returns all executions, optionally filtered by runbook or status
func (im *IncidentManager) ListExecutions(runbookID, status string) ([]*Execution, error) {
	im.mu.RLock()
	defer im.mu.RUnlock()

	var result []*Execution
	for _, execution := range im.executions {
		if runbookID != "" && execution.RunbookID != runbookID {
			continue
		}
		if status != "" && execution.Status != status {
			continue
		}
		result = append(result, execution)
	}
	return result, nil
}

// CancelExecution cancels a running execution
func (im *IncidentManager) CancelExecution(executionID string) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	execution, ok := im.executions[executionID]
	if !ok {
		return fmt.Errorf("execution not found: %s", executionID)
	}

	if execution.Status != "running" {
		return fmt.Errorf("execution is not running: %s", execution.Status)
	}

	execution.Status = "cancelled"
	now := time.Now()
	execution.CompletedAt = &now

	return nil
}

// CreateRunbook creates a new runbook
func (im *IncidentManager) CreateRunbook(runbook *Runbook) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	if runbook.ID == "" {
		runbook.ID = generateRunbookID()
	}
	runbook.CreatedAt = time.Now()
	runbook.UpdatedAt = time.Now()

	im.runbooks[runbook.ID] = runbook

	// Save to file if path is configured
	if im.runbookPath != "" {
		path := filepath.Join(im.runbookPath, runbook.ID+".json")
		data, err := json.MarshalIndent(runbook, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal runbook: %w", err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return fmt.Errorf("failed to save runbook: %w", err)
		}
	}

	return nil
}

func generateExecutionID() string {
	return fmt.Sprintf("exec-%d", time.Now().UnixNano())
}

func generateRunbookID() string {
	return fmt.Sprintf("rb-%d", time.Now().UnixNano())
}

// ToJSON converts runbook to JSON
func (r *Runbook) ToJSON() (string, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ToJSON converts execution to JSON
func (e *Execution) ToJSON() (string, error) {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
