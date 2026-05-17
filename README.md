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
| `natural_language_deploy` | Deploy using natural language (e.g., "deploy api-server v2 to prod with 5 replicas") |
| `get_healing_status` | Get autonomous healing mode status |
| `get_healing_history` | Get history of autonomous healing actions |
| `reset_healing_count` | Reset restart count for a workload |
| **Multi-Agent Coordination** | |
| `create_task` | Create a new task for multi-agent coordination |
| `delegate_task` | Delegate a task to another agent |
| `get_task` | Get details of a specific task |
| `list_tasks` | List all tasks, optionally filtered by agent or status |
| `update_task_status` | Update the status of a task |
| `create_subtask` | Create a sub-task for a parent task |
| `claim_task` | Claim a pending task for the current agent |
| **Dry-Run Mode** | |
| `set_dryrun_mode` | Set the dry-run mode (disabled, preview, log) |
| `get_dryrun_mode` | Get the current dry-run mode |
| `get_dryrun_preview` | Get a preview of actions that would be executed in dry-run mode |
| `clear_dryrun_actions` | Clear all recorded dry-run actions |
| **Incident Response** | |
| `list_runbooks` | List all available incident response runbooks |
| `get_runbook` | Get details of a specific runbook |
| `execute_runbook` | Execute an incident response runbook |
| `get_execution` | Get details of a runbook execution |
| `list_executions` | List all runbook executions, optionally filtered by runbook or status |
| `cancel_execution` | Cancel a running runbook execution |
| `create_runbook` | Create a new incident response runbook |

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

## New Features

### Autonomous Healing Mode

The autonomous healing mode continuously monitors cluster health and automatically fixes issues without being prompted. It can:

- Detect failing workloads and pods
- Automatically restart crashed services
- Scale up workloads experiencing crash loop backoff
- Track restart counts to prevent restart loops
- Operate in three modes: `disabled`, `observe`, or `auto`

**Configuration:**

```yaml
healing:
  enabled: true
  mode: "observe"           # disabled | observe | auto
  check_interval: 30s
  max_restarts_per_hour: 10
  restart_cooldown: 5m
  auto_scale_enabled: true
  min_replicas: 1
  max_replicas: 10
  namespaces: []            # Empty means all namespaces
```

**Tools:**
- `get_healing_status` - Check current healing mode status
- `get_healing_history` - View history of healing actions
- `reset_healing_count` - Reset restart counter after fixing issues

### Natural Language Deploys

Deploy workloads using plain English commands. The NLP parser understands commands like:

- "deploy api-server v2 to prod with 5 replicas"
- "launch nginx:latest to staging with env PORT=8080"
- "scale workload frontend to 10 replicas in production"

**Usage:**

```json
{
  "command": "deploy api-server v2 to prod with 5 replicas",
  "execute": false  // Set to true to actually deploy
}
```

**Tools:**
- `natural_language_deploy` - Parse and execute natural language deployment commands

### Multi-Agent Coordination

The multi-agent coordination system enables agents to delegate sub-tasks to other agents, enabling complex workflows and parallel execution:

**Features:**
- **Task Creation**: Agents can create tasks with priorities, descriptions, and assign them to specific agents
- **Task Delegation**: Reassign tasks between agents as needed
- **Sub-task Support**: Break down complex tasks into hierarchical sub-tasks
- **Task Claiming**: Agents can claim pending tasks from a shared queue
- **Status Tracking**: Real-time task status updates (pending, in_progress, completed, failed)

**Usage Example:**

```json
{
  "title": "Deploy database migration",
  "description": "Run database schema migration for new feature",
  "assigned_to": "database-agent",
  "priority": "high",
  "inputs": {
    "migration_script": "migrations/v2_add_users.sql"
  }
}
```

**Configuration:**

```yaml
coordination:
  enabled: true
  max_concurrent_tasks: 10
  task_timeout: 30m
```

### Dry-Run Mode

Dry-run mode allows agents to preview exactly what would happen before executing actions, providing safety and predictability:

