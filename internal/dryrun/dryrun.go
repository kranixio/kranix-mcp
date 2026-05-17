package dryrun

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// DryRunMode controls the dry-run behavior
type DryRunMode string

const (
	ModeDisabled DryRunMode = "disabled" // Execute normally
	ModePreview  DryRunMode = "preview"  // Show what would happen without executing
	ModeLog      DryRunMode = "log"      // Execute but log all actions
)

// Action represents a planned action
type Action struct {
	ID        string                 `json:"id"`
	Tool      string                 `json:"tool"`
	Inputs    map[string]interface{} `json:"inputs"`
	Predicted map[string]interface{} `json:"predicted"` // Predicted output
	Timestamp int64                  `json:"timestamp"`
	AgentID   string                 `json:"agent_id"`
}

// DryRunner manages dry-run mode
type DryRunner struct {
	mode        DryRunMode
	actions     []Action
	mu          sync.RWMutex
	predictions map[string]func(map[string]interface{}) (map[string]interface{}, error)
}

// Config for dry-run
type Config struct {
	Mode       DryRunMode `yaml:"mode"`
	MaxActions int        `yaml:"max_actions"`
	Enabled    bool       `yaml:"enabled"`
}

func New(config *Config) *DryRunner {
	if config == nil {
		config = &Config{
			Mode:       ModeDisabled,
			MaxActions: 100,
			Enabled:    false,
		}
	}

	if !config.Enabled {
		config.Mode = ModeDisabled
	}

	return &DryRunner{
		mode:        config.Mode,
		actions:     make([]Action, 0, config.MaxActions),
		predictions: make(map[string]func(map[string]interface{}) (map[string]interface{}, error)),
	}
}

// SetMode changes the dry-run mode
func (d *DryRunner) SetMode(mode DryRunMode) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mode = mode
}

// GetMode returns the current dry-run mode
func (d *DryRunner) GetMode() DryRunMode {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.mode
}

// ShouldExecute returns whether the action should be executed
func (d *DryRunner) ShouldExecute() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.mode == ModeDisabled || d.mode == ModeLog
}

// RecordAction records an action for dry-run preview
func (d *DryRunner) RecordAction(tool string, inputs map[string]interface{}, agentID string) (*Action, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	action := Action{
		ID:        fmt.Sprintf("action-%d", len(d.actions)),
		Tool:      tool,
		Inputs:    inputs,
		Timestamp: 0, // Set below
		AgentID:   agentID,
	}

	// Try to predict the output
	if predictor, ok := d.predictions[tool]; ok {
		predicted, err := predictor(inputs)
		if err == nil {
			action.Predicted = predicted
		}
	}

	d.actions = append(d.actions, action)

	// Trim if exceeding max actions
	if len(d.actions) > 100 {
		d.actions = d.actions[1:]
	}

	return &action, nil
}

// GetActions returns all recorded actions
func (d *DryRunner) GetActions() []Action {
	d.mu.RLock()
	defer d.mu.RUnlock()
	actions := make([]Action, len(d.actions))
	copy(actions, d.actions)
	return actions
}

// ClearActions clears all recorded actions
func (d *DryRunner) ClearActions() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.actions = make([]Action, 0)
}

// GetPreview returns a human-readable preview of what would happen
func (d *DryRunner) GetPreview() (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.actions) == 0 {
		return "No actions recorded in dry-run mode.", nil
	}

	var sb strings.Builder
	sb.WriteString("Dry-Run Preview - Actions that would be executed:\n")
	sb.WriteString(strings.Repeat("=", 60) + "\n\n")

	for i, action := range d.actions {
		sb.WriteString(fmt.Sprintf("%d. Tool: %s\n", i+1, action.Tool))
		sb.WriteString(fmt.Sprintf("   Agent: %s\n", action.AgentID))

		inputsJSON, _ := json.MarshalIndent(action.Inputs, "   ", "  ")
		sb.WriteString(fmt.Sprintf("   Inputs:\n%s\n", string(inputsJSON)))

		if len(action.Predicted) > 0 {
			predJSON, _ := json.MarshalIndent(action.Predicted, "   ", "  ")
			sb.WriteString(fmt.Sprintf("   Predicted Output:\n%s\n", string(predJSON)))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(strings.Repeat("=", 60) + "\n")
	sb.WriteString(fmt.Sprintf("Total: %d action(s)\n", len(d.actions)))
	sb.WriteString("Note: No actions were executed. Set dry-run mode to 'disabled' to execute.\n")

	return sb.String(), nil
}

// RegisterPredictor registers a prediction function for a tool
func (d *DryRunner) RegisterPredictor(tool string, predictor func(map[string]interface{}) (map[string]interface{}, error)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.predictions[tool] = predictor
}

// RegisterDefaultPredictors registers default prediction functions
func (d *DryRunner) RegisterDefaultPredictors() {
	d.RegisterPredictor("deploy_workload", d.predictDeploy)
	d.RegisterPredictor("restart_workload", d.predictRestart)
	d.RegisterPredictor("delete_workload", d.predictDelete)
	d.RegisterPredictor("create_namespace", d.predictCreateNamespace)
}

func (d *DryRunner) predictDeploy(inputs map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{
		"status":    "would_deploy",
		"workload":  inputs["name"],
		"namespace": inputs["namespace"],
		"image":     inputs["image"],
		"replicas":  inputs["replicas"],
		"message":   "Workload would be deployed to the cluster",
	}, nil
}

func (d *DryRunner) predictRestart(inputs map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{
		"status":    "would_restart",
		"workload":  inputs["name"],
		"namespace": inputs["namespace"],
		"message":   "Workload would be restarted",
	}, nil
}

func (d *DryRunner) predictDelete(inputs map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{
		"status":    "would_delete",
		"workload":  inputs["name"],
		"namespace": inputs["namespace"],
		"message":   "Workload would be deleted from the cluster",
	}, nil
}

func (d *DryRunner) predictCreateNamespace(inputs map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{
		"status":  "would_create",
		"name":    inputs["name"],
		"message": "Namespace would be created",
	}, nil
}

// ToJSON converts actions to JSON
func (d *DryRunner) ToJSON() (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	data, err := json.MarshalIndent(d.actions, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
