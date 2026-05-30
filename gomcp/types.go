package gomcp

import "context"

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
type initializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	ServerInfo      serverInfo         `json:"serverInfo"`
	Capabilities    serverCapabilities `json:"capabilities"`
}

// textContent is a single text content block returned by tool calls.
type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// toolCallResult is the result payload for a tools/call response. IsError is
// omitted on success and set to true for in-band tool errors.
type toolCallResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// toolsListResult is the result payload for tools/list.
type toolsListResult struct {
	Tools []Tool `json:"tools"`
}

// resourcesListResult is the result payload for resources/list.
type resourcesListResult struct {
	Resources []Resource `json:"resources"`
}

// resourceContent is a single content block returned by resources/read.
type resourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// resourcesReadResult is the result payload for resources/read.
type resourcesReadResult struct {
	Contents []resourceContent `json:"contents"`
}

// promptsListResult is the result payload for prompts/list.
type promptsListResult struct {
	Prompts []Prompt `json:"prompts"`
}

// promptsGetResult is the result payload for prompts/get.
type promptsGetResult struct {
	Messages []PromptMessage `json:"messages"`
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

// Tool defines a callable tool registered with the MCP server.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema any         `json:"inputSchema"`
	Handler     ToolHandler `json:"-"`
}

// Resource defines a readable resource registered with the MCP server.
type Resource struct {
	URI         string          `json:"uri"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	MimeType    string          `json:"mimeType,omitempty"`
	Handler     ResourceHandler `json:"-"`
}

// Prompt defines a prompt template registered with the MCP server.
type Prompt struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Arguments   []PromptArg   `json:"arguments,omitempty"`
	Handler     PromptHandler `json:"-"`
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
