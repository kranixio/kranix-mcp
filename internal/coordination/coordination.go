package coordination

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Task represents a task that can be delegated between agents
type Task struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	AssignedTo  string                 `json:"assigned_to"`   // Agent ID
	CreatedBy   string                 `json:"created_by"`    // Agent ID who created the task
	Status      string                 `json:"status"`        // pending, in_progress, completed, failed
	Priority    string                 `json:"priority"`      // low, medium, high, critical
	Inputs      map[string]interface{} `json:"inputs"`
	Outputs     map[string]interface{} `json:"outputs"`
	SubTasks    []string               `json:"sub_tasks"`    // IDs of sub-tasks
	ParentTask  string                 `json:"parent_task"`  // ID of parent task if any
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Error       string                 `json:"error,omitempty"`
}

// Coordinator manages multi-agent task coordination
type Coordinator struct {
	tasks    map[string]*Task
	mu       sync.RWMutex
	taskChan chan *Task
}

// Config for the coordinator
type Config struct {
	MaxConcurrentTasks int           `yaml:"max_concurrent_tasks"`
	TaskTimeout        time.Duration `yaml:"task_timeout"`
}

func New(config *Config) *Coordinator {
	if config == nil {
		config = &Config{
			MaxConcurrentTasks: 10,
			TaskTimeout:        30 * time.Minute,
		}
	}
	return &Coordinator{
		tasks:    make(map[string]*Task),
		taskChan: make(chan *Task, 100),
	}
}

// CreateTask creates a new task
func (c *Coordinator) CreateTask(ctx context.Context, task *Task) (*Task, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if task.ID == "" {
		task.ID = generateTaskID()
	}
	if task.Status == "" {
		task.Status = "pending"
	}
	if task.Priority == "" {
		task.Priority = "medium"
	}
	task.CreatedAt = time.Now()
	task.UpdatedAt = time.Now()

	c.tasks[task.ID] = task
	return task, nil
}

// DelegateTask delegates a task to another agent
func (c *Coordinator) DelegateTask(ctx context.Context, taskID, targetAgentID string) (*Task, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	task, ok := c.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}

	task.AssignedTo = targetAgentID
	task.Status = "pending"
	task.UpdatedAt = time.Now()

	return task, nil
}

// GetTask retrieves a task by ID
func (c *Coordinator) GetTask(taskID string) (*Task, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	task, ok := c.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}
	return task, nil
}

// ListTasks lists all tasks, optionally filtered by agent or status
func (c *Coordinator) ListTasks(agentID, status string) ([]*Task, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*Task
	for _, task := range c.tasks {
		if agentID != "" && task.AssignedTo != agentID {
			continue
		}
		if status != "" && task.Status != status {
			continue
		}
		result = append(result, task)
	}
	return result, nil
}

// UpdateTaskStatus updates the status of a task
func (c *Coordinator) UpdateTaskStatus(ctx context.Context, taskID, status string, outputs map[string]interface{}) (*Task, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	task, ok := c.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}

	task.Status = status
	task.UpdatedAt = time.Now()
	if outputs != nil {
		task.Outputs = outputs
	}

	if status == "completed" {
		now := time.Now()
		task.CompletedAt = &now
	} else if status == "failed" {
		task.Error = fmt.Sprintf("Task failed at %s", time.Now().Format(time.RFC3339))
	}

	return task, nil
}

// CreateSubTask creates a sub-task for a parent task
func (c *Coordinator) CreateSubTask(ctx context.Context, parentTaskID string, subTask *Task) (*Task, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	parent, ok := c.tasks[parentTaskID]
	if !ok {
		return nil, fmt.Errorf("parent task not found: %s", parentTaskID)
	}

	if subTask.ID == "" {
		subTask.ID = generateTaskID()
	}
	subTask.ParentTask = parentTaskID
	subTask.Status = "pending"
	subTask.CreatedAt = time.Now()
	subTask.UpdatedAt = time.Now()

	c.tasks[subTask.ID] = subTask
	parent.SubTasks = append(parent.SubTasks, subTask.ID)
	parent.UpdatedAt = time.Now()

	return subTask, nil
}

// GetTaskHierarchy returns the full hierarchy of a task including sub-tasks
func (c *Coordinator) GetTaskHierarchy(taskID string) (map[string]*Task, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	hierarchy := make(map[string]*Task)
	c.buildHierarchy(taskID, hierarchy)
	return hierarchy, nil
}

func (c *Coordinator) buildHierarchy(taskID string, hierarchy map[string]*Task) {
	task, ok := c.tasks[taskID]
	if !ok {
		return
	}
	hierarchy[taskID] = task
	for _, subTaskID := range task.SubTasks {
		c.buildHierarchy(subTaskID, hierarchy)
	}
}

// ClaimTask allows an agent to claim a pending task
func (c *Coordinator) ClaimTask(ctx context.Context, taskID, agentID string) (*Task, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	task, ok := c.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}

	if task.Status != "pending" {
		return nil, fmt.Errorf("task is not in pending state: %s", task.Status)
	}

	task.AssignedTo = agentID
	task.Status = "in_progress"
	task.UpdatedAt = time.Now()

	return task, nil
}

func generateTaskID() string {
	return fmt.Sprintf("task-%d", time.Now().UnixNano())
}

// ToJSON converts a task to JSON
func (t *Task) ToJSON() (string, error) {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
