package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kranix-io/kranix-mcp/internal/audit"
	"github.com/kranix-io/kranix-mcp/internal/client"
	"github.com/kranix-io/kranix-mcp/internal/healing"
	"github.com/kranix-io/kranix-mcp/internal/nlp"
	"github.com/kranix-io/kranix-mcp/internal/safety"
)

type ToolFunc func(ctx context.Context, inputs map[string]interface{}) (string, error)

type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     ToolFunc
}

type Registry struct {
	client      *client.Client
	auditLogger *audit.AuditLogger
	safety      *safety.SafetyPolicy
	healer      *healing.Healer
	nlpParser   *nlp.Parser
	tools       map[string]ToolDefinition
}

func New(client *client.Client, auditLogger *audit.AuditLogger, safety *safety.SafetyPolicy, healer *healing.Healer) *Registry {
	return &Registry{
		client:      client,
		auditLogger: auditLogger,
		safety:      safety,
		healer:      healer,
		nlpParser:   nlp.NewParser(),
		tools:       make(map[string]ToolDefinition),
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
