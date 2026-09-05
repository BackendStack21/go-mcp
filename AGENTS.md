# AGENTS.md

> This file is for AI coding agents. Read it before working on this repository.

## Project Identity

**go-mcp** is a zero-dependency [Model Context Protocol](https://modelcontextprotocol.io) server framework for Go. It lets you build MCP-compatible servers that expose tools, resources, and prompts to AI clients (Claude Desktop, Cursor, etc.) — as a single Go binary with zero external dependencies.

## Architecture

```
stdio (os.Stdin/os.Stdout)
    │
    ▼
newline-delimited JSON-RPC 2.0  ← one message per line (MCP stdio framing)
    │
    ▼
Server.run()  ← dispatch loop
    ├── unparseable line              → -32700 / -32600, loop keeps serving
    ├── oversized line                → -32600, remainder discarded
    ├── "initialize"                  → legacy handshake (version negotiated)
    ├── "notifications/initialized"   → consumed silently
    ├── "server/discover"             → 2026-07-28 capability probe
    ├── "ping"                        → empty result (legacy clients)
    ├── "tools/list"                  → registered tools metadata
    ├── "tools/call"                  → dispatch to Tool.Handler
    ├── "resources/list"              → registered resources metadata
    ├── "resources/read"              → dispatch to Resource.Handler
    ├── "resources/templates/list"    → empty catalog (spec-compliant)
    ├── "prompts/list"                → registered prompts metadata
    └── "prompts/get"                 → dispatch to Prompt.Handler
```

## Code Map

| File | Purpose |
|------|---------|
| `gomcp/types.go` | Tool, Resource, Prompt, InputSchema, handler signatures |
| `gomcp/jsonrpc.go` | JSON-RPC 2.0 request/response/error types |
| `gomcp/protocol.go` | Protocol versions, `_meta` negotiation, pagination |
| `gomcp/path.go` | `SafeJoin` — path-traversal-safe filesystem helper |
| `gomcp/server.go` | Server struct, Run(), all JSON-RPC method handlers |
| `gomcp/server_test.go` | Unit + integration tests (pipe-based) |
| `gomcp/protocol_test.go` | 2026-07-28 + security tests |
| `gomcp/e2e_test.go` | Subprocess E2E test |
| `examples/greet/main.go` | Canonical example MCP server |

## Conventions

- **Zero dependencies.** Only Go stdlib — `encoding/json`, `bufio`, `context`, `fmt`, `io`, `os`. Never add a third-party import to go.mod.
- **Interfaces over structs.** Handler signatures use `context.Context` + maps for extensibility. Future: typed generics.
- **Tests use pipes.** Integration tests simulate stdio with `io.Pipe()`. E2E tests spawn a real subprocess via `os/exec`.
- **Error codes follow JSON-RPC 2.0.** `-32700` = parse error (broken JSON), `-32600` = invalid request (well-formed JSON that is not a Request object, id `null`), `-32601` = method not found, `-32602` = invalid params, `-32000` = application error.
- **Bad input never kills the loop.** A malformed line is answered in-band and the server keeps serving; `RunWithIO` returns only on EOF or a read/write failure.
- **Inbound messages are size-capped.** One line may carry at most `Server.MaxRequestBytes` bytes (default `DefaultMaxRequestBytes`, 10 MiB; negative disables — not recommended). An oversized line is answered with `-32600` (id null), its remainder discarded, and the loop keeps serving — a single line can never exhaust memory.
- **Go naming.** Exported types are PascalCase. Unexported internals are camelCase. Test functions are `TestXxx`.
- **Protocol versions.** Default is `2026-07-28`. Legacy `initialize` still works and echoes `2024-11-05`, `2025-03-26`, or `2025-11-25` when the client asks for them. 2026-only fields (`resultType`, `ttlMs`, `cacheScope`, result `_meta`) are emitted only when the request declares `2026-07-28`.
- **Handlers must not kill the loop.** A panicking or timed-out handler is answered in-band; registration maps are mutex-protected.

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
