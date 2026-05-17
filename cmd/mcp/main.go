package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kranix-io/kranix-mcp/internal/audit"
	"github.com/kranix-io/kranix-mcp/internal/client"
	"github.com/kranix-io/kranix-mcp/internal/healing"
	"github.com/kranix-io/kranix-mcp/internal/safety"
	"github.com/kranix-io/kranix-mcp/internal/server"
	"github.com/kranix-io/kranix-mcp/internal/tools"
	"gopkg.in/yaml.v3"
)

type Config struct {
	MCP      MCPConfig      `yaml:"mcp"`
	KraneAPI KraneAPIConfig `yaml:"krane_api"`
	Safety   SafetyConfig   `yaml:"safety"`
	Healing  HealingConfig  `yaml:"healing"`
	Audit    AuditConfig    `yaml:"audit"`
}
type MCPConfig struct {
	Transport string `yaml:"transport"` // stdio | http
	Port      int    `yaml:"port"`
}

type KraneAPIConfig struct {
	URL     string `yaml:"url"`
	APIKey  string `yaml:"api_key"`
	Timeout string `yaml:"timeout"`
}

type SafetyConfig struct {
	AllowDeleteWorkload  bool                   `yaml:"allow_delete_workload"`
	AllowCreateNamespace bool                   `yaml:"allow_create_namespace"`
	ReadonlyMode         bool                   `yaml:"readonly_mode"`
	DefaultScope         string                 `yaml:"default_scope"`
	Agents               map[string]AgentConfig `yaml:"agents"`
}

type AgentConfig struct {
	Scope        string   `yaml:"scope"`
	Namespaces   []string `yaml:"namespaces"`
	AllowedTools []string `yaml:"allowed_tools"`
}

type HealingConfig struct {
	Enabled          bool          `yaml:"enabled"`
	Mode             string        `yaml:"mode"`
	CheckInterval    time.Duration `yaml:"check_interval"`
	MaxRestarts      int           `yaml:"max_restarts_per_hour"`
	RestartCooldown  time.Duration `yaml:"restart_cooldown"`
	AutoScaleEnabled bool          `yaml:"auto_scale_enabled"`
	MinReplicas      int           `yaml:"min_replicas"`
	MaxReplicas      int           `yaml:"max_replicas"`
	Namespaces       []string      `yaml:"namespaces"`
}

type AuditConfig struct {
	Enabled bool   `yaml:"enabled"`
	Sink    string `yaml:"sink"` // stdout | file
}

func main() {
	configPath := flag.String("config", "config/config.yaml", "Path to config file")
	transport := flag.String("transport", "", "Transport type (stdio or http, overrides config)")
	port := flag.Int("port", 0, "Port for HTTP transport (overrides config)")
	flag.Parse()

	// Load config
	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override with CLI flags
	if *transport != "" {
		config.MCP.Transport = *transport
	}
	if *port != 0 {
		config.MCP.Port = *port
	}

	// Override with environment variables
	if apiURL := os.Getenv("KRANE_API_URL"); apiURL != "" {
		config.KraneAPI.URL = apiURL
	}
	if apiKey := os.Getenv("KRANE_API_KEY"); apiKey != "" {
		config.KraneAPI.APIKey = apiKey
	}
	if transport := os.Getenv("KRANE_MCP_TRANSPORT"); transport != "" {
		config.MCP.Transport = transport
	}
	if port := os.Getenv("KRANE_MCP_PORT"); port != "" {
		fmt.Sscanf(port, "%d", &config.MCP.Port)
	}

	// Validate config
	if config.KraneAPI.URL == "" {
		log.Fatal("KRANE_API_URL must be set")
	}
	if config.KraneAPI.APIKey == "" {
		log.Fatal("KRANE_API_KEY must be set")
	}
	if config.MCP.Transport == "" {
		config.MCP.Transport = "stdio"
	}

	// Initialize components
	kraneClient := client.New(config.KraneAPI.URL, config.KraneAPI.APIKey)
	auditLogger := audit.New(config.Audit.Enabled, config.Audit.Sink)

	// Build safety config
	safetyConfigMap := map[string]interface{}{
		"allow_delete_workload":  config.Safety.AllowDeleteWorkload,
		"allow_create_namespace": config.Safety.AllowCreateNamespace,
		"readonly_mode":          config.Safety.ReadonlyMode,
		"default_scope":          config.Safety.DefaultScope,
		"agents":                 buildAgentConfigMap(config.Safety.Agents),
	}
	safetyPolicy := safety.New(safetyConfigMap)

	// Initialize healer
	healer := healing.New(&healing.Config{
		Enabled:          config.Healing.Enabled,
		Mode:             healing.HealingMode(config.Healing.Mode),
		CheckInterval:    config.Healing.CheckInterval,
		MaxRestarts:      config.Healing.MaxRestarts,
		RestartCooldown:  config.Healing.RestartCooldown,
		AutoScaleEnabled: config.Healing.AutoScaleEnabled,
		MinReplicas:      config.Healing.MinReplicas,
		MaxReplicas:      config.Healing.MaxReplicas,
		Namespaces:       config.Healing.Namespaces,
	}, kraneClient, auditLogger)

	// Register all tools
	toolRegistry := tools.New(kraneClient, auditLogger, safetyPolicy, healer)
	toolRegistry.RegisterTools()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start healer if enabled
	if config.Healing.Enabled {
		healer.Start()
		defer healer.Stop()
	}

	// Handle shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		cancel()
	}()

	// Start MCP server
	mcpServer := server.New(config.MCP.Transport, config.MCP.Port, toolRegistry)
	if err := mcpServer.Start(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default config if file doesn't exist
			return &Config{
				MCP: MCPConfig{
					Transport: "stdio",
					Port:      3100,
				},
				KraneAPI: KraneAPIConfig{
					URL:     "http://localhost:8080",
					Timeout: "30s",
				},
				Safety: SafetyConfig{
					AllowDeleteWorkload:  true,
					AllowCreateNamespace: true,
					ReadonlyMode:         false,
					DefaultScope:         "write",
					Agents:               make(map[string]AgentConfig),
				},
				Healing: HealingConfig{
					Enabled:          false,
					Mode:             "observe",
					CheckInterval:    30 * time.Second,
					MaxRestarts:      10,
					RestartCooldown:  5 * time.Minute,
					AutoScaleEnabled: true,
					MinReplicas:      1,
					MaxReplicas:      10,
					Namespaces:       []string{},
				},
				Audit: AuditConfig{
					Enabled: true,
					Sink:    "stdout",
				},
			}, nil
		}
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// Set defaults for healing config if not specified
	if config.Healing.CheckInterval == 0 {
		config.Healing.CheckInterval = 30 * time.Second
	}
	if config.Healing.RestartCooldown == 0 {
		config.Healing.RestartCooldown = 5 * time.Minute
	}
	if config.Healing.MaxRestarts == 0 {
		config.Healing.MaxRestarts = 10
	}
	if config.Healing.MinReplicas == 0 {
		config.Healing.MinReplicas = 1
	}
	if config.Healing.MaxReplicas == 0 {
		config.Healing.MaxReplicas = 10
	}

	return &config, nil
}

func buildAgentConfigMap(agents map[string]AgentConfig) map[string]interface{} {
	result := make(map[string]interface{})
	for agentID, config := range agents {
		agentMap := map[string]interface{}{
			"scope":         config.Scope,
			"namespaces":    config.Namespaces,
			"allowed_tools": config.AllowedTools,
		}
		result[agentID] = agentMap
	}
	return result
}
