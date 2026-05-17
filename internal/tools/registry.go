package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kranix-io/kranix-mcp/internal/audit"
	"github.com/kranix-io/kranix-mcp/internal/client"
	"github.com/kranix-io/kranix-mcp/internal/coordination"
	"github.com/kranix-io/kranix-mcp/internal/dryrun"
	"github.com/kranix-io/kranix-mcp/internal/healing"
	"github.com/kranix-io/kranix-mcp/internal/incident"
	"github.com/kranix-io/kranix-mcp/internal/memory"
	"github.com/kranix-io/kranix-mcp/internal/nlp"
	"github.com/kranix-io/kranix-mcp/internal/safety"
	"github.com/kranix-io/kranix-mcp/internal/scheduler"
)

type ToolFunc func(ctx context.Context, inputs map[string]interface{}) (string, error)

type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     ToolFunc
}

type Registry struct {
	client        *client.Client
	auditLogger   *audit.AuditLogger
	safety        *safety.SafetyPolicy
	healer        *healing.Healer
	nlpParser     *nlp.Parser
	coordinator   *coordination.Coordinator
	dryRunner     *dryrun.DryRunner
	incidentMgr   *incident.IncidentManager
	sessionMemory *memory.SessionMemory
	taskScheduler *scheduler.Scheduler
	tools         map[string]ToolDefinition
}

func New(client *client.Client, auditLogger *audit.AuditLogger, safety *safety.SafetyPolicy, healer *healing.Healer, coordinator *coordination.Coordinator, dryRunner *dryrun.DryRunner, incidentMgr *incident.IncidentManager, sessionMemory *memory.SessionMemory, taskScheduler *scheduler.Scheduler) *Registry {
	return &Registry{
		client:        client,
		auditLogger:   auditLogger,
		safety:        safety,
		healer:        healer,
		nlpParser:     nlp.NewParser(),
		coordinator:   coordinator,
		dryRunner:     dryRunner,
		incidentMgr:   incidentMgr,
		sessionMemory: sessionMemory,
		taskScheduler: taskScheduler,
		tools:         make(map[string]ToolDefinition),
	}
}

func (r *Registry) RegisterTool(tool ToolDefinition) {
	r.tools[tool.Name] = tool
}

