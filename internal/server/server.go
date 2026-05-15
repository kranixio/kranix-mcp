package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/kranix-io/kranix-mcp/internal/tools"
)

type Server struct {
	transport string
	port      int
	registry  *tools.Registry
}

type MCPRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      interface{}            `json:"id"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

func New(transport string, port int, registry *tools.Registry) *Server {
	return &Server{
		transport: transport,
		port:      port,
		registry:  registry,
	}
}

func (s *Server) Start(ctx context.Context) error {
	switch s.transport {
	case "stdio":
		return s.runStdio(ctx)
	case "http":
		return s.runHTTP(ctx)
	default:
		return fmt.Errorf("unknown transport: %s", s.transport)
	}
}

func (s *Server) runStdio(ctx context.Context) error {
	log.Println("Starting MCP server in stdio mode")

	decoder := json.NewDecoder(io.Reader(os.Stdin))
	encoder := json.NewEncoder(io.Writer(os.Stdout))

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			var req MCPRequest
			if err := decoder.Decode(&req); err != nil {
				if err == io.EOF {
					return nil
				}
				log.Printf("Decode error: %v", err)
				continue
			}

			resp := s.handleRequest(req)
			if err := encoder.Encode(resp); err != nil {
				log.Printf("Encode error: %v", err)
			}
		}
	}
}

func (s *Server) runHTTP(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("Starting MCP server in HTTP mode on %s", addr)

	mux := http.NewServeMux()
	mux.HandleFunc("/sse", s.handleSSE)
	mux.HandleFunc("/", s.handleHTTP)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		log.Println("Shutting down HTTP server")
		server.Shutdown(context.Background())
	}()

	return server.ListenAndServe()
}

func (s *Server) handleRequest(req MCPRequest) MCPResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleListTools(req)
	case "tools/call":
		return s.handleToolCall(req)
	default:
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32601,
				Message: "Method not found",
			},
		}
	}
}

func (s *Server) handleInitialize(req MCPRequest) MCPResponse {
	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "kranix-mcp",
				"version": "0.1.0",
			},
		},
	}
}

func (s *Server) handleListTools(req MCPRequest) MCPResponse {
	toolList := s.registry.ListTools()
	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": toolList,
		},
	}
}

func (s *Server) handleToolCall(req MCPRequest) MCPResponse {
	params, ok := req.Params["arguments"].(map[string]interface{})
	if !ok {
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32602,
				Message: "Invalid params",
			},
		}
	}

	toolName, _ := req.Params["name"].(string)
	result, err := s.registry.CallTool(context.Background(), toolName, params)
	if err != nil {
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32603,
				Message: err.Error(),
			},
		}
	}

	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": result,
				},
			},
		},
	}
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := s.handleRequest(req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Send initial message
	fmt.Fprintf(w, "event: initialized\ndata: {\"status\":\"ready\"}\n\n")
	flusher.Flush()

	// Keep connection alive
	<-r.Context().Done()
}
