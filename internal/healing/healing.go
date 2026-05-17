package healing

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/kranix-io/kranix-mcp/internal/audit"
	"github.com/kranix-io/kranix-mcp/internal/client"
)

type HealingMode string

const (
	HealingModeDisabled HealingMode = "disabled"
	HealingModeObserve  HealingMode = "observe"
	HealingModeAuto     HealingMode = "auto"
)

type Config struct {
	Enabled          bool          `yaml:"enabled"`
	Mode             HealingMode   `yaml:"mode"`
	CheckInterval    time.Duration `yaml:"check_interval"`
	MaxRestarts      int           `yaml:"max_restarts_per_hour"`
	RestartCooldown  time.Duration `yaml:"restart_cooldown"`
	AutoScaleEnabled bool          `yaml:"auto_scale_enabled"`
	MinReplicas      int           `yaml:"min_replicas"`
	MaxReplicas      int           `yaml:"max_replicas"`
	Namespaces       []string      `yaml:"namespaces"`
}

type HealingAction struct {
	Type       string                 `json:"type"`
	Workload   string                 `json:"workload"`
	Namespace  string                 `json:"namespace"`
	Reason     string                 `json:"reason"`
	ExecutedAt time.Time              `json:"executed_at"`
	Success    bool                   `json:"success"`
	Metadata   map[string]interface{} `json:"metadata"`
}

type Healer struct {
	config      *Config
	client      *client.Client
	auditLogger *audit.AuditLogger
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup

	// Track restarts per workload to prevent restart loops
	restartCounts map[string]int
	restartTimes  map[string]time.Time
	mu            sync.RWMutex

	// Action history
	actionHistory []HealingAction
	historyMu     sync.RWMutex
}

func New(config *Config, apiClient *client.Client, auditLogger *audit.AuditLogger) *Healer {
	ctx, cancel := context.WithCancel(context.Background())
	return &Healer{
		config:        config,
		client:        apiClient,
		auditLogger:   auditLogger,
		ctx:           ctx,
		cancel:        cancel,
		restartCounts: make(map[string]int),
		restartTimes:  make(map[string]time.Time),
		actionHistory: make([]HealingAction, 0),
	}
}

func (h *Healer) Start() {
	if !h.config.Enabled {
		log.Println("Autonomous healing mode is disabled")
		return
	}

	log.Printf("Starting autonomous healing mode (mode: %s, interval: %v)", h.config.Mode, h.config.CheckInterval)
	h.wg.Add(1)
	go h.runLoop()
}

func (h *Healer) Stop() {
	h.cancel()
	h.wg.Wait()
	log.Println("Autonomous healing mode stopped")
}

func (h *Healer) runLoop() {
	defer h.wg.Done()

	ticker := time.NewTicker(h.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.checkAndHeal()
		}
	}
}

func (h *Healer) checkAndHeal() {
	log.Println("Running autonomous health check...")

	// Get cluster health
	health, err := h.client.GetClusterHealth(h.ctx)
	if err != nil {
		log.Printf("Failed to get cluster health: %v", err)
		return
	}

	log.Printf("Cluster health: status=%s, nodes=%d/%d, pods=%d/%d",
		health.Status, health.NodesReady, health.NodesTotal, health.PodsRunning, health.PodsTotal)

	// Check workloads in configured namespaces
	namespaces := h.config.Namespaces
	if len(namespaces) == 0 {
		// If no namespaces specified, check all
		nsList, err := h.client.ListNamespaces(h.ctx)
		if err != nil {
			log.Printf("Failed to list namespaces: %v", err)
			return
		}
		for _, ns := range nsList {
			namespaces = append(namespaces, ns.Name)
		}
	}

	for _, ns := range namespaces {
		h.checkNamespace(ns)
	}
}

func (h *Healer) checkNamespace(namespace string) {
	workloads, err := h.client.ListWorkloads(h.ctx, namespace)
	if err != nil {
		log.Printf("Failed to list workloads in namespace %s: %v", namespace, err)
		return
	}

	for _, workload := range workloads {
		h.checkWorkload(workload)
	}
}

func (h *Healer) checkWorkload(workload client.Workload) {
	// Skip if workload is in a good state
	if workload.Status == "Running" || workload.Status == "Healthy" {
		return
	}

	key := fmt.Sprintf("%s/%s", workload.Namespace, workload.Name)
	log.Printf("Checking workload %s (status: %s)", key, workload.Status)

	// Get detailed workload info
	detailed, err := h.client.GetWorkload(h.ctx, workload.Name, workload.Namespace)
	if err != nil {
		log.Printf("Failed to get workload details for %s: %v", key, err)
		return
	}

	// Check pods for issues
	pods, err := h.client.ListPods(h.ctx, workload.Name, workload.Namespace)
	if err != nil {
		log.Printf("Failed to list pods for %s: %v", key, err)
		return
	}

	// Analyze workload for issues
	analysis, err := h.client.AnalyzeWorkload(h.ctx, workload.Name, workload.Namespace)
	if err != nil {
		log.Printf("Failed to analyze workload %s: %v", key, err)
		return
	}

	// Determine healing action based on issues
	if h.config.Mode == HealingModeAuto {
		h.performAutoHeal(detailed, pods, analysis)
	} else if h.config.Mode == HealingModeObserve {
		h.logObservation(detailed, pods, analysis)
	}
}

