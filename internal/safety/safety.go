package safety

import (
	"fmt"
)

type PermissionScope string

const (
	ScopeReadOnly PermissionScope = "readonly"
	ScopeWrite    PermissionScope = "write"
	ScopeAdmin    PermissionScope = "admin"
)

type AgentPermissions struct {
	Scope        PermissionScope `json:"scope"`
	Namespaces   []string        `json:"namespaces"`    // Empty means all namespaces
	AllowedTools []string        `json:"allowed_tools"` // Empty means all tools based on scope
}

type SafetyPolicy struct {
	allowDeleteWorkload  bool
	allowCreateNamespace bool
	readonlyMode         bool
	agentPermissions     map[string]AgentPermissions // agent_id -> permissions
	defaultScope         PermissionScope
}

type ToolPermission struct {
	Allowed              bool
	Reason               string
	RequiresConfirmation bool
}

func New(config map[string]interface{}) *SafetyPolicy {
	policy := &SafetyPolicy{
		allowDeleteWorkload:  getBool(config, "allow_delete_workload", true),
		allowCreateNamespace: getBool(config, "allow_create_namespace", true),
		readonlyMode:         getBool(config, "readonly_mode", false),
		agentPermissions:     make(map[string]AgentPermissions),
		defaultScope:         ScopeWrite,
	}

	// Parse default scope
	if scope, ok := config["default_scope"].(string); ok {
		policy.defaultScope = PermissionScope(scope)
	}

	// Parse agent permissions
	if agents, ok := config["agents"].(map[string]interface{}); ok {
		for agentID, agentConfig := range agents {
			if agentMap, ok := agentConfig.(map[string]interface{}); ok {
				perms := AgentPermissions{
					Scope: ScopeWrite, // default
				}

				if scope, ok := agentMap["scope"].(string); ok {
					perms.Scope = PermissionScope(scope)
				}

				if namespaces, ok := agentMap["namespaces"].([]interface{}); ok {
					for _, ns := range namespaces {
						if nsStr, ok := ns.(string); ok {
							perms.Namespaces = append(perms.Namespaces, nsStr)
						}
					}
				}

				if tools, ok := agentMap["allowed_tools"].([]interface{}); ok {
					for _, tool := range tools {
						if toolStr, ok := tool.(string); ok {
							perms.AllowedTools = append(perms.AllowedTools, toolStr)
						}
					}
				}

				policy.agentPermissions[agentID] = perms
			}
		}
	}

	return policy
}

func getBool(config map[string]interface{}, key string, defaultValue bool) bool {
	if val, ok := config[key]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return defaultValue
}

func (s *SafetyPolicy) CheckPermission(toolName string, inputs map[string]interface{}) ToolPermission {
	return s.CheckPermissionWithContext(toolName, inputs, "")
}

