# go-mcp

Zero-dependency [Model Context Protocol](https://modelcontextprotocol.io) server framework for Go. Expose your Go code as **tools**, **resources**, and **prompts** that AI agents can call. Stdio transport. Single binary. No runtime.

```bash
go get github.com/BackendStack21/go-mcp
```

---

## What can you build with this?

An MCP server turns any Go program into an AI-accessible service. The AI client (Claude Desktop, Cursor, Cody) discovers your tools, calls them, and reads your resources — all over stdin/stdout JSON-RPC.

### Your first MCP server — 30 seconds

```go
package main

import (
	"context"
	"fmt"
	"github.com/BackendStack21/go-mcp/gomcp"
)

func main() {
	srv := gomcp.NewServer("greeter", "1.0.0")

	srv.AddTool(gomcp.Tool{
		Name:        "greet",
		Description: "Greet a person by name",
		InputSchema: gomcp.InputSchema{
			Type:       "object",
			Properties: map[string]gomcp.Property{
				"name": {Type: "string", Description: "The name to greet"},
			},
			Required: []string{"name"},
		},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("Hello, %s!", args["name"]), nil
		},
	})

	srv.Run() // blocks on stdin
}
```

```json
// ~/.claude/claude_desktop_config.json
{
  "mcpServers": {
    "greeter": { "command": "greeter" }
  }
}
```

---

## Use Cases

### Database Explorer — give AI safe, read-only SQL access

Let an AI agent explore your schema, run queries, and analyze data — without giving it raw database credentials.

```go
srv.AddTool(gomcp.Tool{
	Name:        "db_query",
	Description: "Run a read-only SQL query against the database",
	InputSchema: gomcp.InputSchema{
		Type: "object",
		Properties: map[string]gomcp.Property{
			"query": {Type: "string", Description: "SELECT query to execute"},
		},
		Required: []string{"query"},
	},
	Handler: func(ctx context.Context, args map[string]any) (string, error) {
		query := args["query"].(string)
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "SELECT") {
			return "", fmt.Errorf("only SELECT queries allowed")
		}
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return "", err
		}
		defer rows.Close()
		return formatAsTable(rows)
	},
})

srv.AddResource(gomcp.Resource{
	URI:      "db://schema",
	Name:     "Database Schema",
	MimeType: "text/plain",
	Handler: func(ctx context.Context) (string, error) {
		return getDBSchema(), nil
	},
})
```

The AI can now: "How many users signed up this week?" → calls `db_query`. "Show me the table structure" → reads `db://schema`.

### Kubernetes Operations — kubectl as an MCP server

Expose your cluster to AI for diagnostics and operations.

```go
srv.AddTool(gomcp.Tool{
	Name:        "kubectl",
	Description: "Run a kubectl command and return the output",
	InputSchema: gomcp.InputSchema{
		Type: "object",
		Properties: map[string]gomcp.Property{
			"args": {Type: "array", Description: "kubectl arguments (e.g. ['get', 'pods', '-n', 'default'])"},
		},
		Required: []string{"args"},
	},
	Handler: func(ctx context.Context, args map[string]any) (string, error) {
		rawArgs := args["args"].([]any)
		cmdArgs := make([]string, len(rawArgs))
		for i, a := range rawArgs { cmdArgs[i] = a.(string) }
		out, err := exec.CommandContext(ctx, "kubectl", cmdArgs...).Output()
		return string(out), err
	},
})
```

Now: "Why is the payments service failing?" → `kubectl describe deployment payments`, `kubectl logs payments-abc123`.

### System Monitor — expose your server's vitals as resources

```go
srv.AddResource(gomcp.Resource{
	URI:      "system://cpu",
	Name:     "CPU Usage",
	MimeType: "application/json",
	Handler: func(ctx context.Context) (string, error) {
		info := map[string]any{
			"cores":   runtime.NumCPU(),
			"percent": getCPUPercent(),
		}
		data, _ := json.Marshal(info)
		return string(data), nil
	},
})

srv.AddResource(gomcp.Resource{
	URI:      "system://memory",
	Name:     "Memory Usage",
	MimeType: "application/json",
	Handler: func(ctx context.Context) (string, error) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return fmt.Sprintf(`{"alloc_mb": %d, "total_mb": %d}`, m.Alloc/1e6, m.Sys/1e6), nil
	},
})
```

### API Integration — bridge any REST API to AI

```go
srv.AddTool(gomcp.Tool{
	Name:        "github_user",
	Description: "Get GitHub user profile information",
	InputSchema: gomcp.InputSchema{
		Type: "object",
		Properties: map[string]gomcp.Property{
			"username": {Type: "string", Description: "GitHub username"},
		},
		Required: []string{"username"},
	},
	Handler: func(ctx context.Context, args map[string]any) (string, error) {
		resp, err := http.Get(fmt.Sprintf("https://api.github.com/users/%s", args["username"]))
		if err != nil { return "", err }
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return string(body), nil
	},
})
```

### Filesystem Navigator — give AI safe, scoped file access

```go
srv.AddTool(gomcp.Tool{
	Name:        "read_file",
	Description: "Read a file from the project directory",
	InputSchema: gomcp.InputSchema{
		Type: "object",
		Properties: map[string]gomcp.Property{
			"path": {Type: "string", Description: "Relative path within the project"},
		},
		Required: []string{"path"},
	},
	Handler: func(ctx context.Context, args map[string]any) (string, error) {
		path := args["path"].(string)
		// Prevent path traversal
		if strings.Contains(path, "..") {
			return "", fmt.Errorf("path traversal not allowed")
		}
		data, err := os.ReadFile(filepath.Join("/var/project", path))
		return string(data), err
	},
})
```

### Multi-tool Server — combine everything

```go
func main() {
	srv := gomcp.NewServer("devops-assistant", "1.0.0")

	// Tools: actions the AI can take
	srv.AddTool(deployTool())
	srv.AddTool(restartServiceTool())
	srv.AddTool(runTestsTool())

	// Resources: data the AI can read
	srv.AddResource(deploymentStatusResource())
	srv.AddResource(errorLogsResource())
	srv.AddResource(metricsResource())

	// Prompts: templates for common tasks
	srv.AddPrompt(incidentResponsePrompt())
	srv.AddPrompt(codeReviewPrompt())

	srv.Run()
}
```

The AI now has a full DevOps toolkit — deploy, debug, monitor, and respond to incidents. All through a single binary.

---

## API Reference

### `gomcp.NewServer(name, version string) *Server`

Creates a new MCP server. `name` and `version` are reported to the client during the `initialize` handshake.

### `srv.AddTool(tool Tool)`

Register a callable tool. The AI sees the `Name`, `Description`, and `InputSchema`. When called, the `Handler` runs.

```go
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema InputSchema `json:"inputSchema"`
	Handler     ToolHandler `json:"-"`
}

type ToolHandler func(ctx context.Context, args map[string]any) (string, error)
```

### `srv.AddResource(res Resource)`

Register a readable resource identified by URI. The AI can list available resources and read them on demand.

```go
type Resource struct {
	URI         string          `json:"uri"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	MimeType    string          `json:"mimeType,omitempty"`
	Handler     ResourceHandler `json:"-"`
}

