# kranix-mcp

> MCP server — expose Kranix to Claude, GPT, and any MCP-compatible AI agent.

`kranix-mcp` is a [Model Context Protocol (MCP)](https://modelcontextprotocol.io) server that makes the full Kranix platform accessible to AI agents. It translates MCP tool calls into `kranix-api` requests, allowing agents like Claude or GPT to deploy workloads, inspect cluster state, stream logs, and analyze failures — all within safe, audited boundaries.

This is the AI-native layer of the Kranix ecosystem and its primary differentiator from conventional Kubernetes tooling.

---

## What it does

- Runs an MCP-compatible server (stdio or HTTP/SSE transport)
- Exposes a curated set of Kranix operations as MCP tools
- Translates agent tool calls into authenticated `kranix-api` requests
- Enforces safety boundaries — agents can observe and act, but cannot delete namespaces or modify cluster RBAC
- Emits structured audit logs for every agent action

---

## Architecture position

```
AI agents (Claude, GPT, custom)
           │
     MCP protocol
           │
       kranix-mcp
           │
       kranix-api  ──►  kranix-core
```

`kranix-mcp` has no direct knowledge of Docker or Kubernetes. It only speaks to `kranix-api`.

---

## Exposed MCP tools

These are the tools AI agents can call:

| Tool | Description |
|---|---|
| `deploy_workload` | Deploy an application from an image or manifest |
| `list_workloads` | List all running workloads in a namespace |
| `get_workload` | Get the spec and status of a workload |
| `restart_workload` | Restart a workload |
| `delete_workload` | Remove a workload (requires explicit confirmation flag) |
| `list_pods` | List pods for a given workload |
| `stream_logs` | Stream logs from a pod (returns last N lines) |
| `create_namespace` | Create a new namespace |
| `list_namespaces` | List all available namespaces |
| `analyze_workload` | Run AI-powered failure analysis on a workload |
| `generate_manifest` | Generate a Kubernetes manifest from a plain-text description |
| `get_cluster_health` | Summary of overall cluster health |

### Tool schema example

```json
{
  "name": "deploy_workload",
  "description": "Deploy an application workload to a namespace",
  "inputSchema": {
    "type": "object",
    "required": ["name", "image", "namespace"],
    "properties": {
      "name":      { "type": "string", "description": "Workload name" },
      "image":     { "type": "string", "description": "Container image (e.g. nginx:latest)" },
      "namespace": { "type": "string", "description": "Target namespace" },
      "replicas":  { "type": "integer", "default": 1 },
      "env":       { "type": "object", "description": "Environment variables" }
    }
  }
}
```

---

## Safety boundaries

Not everything is exposed. By design, agents **cannot**:

- Delete or modify namespaces (read-only)
- Modify cluster RBAC or service accounts
- Exec into running containers
- Access secrets directly
- Perform bulk deletes

All agent actions are logged with the agent identity, tool name, inputs, and outcome.

---

## Project structure

```
kranix-mcp/
├── cmd/
│   └── mcp/              # Entry point
├── internal/
│   ├── server/           # MCP server setup (stdio + HTTP/SSE)
│   ├── tools/            # One file per MCP tool implementation
│   ├── client/           # kranix-api HTTP client wrapper
│   ├── audit/            # Audit log sink
│   └── safety/           # Tool permission/safety policy
├── schemas/              # JSON schemas for all tool inputs
├── config/               # Default config files
└── tests/
    ├── unit/
    └── integration/
```

---

## Getting started

### Prerequisites

- Node.js 20+ or Go 1.22+ (depending on your build target)
- A running `kranix-api` instance
- An MCP-compatible AI client (Claude Desktop, Claude API, or any MCP host)

### Run locally (stdio transport)

```bash
git clone https://github.com/kranix-io/kranix-mcp
cd kranix-mcp
npm install        # or: go mod download

KRANE_API_URL=http://localhost:8080 \
KRANE_API_KEY=krane_your_key_here \
npm start
```

### Run as HTTP/SSE server

```bash
KRANE_MCP_TRANSPORT=http \
KRANE_MCP_PORT=3100 \
npm start
```

---

## Connecting to Claude

### Claude Desktop (`claude_desktop_config.json`)

```json
{
  "mcpServers": {
    "kranix": {
      "command": "node",
      "args": ["/path/to/kranix-mcp/dist/index.js"],
      "env": {
        "KRANE_API_URL": "http://localhost:8080",
        "KRANE_API_KEY": "krane_your_key_here"
      }
    }
  }
}
```

### Claude API (HTTP/SSE)

```python
import anthropic

client = anthropic.Anthropic()

response = client.messages.create(
    model="claude-opus-4-5",
    max_tokens=1024,
    tools=[],   # tools are discovered from the MCP server
    mcp_servers=[
        {
            "type": "url",
            "url": "http://localhost:3100/sse",
            "name": "kranix"
        }
    ],
    messages=[
        {"role": "user", "content": "Deploy nginx:latest to the staging namespace"}
    ]
)
```

---

## Configuration

```yaml
mcp:
  transport: stdio          # stdio | http
  port: 3100                # only used if transport: http

krane_api:
  url: "http://localhost:8080"
  api_key: ""               # or set KRANE_API_KEY env var
  timeout: 30s

safety:
  allow_delete_workload: true
  allow_create_namespace: true
  readonly_mode: false       # set true to allow only read tools

audit:
  enabled: true
  sink: stdout
```

---

## Connectivity

| Repo | Relationship |
|---|---|
| `kranix-api` | All tool calls translate into kranix-api HTTP requests |
| `kranix-packages` | Imports shared types and API client |
| AI agents | Consumed via MCP protocol (stdio or HTTP/SSE) |

---

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md). Adding a new tool requires: tool definition, JSON schema, implementation, safety classification, and integration test with a mock `kranix-api`.

## License

Apache 2.0 — see [LICENSE](./LICENSE).
