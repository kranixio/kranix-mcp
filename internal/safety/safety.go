package safety

import (
	"fmt"
)

type SafetyPolicy struct {
	allowDeleteWorkload  bool
	allowCreateNamespace bool
	readonlyMode         bool
}

type ToolPermission struct {
	Allowed              bool
	Reason               string
	RequiresConfirmation bool
}

func New(config map[string]interface{}) *SafetyPolicy {
	return &SafetyPolicy{
		allowDeleteWorkload:  getBool(config, "allow_delete_workload", true),
		allowCreateNamespace: getBool(config, "allow_create_namespace", true),
		readonlyMode:         getBool(config, "readonly_mode", false),
	}
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
