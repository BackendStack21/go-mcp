# go-mcp

Zero-dependency [Model Context Protocol](https://modelcontextprotocol.io) server framework for Go. Build MCP-compatible **tools**, **resources**, and **prompts** with stdio transport — tiny binary, fast startup, Go-native.

```bash
go get github.com/BackendStack21/go-mcp
```

## Quick Start

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

	srv.Run() // blocks, reading JSON-RPC from stdin
}
```

Configure in your MCP client:

```json
{
  "mcpServers": {
    "greeter": {
      "command": "greeter"
    }
  }
}
```

## API

### `gomcp.NewServer(name, version string) *Server`

Creates a new MCP server. `name` and `version` are reported to the client during the `initialize` handshake.

### Tools

```go
srv.AddTool(gomcp.Tool{
    Name:        "my-tool",
    Description: "What this tool does",
    InputSchema: gomcp.InputSchema{
        Type:       "object",
        Properties: map[string]gomcp.Property{
            "arg": {Type: "string", Description: "An argument"},
        },
        Required: []string{"arg"},
    },
    Handler: func(ctx context.Context, args map[string]any) (string, error) {
        return "result: " + args["arg"].(string), nil
    },
})
```

| Field | Type | Description |
|-------|------|-------------|
| `Name` | `string` | Unique tool identifier |
| `Description` | `string` | Human-readable description |
| `InputSchema` | `InputSchema` | JSON Schema for arguments |
| `Handler` | `ToolHandler` | `func(ctx, args) (string, error)` |

### Resources

```go
srv.AddResource(gomcp.Resource{
    URI:         "file:///config.yaml",
    Name:        "Config",
    Description: "Application configuration",
    MimeType:    "text/yaml",
    Handler: func(ctx context.Context) (string, error) {
        return "debug: true\n", nil
    },
})
```

### Prompts

```go
srv.AddPrompt(gomcp.Prompt{
    Name:        "code-review",
    Description: "Code review prompt template",
    Arguments: []gomcp.PromptArg{
        {Name: "language", Description: "Programming language", Required: true},
    },
    Handler: func(ctx context.Context, args map[string]any) ([]gomcp.PromptMessage, error) {
        return []gomcp.PromptMessage{
            {Role: "user", Content: gomcp.NewTextContent(
                fmt.Sprintf("Review this %s code for bugs and style issues.", args["language"]),
            )},
        }, nil
    },
})
```

### `srv.Run() error`

Starts the server, reading JSON-RPC 2.0 from stdin and writing responses to stdout. Blocks until stdin closes.

## Protocol Support

| Method | Status |
|--------|--------|
| `initialize` | ✅ |
| `notifications/initialized` | ✅ (consumed silently) |
| `tools/list` | ✅ |
| `tools/call` | ✅ |
| `resources/list` | ✅ |
| `resources/read` | ✅ |
| `prompts/list` | ✅ |
| `prompts/get` | ✅ |

## Transport

- **stdio** — newline-delimited JSON-RPC 2.0 (production)
- HTTP/SSE — planned

## Dependencies

**Zero.** Go stdlib only: `encoding/json`, `bufio`, `context`, `fmt`, `io`, `os`.

## Why Go?

- **Single binary.** Compile, ship, run. No runtime, no node_modules.
- **Fast startup.** Sub-millisecond cold start — critical for MCP servers spawned per-session.
- **Low memory.** < 5 MB RSS for a full MCP server.
- **Go-native.** Idiomatic interfaces, context propagation, zero-cost JSON serialization.

## Example

Full example with tools, resources, and prompts: [`examples/greet/main.go`](examples/greet/main.go)

## License

MIT