func (h *Healer) performAutoHeal(workload *client.Workload, pods []client.Pod, analysis *client.AnalysisResult) {
	key := fmt.Sprintf("%s/%s", workload.Namespace, workload.Name)

	// Check restart limits
	h.mu.RLock()
	restartCount := h.restartCounts[key]
	lastRestart := h.restartTimes[key]
	h.mu.RUnlock()

	if restartCount >= h.config.MaxRestarts {
		log.Printf("Skipping auto-heal for %s: max restarts (%d) reached", key, h.config.MaxRestarts)
		return
	}

	if time.Since(lastRestart) < h.config.RestartCooldown {
		log.Printf("Skipping auto-heal for %s: cooldown period not elapsed", key)
		return
	}

	// Analyze issues and take action
	for _, issue := range analysis.Issues {
		log.Printf("Issue detected for %s: %s", key, issue)

		action := HealingAction{
			Type:       "restart",
			Workload:   workload.Name,
			Namespace:  workload.Namespace,
			Reason:     issue,
			ExecutedAt: time.Now(),
			Metadata:   make(map[string]interface{}),
		}

		// Check for crash loop backoff
		crashLoopDetected := false
		for _, pod := range pods {
			if pod.RestartCount > 5 {
				crashLoopDetected = true
				action.Metadata["crash_loop_pod"] = pod.Name
				action.Metadata["restart_count"] = pod.RestartCount
				break
			}
		}

		if crashLoopDetected {
			// For crash loops, scale up instead of restart
			if h.config.AutoScaleEnabled && workload.Replicas < h.config.MaxReplicas {
				action.Type = "scale_up"
				action.Metadata["old_replicas"] = workload.Replicas
				action.Metadata["new_replicas"] = workload.Replicas + 1
				h.scaleWorkload(workload, workload.Replicas+1)
			} else {
				log.Printf("Cannot auto-heal crash loop for %s: auto-scale disabled or max replicas reached", key)
				action.Success = false
				h.recordAction(action)
				continue
			}
		} else {
			// Standard restart
			err := h.client.RestartWorkload(h.ctx, workload.Name, workload.Namespace)
			action.Success = (err == nil)
			if err != nil {
				log.Printf("Failed to restart workload %s: %v", key, err)
			} else {
				log.Printf("Successfully restarted workload %s", key)
			}
		}

		h.recordAction(action)

		// Update restart tracking
		h.mu.Lock()
		h.restartCounts[key]++
		h.restartTimes[key] = time.Now()
		h.mu.Unlock()

		break // Only take one action per check cycle
	}
}

func (h *Healer) scaleWorkload(workload *client.Workload, newReplicas int) {
	key := fmt.Sprintf("%s/%s", workload.Namespace, workload.Name)
	log.Printf("Scaling workload %s from %d to %d replicas", key, workload.Replicas, newReplicas)

	// This would call the API to scale the workload
	// For now, we'll log it
	// TODO: Implement scale API call when available in client
}

func (h *Healer) logObservation(workload *client.Workload, pods []client.Pod, analysis *client.AnalysisResult) {
	key := fmt.Sprintf("%s/%s", workload.Namespace, workload.Name)

	action := HealingAction{
		Type:       "observe",
		Workload:   workload.Name,
		Namespace:  workload.Namespace,
		Reason:     "Issue detected in observe mode",
		ExecutedAt: time.Now(),
		Success:    true,
		Metadata: map[string]interface{}{
			"status":    workload.Status,
			"issues":    analysis.Issues,
			"pod_count": len(pods),
		},
	}

	h.recordAction(action)
	log.Printf("[OBSERVE] Workload %s has issues: %v", key, analysis.Issues)
}

func (h *Healer) recordAction(action HealingAction) {
	h.historyMu.Lock()
	h.actionHistory = append(h.actionHistory, action)
	// Keep only last 1000 actions
	if len(h.actionHistory) > 1000 {
		h.actionHistory = h.actionHistory[len(h.actionHistory)-1000:]
	}
	h.historyMu.Unlock()

	// Log to audit
	h.auditLogger.Log("healer", "auto_heal", map[string]interface{}{
		"type":      action.Type,
		"workload":  action.Workload,
		"namespace": action.Namespace,
		"reason":    action.Reason,
		"success":   action.Success,
		"metadata":  action.Metadata,
	}, func() string {
		if action.Success {
			return "success"
		}
		return "error"
	}(), "", 0)
}

func (h *Healer) GetActionHistory() []HealingAction {
	h.historyMu.RLock()
	defer h.historyMu.RUnlock()
	return h.actionHistory
}

func (h *Healer) GetStatus() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return map[string]interface{}{
		"enabled":        h.config.Enabled,
		"mode":           string(h.config.Mode),
		"restart_counts": h.restartCounts,
		"actions_taken":  len(h.actionHistory),
	}
}

func (h *Healer) ResetRestartCount(workload, namespace string) {
	key := fmt.Sprintf("%s/%s", namespace, workload)
	h.mu.Lock()
	delete(h.restartCounts, key)
	delete(h.restartTimes, key)
	h.mu.Unlock()
}