func (r *Registry) RegisterTools() {
	r.RegisterTool(ToolDefinition{
		Name:        "deploy_workload",
		Description: "Deploy an application workload to a namespace",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"name", "image", "namespace"},
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Workload name",
				},
				"image": map[string]interface{}{
					"type":        "string",
					"description": "Container image (e.g. nginx:latest)",
				},
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Target namespace",
				},
				"replicas": map[string]interface{}{
					"type":        "integer",
					"default":     1,
					"description": "Number of replicas",
				},
				"env": map[string]interface{}{
					"type":        "object",
					"description": "Environment variables as key-value pairs",
				},
			},
		},
		Handler: r.deployWorkload,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "list_workloads",
		Description: "List all running workloads in a namespace",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Namespace to filter by (optional)",
				},
			},
		},
		Handler: r.listWorkloads,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "get_workload",
		Description: "Get the spec and status of a workload",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"name", "namespace"},
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Workload name",
				},
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Namespace",
				},
			},
		},
		Handler: r.getWorkload,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "restart_workload",
		Description: "Restart a workload",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"name", "namespace"},
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Workload name",
				},
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Namespace",
				},
			},
		},
		Handler: r.restartWorkload,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "delete_workload",
		Description: "Remove a workload (requires explicit confirmation flag)",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"name", "namespace", "confirm"},
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Workload name",
				},
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Namespace",
				},
				"confirm": map[string]interface{}{
					"type":        "boolean",
					"description": "Must be true to confirm deletion",
				},
			},
		},
		Handler: r.deleteWorkload,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "list_pods",
		Description: "List pods for a given workload",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"workload", "namespace"},
			"properties": map[string]interface{}{
				"workload": map[string]interface{}{
					"type":        "string",
					"description": "Workload name",
				},
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Namespace",
				},
			},
		},
		Handler: r.listPods,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "stream_logs",
		Description: "Stream logs from a pod (returns last N lines)",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"pod", "namespace"},
			"properties": map[string]interface{}{
				"pod": map[string]interface{}{
					"type":        "string",
					"description": "Pod name",
				},
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Namespace",
				},
				"tail_lines": map[string]interface{}{
					"type":        "integer",
					"default":     100,
					"description": "Number of lines to retrieve",
				},
			},
		},
		Handler: r.streamLogs,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "create_namespace",
		Description: "Create a new namespace",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"name"},
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Namespace name",
				},
			},
		},
		Handler: r.createNamespace,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "list_namespaces",
		Description: "List all available namespaces",
		InputSchema: map[string]interface{}{
			"type": "object",
		},
		Handler: r.listNamespaces,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "analyze_workload",
		Description: "Run AI-powered failure analysis on a workload",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"name", "namespace"},
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Workload name",
				},
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Namespace",
				},
			},
		},
		Handler: r.analyzeWorkload,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "generate_manifest",
		Description: "Generate a Kubernetes manifest from a plain-text description",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"description"},
			"properties": map[string]interface{}{
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Plain-text description of the desired deployment",
				},
			},
		},
		Handler: r.generateManifest,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "get_cluster_health",
		Description: "Summary of overall cluster health",
		InputSchema: map[string]interface{}{
			"type": "object",
		},
		Handler: r.getClusterHealth,
	})

	// Multi-agent coordination tools
	r.RegisterTool(ToolDefinition{
		Name:        "create_task",
		Description: "Create a new task for multi-agent coordination",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"title", "assigned_to"},
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Task title",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Task description",
				},
				"assigned_to": map[string]interface{}{
					"type":        "string",
					"description": "Agent ID to assign the task to",
				},
				"priority": map[string]interface{}{
					"type":        "string",
					"description": "Task priority (low, medium, high, critical)",
					"default":     "medium",
				},
				"inputs": map[string]interface{}{
					"type":        "object",
					"description": "Additional inputs for the task",
				},
			},
		},
		Handler: r.createTask,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "delegate_task",
		Description: "Delegate a task to another agent",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"task_id", "target_agent"},
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Task ID to delegate",
				},
				"target_agent": map[string]interface{}{
					"type":        "string",
					"description": "Target agent ID",
				},
			},
		},
		Handler: r.delegateTask,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "get_task",
		Description: "Get details of a specific task",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"task_id"},
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Task ID",
				},
			},
		},
		Handler: r.getTask,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "list_tasks",
		Description: "List all tasks, optionally filtered by agent or status",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by assigned agent ID",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Filter by status (pending, in_progress, completed, failed)",
				},
			},
		},
		Handler: r.listTasks,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "update_task_status",
		Description: "Update the status of a task",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"task_id", "status"},
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Task ID",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "New status (pending, in_progress, completed, failed)",
				},
				"outputs": map[string]interface{}{
					"type":        "object",
					"description": "Task outputs",
				},
			},
		},
		Handler: r.updateTaskStatus,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "create_subtask",
		Description: "Create a sub-task for a parent task",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"parent_task_id", "title", "assigned_to"},
			"properties": map[string]interface{}{
				"parent_task_id": map[string]interface{}{
					"type":        "string",
					"description": "Parent task ID",
				},
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Sub-task title",
				},
				"assigned_to": map[string]interface{}{
					"type":        "string",
					"description": "Agent ID to assign the sub-task to",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Sub-task description",
				},
			},
		},
		Handler: r.createSubtask,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "claim_task",
		Description: "Claim a pending task for the current agent",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"task_id", "agent_id"},
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Task ID to claim",
				},
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent ID claiming the task",
				},
			},
		},
		Handler: r.claimTask,
	})

	// Dry-run mode tools
	r.RegisterTool(ToolDefinition{
		Name:        "set_dryrun_mode",
		Description: "Set the dry-run mode (disabled, preview, log)",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"mode"},
			"properties": map[string]interface{}{
				"mode": map[string]interface{}{
					"type":        "string",
					"description": "Dry-run mode: disabled (execute), preview (show only), log (execute and log)",
					"enum":        []string{"disabled", "preview", "log"},
				},
			},
		},
		Handler: r.setDryRunMode,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "get_dryrun_mode",
		Description: "Get the current dry-run mode",
		InputSchema: map[string]interface{}{
			"type": "object",
		},
		Handler: r.getDryRunMode,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "get_dryrun_preview",
		Description: "Get a preview of actions that would be executed in dry-run mode",
		InputSchema: map[string]interface{}{
			"type": "object",
		},
		Handler: r.getDryRunPreview,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "clear_dryrun_actions",
		Description: "Clear all recorded dry-run actions",
		InputSchema: map[string]interface{}{
			"type": "object",
		},
		Handler: r.clearDryRunActions,
	})

	// Incident response tools
	r.RegisterTool(ToolDefinition{
		Name:        "list_runbooks",
		Description: "List all available incident response runbooks",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"category": map[string]interface{}{
					"type":        "string",
					"description": "Filter by category (e.g., database, network, application)",
				},
			},
		},
		Handler: r.listRunbooks,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "get_runbook",
		Description: "Get details of a specific runbook",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"runbook_id"},
			"properties": map[string]interface{}{
				"runbook_id": map[string]interface{}{
					"type":        "string",
					"description": "Runbook ID",
				},
			},
		},
		Handler: r.getRunbook,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "execute_runbook",
		Description: "Execute an incident response runbook",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"runbook_id", "agent_id"},
			"properties": map[string]interface{}{
				"runbook_id": map[string]interface{}{
					"type":        "string",
					"description": "Runbook ID to execute",
				},
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent ID triggering the execution",
				},
				"context": map[string]interface{}{
					"type":        "object",
					"description": "Additional context for the runbook execution",
				},
			},
		},
		Handler: r.executeRunbook,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "get_execution",
		Description: "Get details of a runbook execution",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"execution_id"},
			"properties": map[string]interface{}{
				"execution_id": map[string]interface{}{
					"type":        "string",
					"description": "Execution ID",
				},
			},
		},
		Handler: r.getExecution,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "list_executions",
		Description: "List all runbook executions, optionally filtered by runbook or status",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"runbook_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by runbook ID",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Filter by status (running, completed, failed, cancelled)",
				},
			},
		},
		Handler: r.listExecutions,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "cancel_execution",
		Description: "Cancel a running runbook execution",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"execution_id"},
			"properties": map[string]interface{}{
				"execution_id": map[string]interface{}{
					"type":        "string",
					"description": "Execution ID to cancel",
				},
			},
		},
		Handler: r.cancelExecution,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "create_runbook",
		Description: "Create a new incident response runbook",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"name", "steps"},
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Runbook name",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Runbook description",
				},
				"category": map[string]interface{}{
					"type":        "string",
					"description": "Runbook category (e.g., database, network, application)",
				},
				"severity": map[string]interface{}{
					"type":        "string",
					"description": "Runbook severity (low, medium, high, critical)",
					"default":     "medium",
				},
				"steps": map[string]interface{}{
					"type":        "array",
					"description": "Array of step objects with name, tool, inputs, etc.",
				},
			},
		},
		Handler: r.createRunbook,
	})

	// Session Memory Tools
	r.RegisterTool(ToolDefinition{
		Name:        "set_memory_context",
		Description: "Store context data in the agent's session memory",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"key", "value"},
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Context key",
				},
				"value": map[string]interface{}{
					"type":        "string",
					"description": "Context value (JSON string for complex types)",
				},
			},
		},
		Handler: r.setMemoryContext,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "get_memory_context",
		Description: "Retrieve context data from the agent's session memory",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"key"},
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Context key to retrieve",
				},
			},
		},
		Handler: r.getMemoryContext,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "get_all_memory_context",
		Description: "Retrieve all context data from the agent's session memory",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: r.getAllMemoryContext,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "get_session_history",
		Description: "Get the tool call history for the agent's session",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of history entries to return",
					"default":     10,
				},
			},
		},
		Handler: r.getSessionHistory,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "clear_session_history",
		Description: "Clear the tool call history for the agent's session",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: r.clearSessionHistory,
	})

	// Per-Agent Audit Tools
	r.RegisterTool(ToolDefinition{
		Name:        "get_agent_audit_log",
		Description: "Retrieve audit log entries for a specific agent",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"agent_id"},
			"properties": map[string]interface{}{
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent ID to query",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of entries to return",
					"default":     50,
				},
			},
		},
		Handler: r.getAgentAuditLog,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "get_agent_summary",
		Description: "Get a summary of agent activity including total calls, success rate, tools used",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"agent_id"},
			"properties": map[string]interface{}{
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent ID to summarize",
				},
			},
		},
		Handler: r.getAgentSummary,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "list_all_agents",
		Description: "List all unique agent IDs that have performed actions",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: r.listAllAgents,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "export_audit_log",
		Description: "Export audit log as JSON for a specific agent or all agents",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent ID to export (empty for all agents)",
				},
			},
		},
		Handler: r.exportAuditLog,
	})

	// Scheduler Tools
	r.RegisterTool(ToolDefinition{
		Name:        "create_scheduled_task",
		Description: "Create a new scheduled task to run on a cron schedule",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"id", "name", "cron_expr", "tool_name"},
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "Unique task ID",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Task name",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Task description",
				},
				"cron_expr": map[string]interface{}{
					"type":        "string",
					"description": "Cron expression (e.g., '0 */5 * * *' for every 5 minutes)",
				},
				"tool_name": map[string]interface{}{
					"type":        "string",
					"description": "Tool to execute",
				},
				"inputs": map[string]interface{}{
					"type":        "object",
					"description": "Tool inputs as key-value pairs",
				},
				"enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether the task is enabled",
					"default":     true,
				},
			},
		},
		Handler: r.createScheduledTask,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "list_scheduled_tasks",
		Description: "List all scheduled tasks",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: r.listScheduledTasks,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "get_scheduled_task",
		Description: "Get details of a specific scheduled task",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"task_id"},
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Task ID",
				},
			},
		},
		Handler: r.getScheduledTask,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "update_scheduled_task",
		Description: "Update an existing scheduled task",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "Task ID",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Task name",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Task description",
				},
				"cron_expr": map[string]interface{}{
					"type":        "string",
					"description": "Cron expression",
				},
				"tool_name": map[string]interface{}{
					"type":        "string",
					"description": "Tool to execute",
				},
				"inputs": map[string]interface{}{
					"type":        "object",
					"description": "Tool inputs",
				},
				"enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether the task is enabled",
				},
			},
		},
		Handler: r.updateScheduledTask,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "delete_scheduled_task",
		Description: "Delete a scheduled task",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"task_id"},
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Task ID",
				},
			},
		},
		Handler: r.deleteScheduledTask,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "enable_scheduled_task",
		Description: "Enable a scheduled task",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"task_id"},
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Task ID",
				},
			},
		},
		Handler: r.enableScheduledTask,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "disable_scheduled_task",
		Description: "Disable a scheduled task",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"task_id"},
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Task ID",
				},
			},
		},
		Handler: r.disableScheduledTask,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "get_task_results",
		Description: "Get execution results for a scheduled task",
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"task_id"},
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Task ID",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results to return",
					"default":     10,
				},
			},
		},
		Handler: r.getTaskResults,
	})

	r.RegisterTool(ToolDefinition{
		Name:        "get_scheduler_stats",
		Description: "Get scheduler statistics including total tasks, executions, etc.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: r.getSchedulerStats,
	})
}

