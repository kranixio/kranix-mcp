package audit

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

type AuditLogger struct {
	enabled bool
	sink    string
	file    *os.File
}

type AuditEvent struct {
	Timestamp   time.Time         `json:"timestamp"`
	AgentID     string            `json:"agent_id"`
	ToolName    string            `json:"tool_name"`
	Inputs      map[string]interface{} `json:"inputs"`
	Outcome     string            `json:"outcome"` // success | error
	Error       string            `json:"error,omitempty"`
	DurationMs  int64             `json:"duration_ms"`
}

func New(enabled bool, sink string) *AuditLogger {
	al := &AuditLogger{
		enabled: enabled,
		sink:    sink,
	}

	if enabled && sink == "file" {
		// Open audit log file
		f, err := os.OpenFile("audit.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("Failed to open audit log file: %v", err)
			al.enabled = false
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
	default:
		fmt.Printf("[AUDIT] %s\n", string(data))
	}
}

func (a *AuditLogger) Close() error {
	if a.file != nil {
		return a.file.Close()
	}
	return nil
}
