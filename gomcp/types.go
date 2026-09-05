package gomcp

import (
	"context"
	"time"
)

// ToolHandler is the function signature for tool implementations.
// Receives the request context and parsed arguments, returns a text result.
type ToolHandler func(ctx context.Context, args map[string]any) (string, error)

// ResourceHandler returns the full content of a resource as a string.
type ResourceHandler func(ctx context.Context) (string, error)

// PromptHandler builds a prompt message from arguments.
// Returns a list of prompt messages (role + content).
type PromptHandler func(ctx context.Context, args map[string]any) ([]PromptMessage, error)

// --- Response result payloads ---
//
// These typed structs are used to build JSON-RPC result objects instead of
// ad-hoc map[string]any literals. Encoding a struct avoids per-request map
// allocation and the runtime key-sorting that encoding/json performs for maps,
// while producing identical JSON output.

// emptyObject marshals to "{}". It is used for capability advertisements and
// the ping result, which are intentionally empty objects.
type emptyObject = struct{}

// serverInfo identifies the server in the initialize handshake.
type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// serverCapabilities advertises which MCP feature groups the server supports.
type serverCapabilities struct {
	Tools     emptyObject `json:"tools"`
	Resources emptyObject `json:"resources"`
	Prompts   emptyObject `json:"prompts"`
}

// initializeResult is the result payload for the initialize handshake.
// The shape is the pre-2026 handshake so existing clients keep working.
type initializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	ServerInfo      serverInfo         `json:"serverInfo"`
	Capabilities    serverCapabilities `json:"capabilities"`
	Instructions    string             `json:"instructions,omitempty"`
}

// resultMeta is the 2026-07-28 result _meta object. Servers SHOULD identify
// themselves on every modern result (SEP-2575).
type resultMeta struct {
	ServerInfo serverInfo `json:"io.modelcontextprotocol/serverInfo"`
}

// discoverResult is the result payload for server/discover (2026-07-28).
type discoverResult struct {
	ResultType        string             `json:"resultType"`
	SupportedVersions []string           `json:"supportedVersions"`
	Capabilities      serverCapabilities `json:"capabilities"`
	Instructions      string             `json:"instructions,omitempty"`
	TTLMs             int64              `json:"ttlMs"`
	CacheScope        string             `json:"cacheScope"`
	Meta              resultMeta         `json:"_meta"`
}

// textContent is a single text content block returned by tool calls.
type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// toolCallResult is the result payload for a tools/call response. IsError is
// omitted on success and set to true for in-band tool errors. ResultType and
// Meta are populated only for 2026-07-28 clients.
type toolCallResult struct {
	Content    []textContent `json:"content"`
	IsError    bool          `json:"isError,omitempty"`
	ResultType string        `json:"resultType,omitempty"`
	Meta       *resultMeta   `json:"_meta,omitempty"`
}

// toolsListResult is the result payload for tools/list.
// TTLMs is a pointer so a zero value is still emitted for 2026-07-28
// clients (omitempty on int64 would drop ttlMs: 0).
type toolsListResult struct {
	Tools      []Tool      `json:"tools"`
	NextCursor string      `json:"nextCursor,omitempty"`
	ResultType string      `json:"resultType,omitempty"`
	TTLMs      *int64      `json:"ttlMs,omitempty"`
	CacheScope string      `json:"cacheScope,omitempty"`
	Meta       *resultMeta `json:"_meta,omitempty"`
}

// resourcesListResult is the result payload for resources/list.
type resourcesListResult struct {
	Resources  []Resource  `json:"resources"`
	NextCursor string      `json:"nextCursor,omitempty"`
	ResultType string      `json:"resultType,omitempty"`
	TTLMs      *int64      `json:"ttlMs,omitempty"`
	CacheScope string      `json:"cacheScope,omitempty"`
	Meta       *resultMeta `json:"_meta,omitempty"`
}