func (r *Registry) ListTools() []map[string]interface{} {
	var tools []map[string]interface{}
	for _, tool := range r.tools {
		tools = append(tools, map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": tool.InputSchema,
		})
	}
	return tools
}

func (r *Registry) CallTool(ctx context.Context, toolName string, inputs map[string]interface{}) (string, error) {
	// Check safety permission
	permission := r.safety.CheckPermission(toolName, inputs)
	if !permission.Allowed {
		return "", fmt.Errorf("tool not permitted: %s", permission.Reason)
	}

	// Validate inputs
	if err := r.validateInputs(toolName, inputs); err != nil {
		return "", err
	}

	// Get tool handler
	tool, ok := r.tools[toolName]
	if !ok {
		return "", fmt.Errorf("tool not found: %s", toolName)
	}

	// Execute tool with audit logging
	start := time.Now()
	result, err := tool.Handler(ctx, inputs)
	duration := time.Since(start).Milliseconds()

	outcome := "success"
	errorMsg := ""
	if err != nil {
		outcome = "error"
		errorMsg = err.Error()
	}

	// Extract agent ID from context if available
	agentID := "unknown"
	if ctxAgentID, ok := ctx.Value("agent_id").(string); ok {
		agentID = ctxAgentID
	}

	r.auditLogger.Log(agentID, toolName, inputs, outcome, errorMsg, duration)

	return result, err
}