type ResourceHandler func(ctx context.Context) (string, error)
```

### `srv.AddPrompt(prompt Prompt)`

Register a prompt template. The AI requests a prompt by name with arguments, and gets back formatted messages.

```go
type Prompt struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Arguments   []PromptArg   `json:"arguments,omitempty"`
	Handler     PromptHandler `json:"-"`
}

type PromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type PromptHandler func(ctx context.Context, args map[string]any) ([]PromptMessage, error)

type PromptMessage struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content any    `json:"content"` // string or content block
}

// Helper:
func NewTextContent(text string) map[string]any
```

### `srv.Run() error`

Starts the server loop. Reads JSON-RPC 2.0 from `os.Stdin`, writes responses to `os.Stdout`. Blocks until stdin closes (EOF).

---

## Protocol Support

| Method | Description |
|--------|-------------|
| `initialize` | MCP handshake — reports server name, version, capabilities |
| `notifications/initialized` | Consumed silently |
| `tools/list` | Returns metadata for all registered tools |
| `tools/call` | Dispatches a call to the matching tool handler |
| `resources/list` | Returns metadata for all registered resources |
| `resources/read` | Reads a resource by URI |
| `prompts/list` | Returns metadata for all registered prompts |
| `prompts/get` | Builds a prompt from arguments |

All eight methods implemented. **No partial support.**

---

## Why Go?

- **Single binary** — compile, ship, run. No runtime, no `node_modules`.
- **Cold start < 1ms** — critical for MCP servers spawned per-session by AI clients.
- **~5 MB RSS** — a full MCP server with tools, resources, and prompts runs in under 5 MB.
- **Zero dependencies** — `encoding/json`, `context`, `io`, `os`. That's it.
- **Go's concurrency** — run blocking operations (DB queries, HTTP calls, subprocesses) without blocking the event loop.

---

## Examples

Every example is a working MCP server you can compile and connect to Claude Desktop:

| Example | Description | File |
|---------|-------------|------|
| Greeter | Hello World: tool + resource + prompt | [`examples/greet/main.go`](examples/greet/main.go) |
| DB Explorer | Safe read-only PostgreSQL access | [`examples/db-explorer/main.go`](examples/db-explorer/main.go) |
| System Monitor | Expose CPU, memory, disk as resources | [`examples/sys-monitor/main.go`](examples/sys-monitor/main.go) |
| FS Navigator | Scoped filesystem read/write | [`examples/fs-navigator/main.go`](examples/fs-navigator/main.go) |

```bash
# Build and test any example
cd examples/greet
go build -o greeter .
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | ./greeter
```

---

## License

MIT
