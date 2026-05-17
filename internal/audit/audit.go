package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

type AuditLogger struct {
	enabled     bool
	sink        string
	file        *os.File
	inMemory    bool
	events      []AuditEvent
	mu          sync.RWMutex
	maxInMemory int
}

type AuditEvent struct {
	Timestamp  time.Time              `json:"timestamp"`
	AgentID    string                 `json:"agent_id"`
	ToolName   string                 `json:"tool_name"`
	Inputs     map[string]interface{} `json:"inputs"`
	Outcome    string                 `json:"outcome"` // success | error
	Error      string                 `json:"error,omitempty"`
	DurationMs int64                  `json:"duration_ms"`
}

type AuditQuery struct {
	AgentID   string    `json:"agent_id,omitempty"`
	ToolName  string    `json:"tool_name,omitempty"`
	Outcome   string    `json:"outcome,omitempty"`
	StartTime time.Time `json:"start_time,omitempty"`
	EndTime   time.Time `json:"end_time,omitempty"`
	Limit     int       `json:"limit,omitempty"`
}

type Config struct {
	Enabled     bool   `json:"enabled"`
	Sink        string `json:"sink"` // stdout | file | memory | all
	MaxInMemory int    `json:"max_in_memory"`
}

func New(enabled bool, sink string) *AuditLogger {
	return NewWithConfig(&Config{
		Enabled:     enabled,
		Sink:        sink,
		MaxInMemory: 1000,
	})
}

func NewWithConfig(cfg *Config) *AuditLogger {
	if cfg == nil {
		cfg = &Config{
			Enabled:     true,
			Sink:        "stdout",
			MaxInMemory: 1000,
		}
	}

	al := &AuditLogger{
		enabled:     cfg.Enabled,
		sink:        cfg.Sink,
		inMemory:    cfg.Sink == "memory" || cfg.Sink == "all",
		events:      make([]AuditEvent, 0, cfg.MaxInMemory),
		maxInMemory: cfg.MaxInMemory,
	}

	if cfg.Enabled && (cfg.Sink == "file" || cfg.Sink == "all") {
		// Open audit log file
		f, err := os.OpenFile("audit.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("Failed to open audit log file: %v", err)
			if cfg.Sink == "file" {
				al.enabled = false
			}
		} else {
			al.file = f
		}
	}

	return al
}

func (a *AuditLogger) Log(agentID, toolName string, inputs map[string]interface{}, outcome, errorMsg string, durationMs int64) {
	if !a.enabled {
		return
	}

	event := AuditEvent{
		Timestamp:  time.Now().UTC(),
		AgentID:    agentID,
		ToolName:   toolName,
		Inputs:     inputs,
		Outcome:    outcome,
		Error:      errorMsg,
		DurationMs: durationMs,
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("Failed to marshal audit event: %v", err)
		return
	}

	switch a.sink {
	case "stdout":
		fmt.Printf("[AUDIT] %s\n", string(data))
	case "file":
		if a.file != nil {
			a.file.WriteString(string(data) + "\n")
		}
	case "memory":
		a.addToMemory(event)
	case "all":
		fmt.Printf("[AUDIT] %s\n", string(data))
		if a.file != nil {
			a.file.WriteString(string(data) + "\n")
		}
		a.addToMemory(event)
	default:
		fmt.Printf("[AUDIT] %s\n", string(data))
	}
}

func (a *AuditLogger) addToMemory(event AuditEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.events = append(a.events, event)
	if len(a.events) > a.maxInMemory {
		a.events = a.events[len(a.events)-a.maxInMemory:]
	}
}

// QueryByAgent retrieves all audit events for a specific agent
func (a *AuditLogger) QueryByAgent(agentID string) []AuditEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var result []AuditEvent
	for _, event := range a.events {
		if event.AgentID == agentID {
			result = append(result, event)
		}
	}
	return result
}

