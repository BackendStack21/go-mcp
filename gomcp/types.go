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
	InputSchema InputSchema `json:"inputSchema"`
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
