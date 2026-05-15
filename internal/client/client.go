package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type Workload struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Image     string            `json:"image"`
	Replicas  int               `json:"replicas"`
	Env       map[string]string `json:"env,omitempty"`
	Spec      json.RawMessage   `json:"spec,omitempty"`
	Status    string            `json:"status,omitempty"`
	CreatedAt string            `json:"created_at,omitempty"`
}

type Pod struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Workload   string `json:"workload"`
	Status     string `json:"status"`
	Node       string `json:"node,omitempty"`
	RestartCount int  `json:"restart_count,omitempty"`
}

type Namespace struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at,omitempty"`
}

type ClusterHealth struct {
	Status          string `json:"status"`
	NodesReady      int    `json:"nodes_ready"`
	NodesTotal      int    `json:"nodes_total"`
	PodsRunning     int    `json:"pods_running"`
	PodsTotal       int    `json:"pods_total"`
	LastChecked     string `json:"last_checked"`
}

type DeploymentRequest struct {
	Name      string            `json:"name"`
	Image     string            `json:"image"`
	Namespace string            `json:"namespace"`
	Replicas  int               `json:"replicas,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Manifest  string            `json:"manifest,omitempty"`
}

type AnalysisResult struct {
	Workload   string   `json:"workload"`
	Namespace  string   `json:"namespace"`
	Status     string   `json:"status"`
	Issues     []string `json:"issues"`
	Suggestions []string `json:"suggestions"`
	AnalyzedAt string   `json:"analyzed_at"`
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}

	return nil
}

// Workload operations
func (c *Client) DeployWorkload(ctx context.Context, req DeploymentRequest) (*Workload, error) {
	var workload Workload
	body, _ := json.Marshal(req)
	err := c.doRequest(ctx, "POST", "/api/v1/workloads", bytes.NewReader(body), &workload)
	return &workload, err
}

func (c *Client) ListWorkloads(ctx context.Context, namespace string) ([]Workload, error) {
	var workloads []Workload
	path := "/api/v1/workloads"
	if namespace != "" {
		path += "?namespace=" + namespace
	}
	err := c.doRequest(ctx, "GET", path, nil, &workloads)
	return workloads, err
}

func (c *Client) GetWorkload(ctx context.Context, name, namespace string) (*Workload, error) {
	var workload Workload
	err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/v1/workloads/%s?namespace=%s", name, namespace), nil, &workload)
	return &workload, err
}

func (c *Client) RestartWorkload(ctx context.Context, name, namespace string) error {
	return c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/workloads/%s/restart?namespace=%s", name, namespace), nil, nil)
}

func (c *Client) DeleteWorkload(ctx context.Context, name, namespace string) error {
	return c.doRequest(ctx, "DELETE", fmt.Sprintf("/api/v1/workloads/%s?namespace=%s", name, namespace), nil, nil)
}

// Pod operations
func (c *Client) ListPods(ctx context.Context, workloadName, namespace string) ([]Pod, error) {
	var pods []Pod
	err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/v1/pods?workload=%s&namespace=%s", workloadName, namespace), nil, &pods)
	return pods, err
}

func (c *Client) StreamLogs(ctx context.Context, podName, namespace string, tailLines int) (string, error) {
	var result struct {
		Logs string `json:"logs"`
	}
	err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/v1/pods/%s/logs?namespace=%s&tail=%d", podName, namespace, tailLines), nil, &result)
	return result.Logs, err
}

// Namespace operations
func (c *Client) CreateNamespace(ctx context.Context, name string) (*Namespace, error) {
	var ns Namespace
	body, _ := json.Marshal(map[string]string{"name": name})
	err := c.doRequest(ctx, "POST", "/api/v1/namespaces", bytes.NewReader(body), &ns)
	return &ns, err
}

func (c *Client) ListNamespaces(ctx context.Context) ([]Namespace, error) {
	var namespaces []Namespace
	err := c.doRequest(ctx, "GET", "/api/v1/namespaces", nil, &namespaces)
	return namespaces, err
}

// Analysis operations
func (c *Client) AnalyzeWorkload(ctx context.Context, name, namespace string) (*AnalysisResult, error) {
	var result AnalysisResult
	err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/workloads/%s/analyze?namespace=%s", name, namespace), nil, &result)
	return &result, err
}

func (c *Client) GenerateManifest(ctx context.Context, description string) (string, error) {
	var result struct {
		Manifest string `json:"manifest"`
	}
	body, _ := json.Marshal(map[string]string{"description": description})
	err := c.doRequest(ctx, "POST", "/api/v1/manifests/generate", bytes.NewReader(body), &result)
	return result.Manifest, err
}

// Cluster operations
func (c *Client) GetClusterHealth(ctx context.Context) (*ClusterHealth, error) {
	var health ClusterHealth
	err := c.doRequest(ctx, "GET", "/api/v1/cluster/health", nil, &health)
	return &health, err
}