func (s *SafetyPolicy) CheckPermissionWithContext(toolName string, inputs map[string]interface{}, agentID string) ToolPermission {
	// Check agent-specific permissions if agent ID is provided
	if agentID != "" {
		if perms, exists := s.agentPermissions[agentID]; exists {
			// Check if tool is in allowed tools list (if specified)
			if len(perms.AllowedTools) > 0 {
				allowed := false
				for _, tool := range perms.AllowedTools {
					if tool == toolName {
						allowed = true
						break
					}
				}
				if !allowed {
					return ToolPermission{
						Allowed: false,
						Reason:  fmt.Sprintf("Tool '%s' not in agent's allowed tools list", toolName),
					}
				}
			}

			// Check namespace access
			if len(perms.Namespaces) > 0 {
				if namespace, ok := inputs["namespace"].(string); ok {
					allowed := false
					for _, ns := range perms.Namespaces {
						if ns == namespace {
							allowed = true
							break
						}
					}
					if !allowed {
						return ToolPermission{
							Allowed: false,
							Reason:  fmt.Sprintf("Agent not authorized to access namespace '%s'", namespace),
						}
					}
				}
			}

			// Apply scope-based restrictions
			switch perms.Scope {
			case "ScopeReadOnly":
				readOnlyTools := map[string]bool{
					"list_workloads":          true,
					"get_workload":            true,
					"list_pods":               true,
					"stream_logs":             true,
					"list_namespaces":         true,
					"get_cluster_health":      true,
					"analyze_workload":        true,
					"generate_manifest":       true,
					"natural_language_deploy": true, // Can parse but not execute
				}
				if !readOnlyTools[toolName] {
					return ToolPermission{
						Allowed: false,
						Reason:  fmt.Sprintf("Agent has read-only scope - tool '%s' requires write permissions", toolName),
					}
				}
			case "ScopeWrite":
				// Write scope allows deploy, restart, create_namespace but not delete
				adminOnlyTools := map[string]bool{
					"delete_workload": true,
				}
				if adminOnlyTools[toolName] {
					return ToolPermission{
						Allowed: false,
						Reason:  fmt.Sprintf("Tool '%s' requires admin scope", toolName),
					}
				}
			case "ScopeAdmin":
				// Admin scope allows all tools
				// Fall through to additional checks
			}
		} else {
			// Agent not found, apply default scope
			switch s.defaultScope {
			case "ScopeReadOnly":
				readOnlyTools := map[string]bool{
					"list_workloads":          true,
					"get_workload":            true,
					"list_pods":               true,
					"stream_logs":             true,
					"list_namespaces":         true,
					"get_cluster_health":      true,
					"analyze_workload":        true,
					"generate_manifest":       true,
					"natural_language_deploy": true,
				}
				if !readOnlyTools[toolName] {
					return ToolPermission{
						Allowed: false,
						Reason:  fmt.Sprintf("Default scope is read-only - tool '%s' requires write permissions", toolName),
					}
				}
			case "ScopeWrite":
				adminOnlyTools := map[string]bool{
					"delete_workload": true,
				}
				if adminOnlyTools[toolName] {
					return ToolPermission{
						Allowed: false,
						Reason:  fmt.Sprintf("Tool '%s' requires admin scope", toolName),
					}
				}
			}
		}
	}

	if s.readonlyMode {
		// In readonly mode, only allow read operations
		readOnlyTools := map[string]bool{
			"list_workloads":     true,
			"get_workload":       true,
			"list_pods":          true,
			"stream_logs":        true,
			"list_namespaces":    true,
			"get_cluster_health": true,
		}
		if !readOnlyTools[toolName] {
			return ToolPermission{
				Allowed: false,
				Reason:  "Readonly mode is enabled - only read operations are permitted",
			}
		}
	}

	switch toolName {
	case "delete_workload":
		if !s.allowDeleteWorkload {
			return ToolPermission{
				Allowed: false,
				Reason:  "Workload deletion is disabled by safety policy",
			}
		}
		return ToolPermission{
			Allowed:              true,
			RequiresConfirmation: true,
		}

	case "create_namespace":
		if !s.allowCreateNamespace {
			return ToolPermission{
				Allowed: false,
				Reason:  "Namespace creation is disabled by safety policy",
			}
		}
		return ToolPermission{
			Allowed: true,
		}

	case "deploy_workload", "restart_workload":
		return ToolPermission{
			Allowed: true,
		}

	// Read-only tools are always allowed
	case "list_workloads", "get_workload", "list_pods", "stream_logs",
		"list_namespaces", "analyze_workload", "generate_manifest", "get_cluster_health":
		return ToolPermission{
			Allowed: true,
		}

	// Multi-agent coordination tools - generally allowed for write scope and above
	case "create_task", "delegate_task", "get_task", "list_tasks",
		"update_task_status", "create_subtask", "claim_task":
		return ToolPermission{
			Allowed: true,
		}

	// Dry-run mode tools - always allowed
	case "set_dryrun_mode", "get_dryrun_mode", "get_dryrun_preview", "clear_dryrun_actions":
		return ToolPermission{
			Allowed: true,
		}

	// Incident response tools - require write scope or above
	case "list_runbooks", "get_runbook", "get_execution", "list_executions":
		return ToolPermission{
			Allowed: true,
		}
	case "execute_runbook", "cancel_execution", "create_runbook":
		return ToolPermission{
			Allowed:              true,
			RequiresConfirmation: true,
		}

	default:
		return ToolPermission{
			Allowed: false,
			Reason:  "Unknown tool - not permitted by safety policy",
		}
	}
}

// ValidateNamespace ensures namespace name is safe
func (s *SafetyPolicy) ValidateNamespace(namespace string) error {
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	// Add more validation as needed (e.g., prevent kube-system, etc.)
	if namespace == "kube-system" || namespace == "kube-public" {
		return fmt.Errorf("access to system namespace '%s' is not allowed", namespace)
	}
	return nil
}

// ValidateWorkloadName ensures workload name is safe
func (s *SafetyPolicy) ValidateWorkloadName(name string) error {
	if name == "" {
		return fmt.Errorf("workload name cannot be empty")
	}
	return nil
}