**Modes:**
- **disabled**: Execute actions normally (default)
- **preview**: Record and preview actions without executing them
- **log**: Execute actions while logging all operations for audit

**Features:**
- Predictive output for common operations
- Human-readable preview of planned actions
- Action history and clearing
- Per-tool prediction functions

**Usage:**

```json
{
  "mode": "preview"
}
```

Then execute tools normally and call `get_dryrun_preview` to see what would happen.

**Configuration:**

```yaml
dryrun:
  enabled: true
  mode: "disabled"  # disabled | preview | log
  max_actions: 100
```

### Incident Response Tool

The incident response tool enables agents to read and execute runbooks for automated on-call responses:

**Features:**
- **Runbook Management**: Load runbooks from JSON files in a configured directory
- **Step Execution**: Execute sequential steps with configurable failure handling
- **Context Injection**: Pass runtime context to runbook steps
- **Execution Tracking**: Monitor runbook execution status and results
- **Cancellation Support**: Cancel running runbook executions

**Runbook Structure:**

```json
{
  "name": "Database Service Restart",
  "category": "database",
  "severity": "high",
  "steps": [
    {
      "name": "Check Database Status",
      "tool": "get_workload",
      "inputs": {
        "name": "{{database_name}}",
        "namespace": "{{namespace}}"
      },
      "on_failure": "stop"
    },
    {
      "name": "Restart Database",
      "tool": "restart_workload",
      "inputs": {
        "name": "{{database_name}}",
        "namespace": "{{namespace}}"
      },
      "on_failure": "stop"
    }
  ]
}
```

**Configuration:**

```yaml
incident:
  enabled: true
  runbook_path: "./runbooks"
  auto_load: true
  default_timeout: 5m
```

**Sample Runbooks:**
- `database-restart.json`: Automated database service recovery
- `pod-crashloop-recovery.json`: Handle CrashLoopBackoff scenarios
- `cluster-health-check.json`: Comprehensive cluster diagnostics

### Agent Permission Scopes

Fine-grained access control per agent identity with three scopes:

- **readonly** - Can only read cluster state (list, get, analyze)
- **write** - Can deploy, restart, create namespaces (cannot delete)
- **admin** - Full access including delete operations

**Configuration:**

```yaml
safety:
  default_scope: "write"     # Default scope for unknown agents
  agents:
    claude-desktop:
      scope: "write"
      namespaces: []         # Empty means all namespaces
      allowed_tools: []      # Empty means all tools based on scope
    production-bot:
      scope: "admin"
      namespaces: ["production", "staging"]
    read-only-agent:
      scope: "readonly"
```

**Namespace Isolation:**

Agents can be restricted to specific namespaces. When an agent tries to access a namespace outside its allowed list, the action is denied with a clear error message.

---

## Audit logging

All agent actions are logged with the agent identity, tool name, inputs, and outcome.

---

## Project structure

```
kranix-mcp/
├── cmd/
│   └── mcp/              # Entry point
├── internal/
│   ├── server/           # MCP server (stdio, HTTP/SSE)
│   ├── tools/            # One file per MCP tool
│   └── client/           # kranix-api client
├── config/
│   └── config.yaml
├── test/
│   ├── unit/
│   └── integration/
```

---

## Getting started

### Prerequisites

- Node.js 20+ or Go 1.22+ (depending on your build target)
- A running `kranix-api` instance

---

## Integration with Kranix Ecosystem

### Kranix-Core Integration

The autonomous healing mode integrates with kranix-core's existing features:

- **Drift Detection**: Healing actions are logged as events in kranix-core's event sourcing system
- **Health Gates**: Healing checks respect workload health gate configurations
- **Failure Prediction**: Uses kranix-core's ML-based failure prediction when available

### Kranix-API Integration

Agent permission scopes align with kranix-api's authentication system:

- Agent identities can be synchronized with API keys or JWT tokens
- Permission checks are enforced both at the MCP layer and API layer
- Audit logs are consistent across both systems for complete traceability

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
