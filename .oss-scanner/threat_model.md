# Threat model — go-mcp

## What this project does and where untrusted input enters
- go-mcp is a zero-dependency Go framework for Model Context Protocol (MCP) servers (stdio and Streamable HTTP transports), plus MCP clients for both transports.
- Server side, untrusted: every JSON-RPC message from clients — over stdio, and over HTTP (method, headers such as `Authorization`, `Origin`, `Accept`, `Content-Type`, and the body). Tool/resource/prompt arguments are attacker-controlled.
- Client side, untrusted: every response, notification and SSE event returned by an MCP server the client connects to.
- Trusted: Go code registering tools/resources/prompts, and server options (`SetAuthToken`, `SetAllowedOrigins`, `MaxRequestBytes`, `HandlerTimeout`).

## Components that matter most / least
- Highest priority: `gomcp/httpserver.go` (bearer auth, Origin allowlist / DNS-rebinding defense, method/content-type checks, body size limits, SSE responses), `gomcp/jsonrpc.go` and `gomcp/protocol.go` (message parsing and dispatch), `gomcp/server.go` (stdio loop, handler timeouts, panics in handlers), `gomcp/path.go` (resource URI/path handling), `gomcp/client.go` (parsing server responses, SSE, stdio child processes).
- Out of scope: `examples/` (demo servers such as `fs-navigator` and `sys-monitor` are intentionally permissive), `docs/`, `test/`.

## How to exercise it
- `go test ./gomcp/ -race`. Example servers are built into `bin/` (`greet`, `sys-monitor`, `fs-navigator`); `srv.ListenAndServe(addr)` exposes the HTTP transport.

## How you rate severity
- Critical: bypassing bearer auth or invoking a tool without a valid token when one is set; remote code execution.
- High: Origin allowlist bypass (DNS rebinding / CSRF from a browser); a single message crashing the server process (unrecovered panic) or deadlocking it; path traversal in resource/path helpers; a malicious server crashing or hijacking a client.
- Medium: request size or timeout limits not enforced (memory/CPU exhaustion), response mix-ups between concurrent requests, information leaks in error messages.
- Low: spec non-conformance without security impact, hardening suggestions.

## Anything to leave alone
- Behavior of `examples/` servers, and HTTP deployments where the operator chose not to set a token or Origin allowlist (documented as optional).