// Query executes a complex query against audit logs
func (a *AuditLogger) Query(query *AuditQuery) []AuditEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var result []AuditEvent
	for _, event := range a.events {
		if query.AgentID != "" && event.AgentID != query.AgentID {
			continue
		}
		if query.ToolName != "" && event.ToolName != query.ToolName {
			continue
		}
		if query.Outcome != "" && event.Outcome != query.Outcome {
			continue
		}
		if !query.StartTime.IsZero() && event.Timestamp.Before(query.StartTime) {
			continue
		}
		if !query.EndTime.IsZero() && event.Timestamp.After(query.EndTime) {
			continue
		}
		result = append(result, event)
	}

	if query.Limit > 0 && len(result) > query.Limit {
		result = result[len(result)-query.Limit:]
	}

	return result
}

// QueryFromFile reads audit events from file (for file-based storage)
func (a *AuditLogger) QueryFromFile(query *AuditQuery) ([]AuditEvent, error) {
	if a.file == nil {
		return nil, fmt.Errorf("file sink not enabled")
	}

	// Close the file and reopen for reading
	a.file.Close()
	defer func() {
		f, err := os.OpenFile("audit.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			a.file = f
		}
	}()

	f, err := os.Open("audit.log")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var result []AuditEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event AuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		if query.AgentID != "" && event.AgentID != query.AgentID {
			continue
		}
		if query.ToolName != "" && event.ToolName != query.ToolName {
			continue
		}
		if query.Outcome != "" && event.Outcome != query.Outcome {
			continue
		}
		if !query.StartTime.IsZero() && event.Timestamp.Before(query.StartTime) {
			continue
		}
		if !query.EndTime.IsZero() && event.Timestamp.After(query.EndTime) {
			continue
		}
		result = append(result, event)
	}

	if query.Limit > 0 && len(result) > query.Limit {
		result = result[len(result)-query.Limit:]
	}

	return result, scanner.Err()
}

// GetAgentSummary returns a summary of agent activity
func (a *AuditLogger) GetAgentSummary(agentID string) map[string]interface{} {
	events := a.QueryByAgent(agentID)

	summary := map[string]interface{}{
		"agent_id":        agentID,
		"total_calls":     len(events),
		"successful":      0,
		"failed":          0,
		"tools_used":      make(map[string]int),
		"avg_duration_ms": 0,
		"first_seen":      nil,
		"last_seen":       nil,
	}

	if len(events) == 0 {
		return summary
	}

	totalDuration := int64(0)
	for _, event := range events {
		if event.Outcome == "success" {
			summary["successful"] = summary["successful"].(int) + 1
		} else {
			summary["failed"] = summary["failed"].(int) + 1
		}
		summary["tools_used"].(map[string]int)[event.ToolName]++
		totalDuration += event.DurationMs

		if summary["first_seen"] == nil || event.Timestamp.Before(summary["first_seen"].(time.Time)) {
			summary["first_seen"] = event.Timestamp
		}
		if summary["last_seen"] == nil || event.Timestamp.After(summary["last_seen"].(time.Time)) {
			summary["last_seen"] = event.Timestamp
		}
	}

	summary["avg_duration_ms"] = totalDuration / int64(len(events))
	return summary
}

// GetAllAgents returns a list of all unique agent IDs
func (a *AuditLogger) GetAllAgents() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	agentSet := make(map[string]struct{})
	for _, event := range a.events {
		agentSet[event.AgentID] = struct{}{}
	}

	agents := make([]string, 0, len(agentSet))
	for agent := range agentSet {
		agents = append(agents, agent)
	}
	return agents
}

// ExportAuditLog exports audit log as JSON
func (a *AuditLogger) ExportAuditLog(agentID string) (string, error) {
	var events []AuditEvent
	if agentID == "" {
		a.mu.RLock()
		events = make([]AuditEvent, len(a.events))
		copy(events, a.events)
		a.mu.RUnlock()
	} else {
		events = a.QueryByAgent(agentID)
	}

	data, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Clear clears in-memory audit events
func (a *AuditLogger) Clear() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = make([]AuditEvent, 0, a.maxInMemory)
}

func (a *AuditLogger) Close() error {
	if a.file != nil {
		return a.file.Close()
	}
	return nil
}
