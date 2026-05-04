# AGENTS.md

> This file is for AI coding agents. Read it before working on this repository.

## Project Identity

**go-mcp** is a zero-dependency [Model Context Protocol](https://modelcontextprotocol.io) server framework for Go. It lets you build MCP-compatible servers that expose tools, resources, and prompts to AI clients (Claude Desktop, Cursor, etc.) — as a single Go binary with zero external dependencies.

## Architecture

```
stdio (os.Stdin/os.Stdout)
    │
    ▼
json.Decoder / json.Encoder  ← newline-delimited JSON-RPC 2.0
    │
    ▼
Server.runWithIO()  ← dispatch loop
    ├── "initialize"         → handshake
    ├── "tools/list"         → registered tools metadata
    ├── "tools/call"         → dispatch to Tool.Handler
    ├── "resources/list"     → registered resources metadata
    ├── "resources/read"     → dispatch to Resource.Handler
    ├── "prompts/list"       → registered prompts metadata
    └── "prompts/get"        → dispatch to Prompt.Handler
```

## Code Map

| File | Purpose |
|------|---------|
| `gomcp/types.go` | Tool, Resource, Prompt, InputSchema, handler signatures |
| `gomcp/jsonrpc.go` | JSON-RPC 2.0 request/response/error types |
| `gomcp/server.go` | Server struct, Run(), all JSON-RPC method handlers |
| `gomcp/server_test.go` | Unit + integration tests (pipe-based) |
| `gomcp/e2e_test.go` | Subprocess E2E test |
| `examples/greet/main.go` | Canonical example MCP server |

## Conventions

- **Zero dependencies.** Only Go stdlib — `encoding/json`, `bufio`, `context`, `fmt`, `io`, `os`. Never add a third-party import to go.mod.
- **Interfaces over structs.** Handler signatures use `context.Context` + maps for extensibility. Future: typed generics.
- **Tests use pipes.** Integration tests simulate stdio with `io.Pipe()`. E2E tests spawn a real subprocess via `os/exec`.
- **Error codes follow JSON-RPC 2.0.** `-32601` = method not found, `-32602` = invalid params, `-32000` = application error.
- **Go naming.** Exported types are PascalCase. Unexported internals are camelCase. Test functions are `TestXxx`.
- **Protocol version pinned.** `2024-11-05` hardcoded — update manually when MCP spec revs.

## Testing

```bash
# All tests
go test ./gomcp/ -v

# E2E only (requires Go in PATH)
go test ./gomcp/ -run TestE2E -v
```

## Building an MCP server with this

```go
srv := gomcp.NewServer("my-server", "1.0.0")

srv.AddTool(gomcp.Tool{
    Name:        "echo",
    Description: "Echo back the message",
    InputSchema: gomcp.InputSchema{
        Type: "object",
        Properties: map[string]gomcp.Property{
            "message": {Type: "string"},
        },
    },
    Handler: func(ctx context.Context, args map[string]any) (string, error) {
        return args["message"].(string), nil
    },
})

srv.Run() // blocks on stdio
```

## Philosophy

- **Simple.** ~350 lines of library code. Readable by humans and agents alike.
- **Fast.** Zero-allocation JSON decoding where possible. Tiny binary.
- **Agent-first.** Built for AI agents that need to expose Go-side tools. The AGENTS.md you're reading now is part of the product.
- **Go-native.** Idiomatic Go, not a TypeScript port. Uses Go's strengths: interfaces, zero-deps compilation, single-binary deploy.