// resourceTemplatesListResult is the result payload for resources/templates/list.
type resourceTemplatesListResult struct {
	ResourceTemplates []resourceTemplate `json:"resourceTemplates"`
	NextCursor        string             `json:"nextCursor,omitempty"`
	ResultType        string             `json:"resultType,omitempty"`
	TTLMs             *int64             `json:"ttlMs,omitempty"`
	CacheScope        string             `json:"cacheScope,omitempty"`
	Meta              *resultMeta        `json:"_meta,omitempty"`
}

// resourceTemplate is a URI-template resource entry. This server does not
// yet register templates; the type exists so resources/templates/list can
// return a spec-compliant empty catalog instead of -32601.
type resourceTemplate struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// resourceContent is a single content block returned by resources/read.
type resourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// resourcesReadResult is the result payload for resources/read.
type resourcesReadResult struct {
	Contents   []resourceContent `json:"contents"`
	ResultType string            `json:"resultType,omitempty"`
	TTLMs      *int64            `json:"ttlMs,omitempty"`
	CacheScope string            `json:"cacheScope,omitempty"`
	Meta       *resultMeta       `json:"_meta,omitempty"`
}

// promptsListResult is the result payload for prompts/list.
type promptsListResult struct {
	Prompts    []Prompt    `json:"prompts"`
	NextCursor string      `json:"nextCursor,omitempty"`
	ResultType string      `json:"resultType,omitempty"`
	TTLMs      *int64      `json:"ttlMs,omitempty"`
	CacheScope string      `json:"cacheScope,omitempty"`
	Meta       *resultMeta `json:"_meta,omitempty"`
}

// promptsGetResult is the result payload for prompts/get.
type promptsGetResult struct {
	Messages   []PromptMessage `json:"messages"`
	ResultType string          `json:"resultType,omitempty"`
	Meta       *resultMeta     `json:"_meta,omitempty"`
}

// Property describes a single input parameter for a tool or prompt argument.
type Property struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// InputSchema defines the JSON Schema for a tool's input arguments.
type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

// ToolAnnotations are optional hints about a tool's behavior. Clients must
// treat them as untrusted hints, never as a security boundary.
type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint,omitempty"`
	DestructiveHint bool   `json:"destructiveHint,omitempty"`
	IdempotentHint  bool   `json:"idempotentHint,omitempty"`
	OpenWorldHint   bool   `json:"openWorldHint,omitempty"`
}

// Tool defines a callable tool registered with the MCP server.
type Tool struct {
	Name         string           `json:"name"`
	Title        string           `json:"title,omitempty"`
	Description  string           `json:"description,omitempty"`
	InputSchema  any              `json:"inputSchema"`
	OutputSchema any              `json:"outputSchema,omitempty"`
	Annotations  *ToolAnnotations `json:"annotations,omitempty"`
	Handler      ToolHandler      `json:"-"`
	// Timeout overrides Server.HandlerTimeout for this tool. Zero means
	// inherit; a negative value disables the timeout for this tool.
	Timeout time.Duration `json:"-"`
}

// Resource defines a readable resource registered with the MCP server.
type Resource struct {
	URI         string          `json:"uri"`
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	MimeType    string          `json:"mimeType,omitempty"`
	Handler     ResourceHandler `json:"-"`
	// Timeout overrides Server.HandlerTimeout for this resource. Zero
	// means inherit; a negative value disables the timeout.
	Timeout time.Duration `json:"-"`
}

// Prompt defines a prompt template registered with the MCP server.
type Prompt struct {
	Name        string        `json:"name"`
	Title       string        `json:"title,omitempty"`
	Description string        `json:"description,omitempty"`
	Arguments   []PromptArg   `json:"arguments,omitempty"`
	Handler     PromptHandler `json:"-"`
	// Timeout overrides Server.HandlerTimeout for this prompt. Zero means
	// inherit; a negative value disables the timeout.
	Timeout time.Duration `json:"-"`
}

// PromptArg describes an argument that a prompt template accepts.
type PromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptMessage is a single message in a prompt response.
type PromptMessage struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content any    `json:"content"` // string or content block object
}

// NewTextContent creates a simple text content block.
func NewTextContent(text string) map[string]any {
	return map[string]any{
		"type": "text",
		"text": text,
	}
}
