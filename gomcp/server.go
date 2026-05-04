// Package gomcp provides a zero-dependency Model Context Protocol (MCP) server
// framework for Go. Communicate with AI clients over stdio using JSON-RPC 2.0.
//
// Quick start:
//
//	srv := gomcp.NewServer("my-server", "1.0.0")
//	srv.AddTool(gomcp.Tool{...})
//	srv.Run() // blocks on stdio
package gomcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Server is an MCP server that communicates over stdio using JSON-RPC 2.0.
// It handles the MCP handshake and dispatches tools, resources, and prompts
// to registered handlers.
type Server struct {
	name      string
	version   string
	tools     map[string]Tool
	resources map[string]Resource
	prompts   map[string]Prompt
}

// NewServer creates a new MCP server with the given name and version.
// These are reported to the client during the initialize handshake.
func NewServer(name, version string) *Server {
	return &Server{
		name:      name,
		version:   version,
		tools:     make(map[string]Tool),
		resources: make(map[string]Resource),
		prompts:   make(map[string]Prompt),
	}
}

// AddTool registers a tool with the server. Tools are callable functions
// that the AI client can invoke with arguments.
func (s *Server) AddTool(tool Tool) {
	s.tools[tool.Name] = tool
}

// AddResource registers a resource with the server. Resources are readable
// data sources identified by URI.
func (s *Server) AddResource(res Resource) {
	s.resources[res.URI] = res
}

// AddPrompt registers a prompt template with the server. Prompts are
// pre-defined conversation templates.
func (s *Server) AddPrompt(prompt Prompt) {
	s.prompts[prompt.Name] = prompt
}

// Run starts the MCP server using os.Stdin and os.Stdout. It blocks until
// stdin closes. Errors are returned if reading or writing fails.
func (s *Server) Run() error {
	return s.runWithIO(os.Stdin, os.Stdout)
}

// runWithIO is the internal entry point accepting arbitrary io.Reader/Writer
// for testing with pipes.
func (s *Server) runWithIO(r io.Reader, w io.Writer) error {
	decoder := json.NewDecoder(r)
	encoder := json.NewEncoder(w)

	for {
		var req JSONRPCRequest
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("decode error: %w", err)
		}

		// Notifications have no ID — we silently consume them
		if req.ID == nil {
			continue
		}

		var respErr error
		switch req.Method {
		case "initialize":
			respErr = s.handleInitialize(req, encoder)
		case "tools/list":
			respErr = s.handleToolsList(req, encoder)
		case "tools/call":
			respErr = s.handleToolsCall(req, encoder)
		case "resources/list":
			respErr = s.handleResourcesList(req, encoder)
		case "resources/read":
			respErr = s.handleResourcesRead(req, encoder)
		case "prompts/list":
			respErr = s.handlePromptsList(req, encoder)
		case "prompts/get":
			respErr = s.handlePromptsGet(req, encoder)
		default:
			errResp := NewJSONRPCError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
			if err := encoder.Encode(errResp); err != nil {
				return fmt.Errorf("write error response: %w", err)
			}
		}

		if respErr != nil {
			return respErr
		}
	}
}

// handleInitialize responds to the MCP initialize handshake.
func (s *Server) handleInitialize(req JSONRPCRequest, encoder *json.Encoder) error {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]any{
				"name":    s.name,
				"version": s.version,
			},
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{},
				"prompts":   map[string]any{},
			},
		},
	}
	return encoder.Encode(resp)
}

// handleToolsList returns metadata for all registered tools.
func (s *Server) handleToolsList(req JSONRPCRequest, encoder *json.Encoder) error {
	toolList := make([]Tool, 0, len(s.tools))
	for _, tool := range s.tools {
		toolList = append(toolList, tool)
	}
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": toolList,
		},
	}
	return encoder.Encode(resp)
}

// toolsCallParams is deserialized from a tools/call request.
type toolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// handleToolsCall dispatches a tool call to the registered handler.
func (s *Server) handleToolsCall(req JSONRPCRequest, encoder *json.Encoder) error {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		errResp := NewJSONRPCError(req.ID, -32602, "Invalid params")
		return encoder.Encode(errResp)
	}

	tool, ok := s.tools[params.Name]
	if !ok {
		errResp := NewJSONRPCError(req.ID, -32602, fmt.Sprintf("Unknown tool: %s", params.Name))
		return encoder.Encode(errResp)
	}

	ctx := context.Background()
	result, err := tool.Handler(ctx, params.Arguments)
	if err != nil {
		errResp := NewJSONRPCError(req.ID, -32000, err.Error())
		return encoder.Encode(errResp)
	}

	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": result,
				},
			},
		},
	}
	return encoder.Encode(resp)
}

// handleResourcesList returns metadata for all registered resources.
func (s *Server) handleResourcesList(req JSONRPCRequest, encoder *json.Encoder) error {
	resourceList := make([]Resource, 0, len(s.resources))
	for _, res := range s.resources {
		resourceList = append(resourceList, res)
	}
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"resources": resourceList,
		},
	}
	return encoder.Encode(resp)
}

// handleResourcesRead reads a registered resource by URI and returns its content.
func (s *Server) handleResourcesRead(req JSONRPCRequest, encoder *json.Encoder) error {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		errResp := NewJSONRPCError(req.ID, -32602, "Invalid params")
		return encoder.Encode(errResp)
	}

	res, ok := s.resources[params.URI]
	if !ok {
		errResp := NewJSONRPCError(req.ID, -32602, fmt.Sprintf("Unknown resource: %s", params.URI))
		return encoder.Encode(errResp)
	}

	ctx := context.Background()
	content, err := res.Handler(ctx)
	if err != nil {
		errResp := NewJSONRPCError(req.ID, -32000, err.Error())
		return encoder.Encode(errResp)
	}

	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"contents": []map[string]any{
				{
					"uri":      res.URI,
					"mimeType": res.MimeType,
					"text":     content,
				},
			},
		},
	}
	return encoder.Encode(resp)
}

// handlePromptsList returns metadata for all registered prompts.
func (s *Server) handlePromptsList(req JSONRPCRequest, encoder *json.Encoder) error {
	promptList := make([]Prompt, 0, len(s.prompts))
	for _, p := range s.prompts {
		promptList = append(promptList, p)
	}
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"prompts": promptList,
		},
	}
	return encoder.Encode(resp)
}

// handlePromptsGet builds and returns a prompt from the registered handler.
func (s *Server) handlePromptsGet(req JSONRPCRequest, encoder *json.Encoder) error {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		errResp := NewJSONRPCError(req.ID, -32602, "Invalid params")
		return encoder.Encode(errResp)
	}

	prompt, ok := s.prompts[params.Name]
	if !ok {
		errResp := NewJSONRPCError(req.ID, -32602, fmt.Sprintf("Unknown prompt: %s", params.Name))
		return encoder.Encode(errResp)
	}

	ctx := context.Background()
	messages, err := prompt.Handler(ctx, params.Arguments)
	if err != nil {
		errResp := NewJSONRPCError(req.ID, -32000, err.Error())
		return encoder.Encode(errResp)
	}

	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"messages": messages,
		},
	}
	return encoder.Encode(resp)
}