func (r *Registry) validateInputs(toolName string, inputs map[string]interface{}) error {
	switch toolName {
	case "deploy_workload", "get_workload", "restart_workload", "delete_workload":
		if namespace, ok := inputs["namespace"].(string); ok {
			if err := r.safety.ValidateNamespace(namespace); err != nil {
				return err
			}
		}
	case "create_namespace":
		if name, ok := inputs["name"].(string); ok {
			if err := r.safety.ValidateNamespace(name); err != nil {
				return err
			}
		}
	}
	return nil
}

// Tool implementations
func (r *Registry) deployWorkload(ctx context.Context, inputs map[string]interface{}) (string, error) {
	name := inputs["name"].(string)
	image := inputs["image"].(string)
	namespace := inputs["namespace"].(string)
	replicas := 1
	if r, ok := inputs["replicas"].(float64); ok {
		replicas = int(r)
	}

	env := make(map[string]string)
	if e, ok := inputs["env"].(map[string]interface{}); ok {
		for k, v := range e {
			env[k] = fmt.Sprintf("%v", v)
		}
	}

	req := client.DeploymentRequest{
		Name:      name,
		Image:     image,
		Namespace: namespace,
		Replicas:  replicas,
		Env:       env,
	}

	workload, err := r.client.DeployWorkload(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(workload, "", "  ")
	return string(data), nil
}

func (r *Registry) listWorkloads(ctx context.Context, inputs map[string]interface{}) (string, error) {
	namespace := ""
	if ns, ok := inputs["namespace"].(string); ok {
		namespace = ns
	}

	workloads, err := r.client.ListWorkloads(ctx, namespace)
	if err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(workloads, "", "  ")
	return string(data), nil
}

func (r *Registry) getWorkload(ctx context.Context, inputs map[string]interface{}) (string, error) {
	name := inputs["name"].(string)
	namespace := inputs["namespace"].(string)

	workload, err := r.client.GetWorkload(ctx, name, namespace)
	if err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(workload, "", "  ")
	return string(data), nil
}

func (r *Registry) restartWorkload(ctx context.Context, inputs map[string]interface{}) (string, error) {
	name := inputs["name"].(string)
	namespace := inputs["namespace"].(string)

	err := r.client.RestartWorkload(ctx, name, namespace)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Workload %s in namespace %s restarted successfully", name, namespace), nil
}

func (r *Registry) deleteWorkload(ctx context.Context, inputs map[string]interface{}) (string, error) {
	confirm, ok := inputs["confirm"].(bool)
	if !ok || !confirm {
		return "", fmt.Errorf("deletion requires confirm=true")
	}

	name := inputs["name"].(string)
	namespace := inputs["namespace"].(string)

	err := r.client.DeleteWorkload(ctx, name, namespace)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Workload %s in namespace %s deleted successfully", name, namespace), nil
}

func (r *Registry) listPods(ctx context.Context, inputs map[string]interface{}) (string, error) {
	workload := inputs["workload"].(string)
	namespace := inputs["namespace"].(string)

	pods, err := r.client.ListPods(ctx, workload, namespace)
	if err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(pods, "", "  ")
	return string(data), nil
}

func (r *Registry) streamLogs(ctx context.Context, inputs map[string]interface{}) (string, error) {
	pod := inputs["pod"].(string)
	namespace := inputs["namespace"].(string)
	tailLines := 100
	if tl, ok := inputs["tail_lines"].(float64); ok {
		tailLines = int(tl)
	}

	logs, err := r.client.StreamLogs(ctx, pod, namespace, tailLines)
	if err != nil {
		return "", err
	}

	return logs, nil
}

func (r *Registry) createNamespace(ctx context.Context, inputs map[string]interface{}) (string, error) {
	name := inputs["name"].(string)

	ns, err := r.client.CreateNamespace(ctx, name)
	if err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(ns, "", "  ")
	return string(data), nil
}

func (r *Registry) listNamespaces(ctx context.Context, inputs map[string]interface{}) (string, error) {
	namespaces, err := r.client.ListNamespaces(ctx)
	if err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(namespaces, "", "  ")
	return string(data), nil
}

func (r *Registry) analyzeWorkload(ctx context.Context, inputs map[string]interface{}) (string, error) {
	name := inputs["name"].(string)
	namespace := inputs["namespace"].(string)

	result, err := r.client.AnalyzeWorkload(ctx, name, namespace)
	if err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

func (r *Registry) generateManifest(ctx context.Context, inputs map[string]interface{}) (string, error) {
	description := inputs["description"].(string)

	manifest, err := r.client.GenerateManifest(ctx, description)
	if err != nil {
		return "", err
	}

	return manifest, nil
}

func (r *Registry) getClusterHealth(ctx context.Context, inputs map[string]interface{}) (string, error) {
	health, err := r.client.GetClusterHealth(ctx)
	if err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(health, "", "  ")
	return string(data), nil
}

func (r *Registry) naturalLanguageDeploy(ctx context.Context, inputs map[string]interface{}) (string, error) {
	command := inputs["command"].(string)
	execute := false
	if exec, ok := inputs["execute"].(bool); ok {
		execute = exec
	}

	intent, err := r.nlpParser.ParseDeploymentIntent(command)
	if err != nil {
		return "", fmt.Errorf("failed to parse command: %w", err)
	}

	errors := r.nlpParser.ValidateIntent(intent)
	if len(errors) > 0 {
		intentJSON, _ := json.MarshalIndent(intent, "", "  ")
		errorStr := ""
		for i, err := range errors {
			if i > 0 {
				errorStr += "\n- "
			}
			errorStr += err
		}
		return fmt.Sprintf("Parsing completed with validation errors:\n%s\n\nErrors:\n- %s", string(intentJSON), errorStr), nil
	}

	if !execute {
		intentJSON, _ := json.MarshalIndent(intent, "", "  ")
		return fmt.Sprintf("Parsed deployment intent (not executed):\n%s", string(intentJSON)), nil
	}

	req := client.DeploymentRequest{
		Name:      intent.Name,
		Image:     intent.Image,
		Namespace: intent.Namespace,
		Replicas:  intent.Replicas,
		Env:       intent.Env,
	}

	workload, err := r.client.DeployWorkload(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to deploy workload: %w", err)
	}

	result := map[string]interface{}{
		"message":  "Deployment executed successfully",
		"intent":   intent,
		"workload": workload,
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}
func (r *Registry) getHealingStatus(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.healer == nil {
		return `{"enabled": false, "message": "Healing mode not configured"}`, nil
	}

	status := r.healer.GetStatus()
	data, _ := json.MarshalIndent(status, "", "  ")
	return string(data), nil
}

//nolint:unused // Used via dynamic tool registration
func (r *Registry) getHealingHistory(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.healer == nil {
		return `{"history": [], "message": "Healing mode not configured"}`, nil
	}

	limit := 50
	if l, ok := inputs["limit"].(float64); ok {
		limit = int(l)
	}

	history := r.healer.GetActionHistory()
	if len(history) > limit {
		history = history[len(history)-limit:]
	}

	data, _ := json.MarshalIndent(history, "", "  ")
	return string(data), nil
}

//nolint:unused // Used via dynamic tool registration
func (r *Registry) resetHealingCount(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.healer == nil {
		return "", fmt.Errorf("healing mode not configured")
	}

	workload := inputs["workload"].(string)
	namespace := inputs["namespace"].(string)

	r.healer.ResetRestartCount(workload, namespace)
	return fmt.Sprintf("Reset restart count for workload %s in namespace %s", workload, namespace), nil
}

// Multi-agent coordination tool implementations
func (r *Registry) createTask(ctx context.Context, inputs map[string]interface{}) (string, error) {
	title := inputs["title"].(string)
	assignedTo := inputs["assigned_to"].(string)
	description := ""
	if desc, ok := inputs["description"].(string); ok {
		description = desc
	}
	priority := "medium"
	if prio, ok := inputs["priority"].(string); ok {
		priority = prio
	}

	taskInputs := make(map[string]interface{})
	if in, ok := inputs["inputs"].(map[string]interface{}); ok {
		taskInputs = in
	}

	// Get agent ID from context
	createdBy := "unknown"
	if agentID, ok := ctx.Value("agent_id").(string); ok {
		createdBy = agentID
	}

	task := &coordination.Task{
		Title:       title,
		Description: description,
		AssignedTo:  assignedTo,
		CreatedBy:   createdBy,
		Priority:    priority,
		Inputs:      taskInputs,
	}

	if r.coordinator == nil {
		return "", fmt.Errorf("coordinator not initialized")
	}

	createdTask, err := r.coordinator.CreateTask(ctx, task)
	if err != nil {
		return "", err
	}

	return createdTask.ToJSON()
}

func (r *Registry) delegateTask(ctx context.Context, inputs map[string]interface{}) (string, error) {
	taskID := inputs["task_id"].(string)
	targetAgent := inputs["target_agent"].(string)

	if r.coordinator == nil {
		return "", fmt.Errorf("coordinator not initialized")
	}

	task, err := r.coordinator.DelegateTask(ctx, taskID, targetAgent)
	if err != nil {
		return "", err
	}

	return task.ToJSON()
}

func (r *Registry) getTask(ctx context.Context, inputs map[string]interface{}) (string, error) {
	taskID := inputs["task_id"].(string)

	if r.coordinator == nil {
		return "", fmt.Errorf("coordinator not initialized")
	}

	task, err := r.coordinator.GetTask(taskID)
	if err != nil {
		return "", err
	}

	return task.ToJSON()
}

func (r *Registry) listTasks(ctx context.Context, inputs map[string]interface{}) (string, error) {
	agentID := ""
	if aid, ok := inputs["agent_id"].(string); ok {
		agentID = aid
	}
	status := ""
	if st, ok := inputs["status"].(string); ok {
		status = st
	}

	if r.coordinator == nil {
		return "", fmt.Errorf("coordinator not initialized")
	}

	tasks, err := r.coordinator.ListTasks(agentID, status)
	if err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(tasks, "", "  ")
	return string(data), nil
}

func (r *Registry) updateTaskStatus(ctx context.Context, inputs map[string]interface{}) (string, error) {
	taskID := inputs["task_id"].(string)
	status := inputs["status"].(string)

	outputs := make(map[string]interface{})
	if out, ok := inputs["outputs"].(map[string]interface{}); ok {
		outputs = out
	}

	if r.coordinator == nil {
		return "", fmt.Errorf("coordinator not initialized")
	}

	task, err := r.coordinator.UpdateTaskStatus(ctx, taskID, status, outputs)
	if err != nil {
		return "", err
	}

	return task.ToJSON()
}

func (r *Registry) createSubtask(ctx context.Context, inputs map[string]interface{}) (string, error) {
	parentTaskID := inputs["parent_task_id"].(string)
	title := inputs["title"].(string)
	assignedTo := inputs["assigned_to"].(string)
	description := ""
	if desc, ok := inputs["description"].(string); ok {
		description = desc
	}

	subTask := &coordination.Task{
		Title:       title,
		Description: description,
		AssignedTo:  assignedTo,
	}

	if r.coordinator == nil {
		return "", fmt.Errorf("coordinator not initialized")
	}

	createdTask, err := r.coordinator.CreateSubTask(ctx, parentTaskID, subTask)
	if err != nil {
		return "", err
	}

	return createdTask.ToJSON()
}

func (r *Registry) claimTask(ctx context.Context, inputs map[string]interface{}) (string, error) {
	taskID := inputs["task_id"].(string)
	agentID := inputs["agent_id"].(string)

	if r.coordinator == nil {
		return "", fmt.Errorf("coordinator not initialized")
	}

	task, err := r.coordinator.ClaimTask(ctx, taskID, agentID)
	if err != nil {
		return "", err
	}

	return task.ToJSON()
}

// Dry-run mode tool implementations
func (r *Registry) setDryRunMode(ctx context.Context, inputs map[string]interface{}) (string, error) {
	mode := inputs["mode"].(string)

	if r.dryRunner == nil {
		return "", fmt.Errorf("dry runner not initialized")
	}

	r.dryRunner.SetMode(dryrun.DryRunMode(mode))
	return fmt.Sprintf("Dry-run mode set to: %s", mode), nil
}

func (r *Registry) getDryRunMode(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.dryRunner == nil {
		return "", fmt.Errorf("dry runner not initialized")
	}

	mode := r.dryRunner.GetMode()
	return fmt.Sprintf(`{"mode": "%s"}`, mode), nil
}

func (r *Registry) getDryRunPreview(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.dryRunner == nil {
		return "", fmt.Errorf("dry runner not initialized")
	}

	preview, err := r.dryRunner.GetPreview()
	if err != nil {
		return "", err
	}

	return preview, nil
}

func (r *Registry) clearDryRunActions(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.dryRunner == nil {
		return "", fmt.Errorf("dry runner not initialized")
	}

	r.dryRunner.ClearActions()
	return "Dry-run actions cleared", nil
}

// Incident response tool implementations
func (r *Registry) listRunbooks(ctx context.Context, inputs map[string]interface{}) (string, error) {
	category := ""
	if cat, ok := inputs["category"].(string); ok {
		category = cat
	}

	if r.incidentMgr == nil {
		return "", fmt.Errorf("incident manager not initialized")
	}

	runbooks, err := r.incidentMgr.ListRunbooks(category)
	if err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(runbooks, "", "  ")
	return string(data), nil
}

func (r *Registry) getRunbook(ctx context.Context, inputs map[string]interface{}) (string, error) {
	runbookID := inputs["runbook_id"].(string)

	if r.incidentMgr == nil {
		return "", fmt.Errorf("incident manager not initialized")
	}

	runbook, err := r.incidentMgr.GetRunbook(runbookID)
	if err != nil {
		return "", err
	}

	return runbook.ToJSON()
}

func (r *Registry) executeRunbook(ctx context.Context, inputs map[string]interface{}) (string, error) {
	runbookID := inputs["runbook_id"].(string)
	agentID := inputs["agent_id"].(string)

	context := make(map[string]interface{})
	if ctx, ok := inputs["context"].(map[string]interface{}); ok {
		context = ctx
	}

	if r.incidentMgr == nil {
		return "", fmt.Errorf("incident manager not initialized")
	}

	execution, err := r.incidentMgr.ExecuteRunbook(ctx, runbookID, agentID, context)
	if err != nil {
		return "", err
	}

	return execution.ToJSON()
}

func (r *Registry) getExecution(ctx context.Context, inputs map[string]interface{}) (string, error) {
	executionID := inputs["execution_id"].(string)

	if r.incidentMgr == nil {
		return "", fmt.Errorf("incident manager not initialized")
	}

	execution, err := r.incidentMgr.GetExecution(executionID)
	if err != nil {
		return "", err
	}

	return execution.ToJSON()
}

func (r *Registry) listExecutions(ctx context.Context, inputs map[string]interface{}) (string, error) {
	runbookID := ""
	if rbID, ok := inputs["runbook_id"].(string); ok {
		runbookID = rbID
	}
	status := ""
	if st, ok := inputs["status"].(string); ok {
		status = st
	}

	if r.incidentMgr == nil {
		return "", fmt.Errorf("incident manager not initialized")
	}

	executions, err := r.incidentMgr.ListExecutions(runbookID, status)
	if err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(executions, "", "  ")
	return string(data), nil
}

func (r *Registry) cancelExecution(ctx context.Context, inputs map[string]interface{}) (string, error) {
	executionID := inputs["execution_id"].(string)

	if r.incidentMgr == nil {
		return "", fmt.Errorf("incident manager not initialized")
	}

	err := r.incidentMgr.CancelExecution(executionID)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Execution %s cancelled", executionID), nil
}

func (r *Registry) createRunbook(ctx context.Context, inputs map[string]interface{}) (string, error) {
	name := inputs["name"].(string)
	description := ""
	if desc, ok := inputs["description"].(string); ok {
		description = desc
	}
	category := ""
	if cat, ok := inputs["category"].(string); ok {
		category = cat
	}
	severity := "medium"
	if sev, ok := inputs["severity"].(string); ok {
		severity = sev
	}

	steps := []incident.Step{}
	if st, ok := inputs["steps"].([]interface{}); ok {
		for _, s := range st {
			stepMap, ok := s.(map[string]interface{})
			if !ok {
				continue
			}
			step := incident.Step{
				ID:          fmt.Sprintf("step-%d", len(steps)),
				Name:        stepMap["name"].(string),
				Description: stepMap["description"].(string),
				Tool:        stepMap["tool"].(string),
				OnFailure:   "stop",
				Timeout:     5 * time.Minute,
			}
			if inputsData, ok := stepMap["inputs"].(map[string]interface{}); ok {
				step.Inputs = inputsData
			}
			steps = append(steps, step)
		}
	}

	runbook := &incident.Runbook{
		Name:        name,
		Description: description,
		Category:    category,
		Severity:    severity,
		Steps:       steps,
	}

	if r.incidentMgr == nil {
		return "", fmt.Errorf("incident manager not initialized")
	}

	err := r.incidentMgr.CreateRunbook(runbook)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Runbook %s created", runbook.Name), nil
}

// Session Memory Tool Handlers
func (r *Registry) setMemoryContext(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.sessionMemory == nil {
		return "", fmt.Errorf("session memory not initialized")
	}

	key := inputs["key"].(string)
	value := inputs["value"].(string)

	agentID := "unknown"
	if aid, ok := ctx.Value("agent_id").(string); ok {
		agentID = aid
	}

	r.sessionMemory.SetContext(agentID, key, value)
	return fmt.Sprintf("Context set: %s = %s", key, value), nil
}

func (r *Registry) getMemoryContext(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.sessionMemory == nil {
		return "", fmt.Errorf("session memory not initialized")
	}

	key := inputs["key"].(string)

	agentID := "unknown"
	if aid, ok := ctx.Value("agent_id").(string); ok {
		agentID = aid
	}

	value, exists := r.sessionMemory.GetContext(agentID, key)
	if !exists {
		return fmt.Sprintf("Key '%s' not found in session memory", key), nil
	}

	return fmt.Sprintf("%v", value), nil
}

func (r *Registry) getAllMemoryContext(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.sessionMemory == nil {
		return "", fmt.Errorf("session memory not initialized")
	}

	agentID := "unknown"
	if aid, ok := ctx.Value("agent_id").(string); ok {
		agentID = aid
	}

	context := r.sessionMemory.GetAllContext(agentID)
	data, _ := json.MarshalIndent(context, "", "  ")
	return string(data), nil
}

func (r *Registry) getSessionHistory(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.sessionMemory == nil {
		return "", fmt.Errorf("session memory not initialized")
	}

	agentID := "unknown"
	if aid, ok := ctx.Value("agent_id").(string); ok {
		agentID = aid
	}

	limit := 10
	if lim, ok := inputs["limit"].(float64); ok {
		limit = int(lim)
	}

	history := r.sessionMemory.GetHistory(agentID)
	if limit > 0 && len(history) > limit {
		history = history[len(history)-limit:]
	}

	data, _ := json.MarshalIndent(history, "", "  ")
	return string(data), nil
}

func (r *Registry) clearSessionHistory(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.sessionMemory == nil {
		return "", fmt.Errorf("session memory not initialized")
	}

	agentID := "unknown"
	if aid, ok := ctx.Value("agent_id").(string); ok {
		agentID = aid
	}

	r.sessionMemory.ClearHistory(agentID)
	return "Session history cleared", nil
}

// Per-Agent Audit Tool Handlers
func (r *Registry) getAgentAuditLog(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.auditLogger == nil {
		return "", fmt.Errorf("audit logger not initialized")
	}

	agentID := inputs["agent_id"].(string)
	limit := 50
	if lim, ok := inputs["limit"].(float64); ok {
		limit = int(lim)
	}

	events := r.auditLogger.QueryByAgent(agentID)
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}

	data, _ := json.MarshalIndent(events, "", "  ")
	return string(data), nil
}

func (r *Registry) getAgentSummary(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.auditLogger == nil {
		return "", fmt.Errorf("audit logger not initialized")
	}

	agentID := inputs["agent_id"].(string)
	summary := r.auditLogger.GetAgentSummary(agentID)

	data, _ := json.MarshalIndent(summary, "", "  ")
	return string(data), nil
}

func (r *Registry) listAllAgents(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.auditLogger == nil {
		return "", fmt.Errorf("audit logger not initialized")
	}

	agents := r.auditLogger.GetAllAgents()
	data, _ := json.MarshalIndent(agents, "", "  ")
	return string(data), nil
}

func (r *Registry) exportAuditLog(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.auditLogger == nil {
		return "", fmt.Errorf("audit logger not initialized")
	}

	agentID := ""
	if aid, ok := inputs["agent_id"].(string); ok {
		agentID = aid
	}

	data, err := r.auditLogger.ExportAuditLog(agentID)
	if err != nil {
		return "", err
	}
	return data, nil
}

// Scheduler Tool Handlers
func (r *Registry) createScheduledTask(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.taskScheduler == nil {
		return "", fmt.Errorf("scheduler not initialized")
	}

	task := &scheduler.Task{
		ID:          inputs["id"].(string),
		Name:        inputs["name"].(string),
		Description: "",
		CronExpr:    inputs["cron_expr"].(string),
		Enabled:     true,
		ToolName:    inputs["tool_name"].(string),
		Inputs:      make(map[string]interface{}),
	}

	if desc, ok := inputs["description"].(string); ok {
		task.Description = desc
	}
	if in, ok := inputs["inputs"].(map[string]interface{}); ok {
		task.Inputs = in
	}
	if enabled, ok := inputs["enabled"].(bool); ok {
		task.Enabled = enabled
	}

	agentID := "unknown"
	if aid, ok := ctx.Value("agent_id").(string); ok {
		agentID = aid
	}
	task.CreatedBy = agentID

	err := r.taskScheduler.AddTask(task)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Scheduled task %s created", task.ID), nil
}

func (r *Registry) listScheduledTasks(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.taskScheduler == nil {
		return "", fmt.Errorf("scheduler not initialized")
	}

	tasks := r.taskScheduler.ListTasks()
	data, _ := json.MarshalIndent(tasks, "", "  ")
	return string(data), nil
}

func (r *Registry) getScheduledTask(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.taskScheduler == nil {
		return "", fmt.Errorf("scheduler not initialized")
	}

	taskID := inputs["task_id"].(string)
	task, err := r.taskScheduler.GetTask(taskID)
	if err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data), nil
}

func (r *Registry) updateScheduledTask(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.taskScheduler == nil {
		return "", fmt.Errorf("scheduler not initialized")
	}

	task := &scheduler.Task{
		ID: inputs["id"].(string),
	}

	if name, ok := inputs["name"].(string); ok {
		task.Name = name
	}
	if desc, ok := inputs["description"].(string); ok {
		task.Description = desc
	}
	if cron, ok := inputs["cron_expr"].(string); ok {
		task.CronExpr = cron
	}
	if tool, ok := inputs["tool_name"].(string); ok {
		task.ToolName = tool
	}
	if in, ok := inputs["inputs"].(map[string]interface{}); ok {
		task.Inputs = in
	}
	if enabled, ok := inputs["enabled"].(bool); ok {
		task.Enabled = enabled
	}

	err := r.taskScheduler.UpdateTask(task)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Scheduled task %s updated", task.ID), nil
}

func (r *Registry) deleteScheduledTask(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.taskScheduler == nil {
		return "", fmt.Errorf("scheduler not initialized")
	}

	taskID := inputs["task_id"].(string)
	err := r.taskScheduler.DeleteTask(taskID)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Scheduled task %s deleted", taskID), nil
}

func (r *Registry) enableScheduledTask(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.taskScheduler == nil {
		return "", fmt.Errorf("scheduler not initialized")
	}

	taskID := inputs["task_id"].(string)
	err := r.taskScheduler.EnableTask(taskID)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Scheduled task %s enabled", taskID), nil
}

func (r *Registry) disableScheduledTask(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.taskScheduler == nil {
		return "", fmt.Errorf("scheduler not initialized")
	}

	taskID := inputs["task_id"].(string)
	err := r.taskScheduler.DisableTask(taskID)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Scheduled task %s disabled", taskID), nil
}

func (r *Registry) getTaskResults(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.taskScheduler == nil {
		return "", fmt.Errorf("scheduler not initialized")
	}

	taskID := inputs["task_id"].(string)
	limit := 10
	if lim, ok := inputs["limit"].(float64); ok {
		limit = int(lim)
	}

	results := r.taskScheduler.GetTaskResults(taskID, limit)
	data, _ := json.MarshalIndent(results, "", "  ")
	return string(data), nil
}

func (r *Registry) getSchedulerStats(ctx context.Context, inputs map[string]interface{}) (string, error) {
	if r.taskScheduler == nil {
		return "", fmt.Errorf("scheduler not initialized")
	}

	stats := r.taskScheduler.GetStats()
	data, _ := json.MarshalIndent(stats, "", "  ")
	return string(data), nil
}
