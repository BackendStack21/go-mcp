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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// DefaultProtocolVersion is the MCP protocol version this server speaks.
const DefaultProtocolVersion = "2025-03-26"

// Server is an MCP server that communicates over stdio using JSON-RPC 2.0.
// It handles the MCP handshake and dispatches tools, resources, and prompts
// to registered handlers.
type Server struct {
	name        string
	version     string
	protocolVer string
	tools       map[string]Tool
	resources   map[string]Resource
	prompts     map[string]Prompt
	initialized bool
	mu          sync.Mutex
}

// NewServer creates a new MCP server with the given name and version.
// These are reported to the client during the initialize handshake.
func NewServer(name, version string) *Server {
	return &Server{
		name:        name,
		version:     version,
		protocolVer: DefaultProtocolVersion,
		tools:       make(map[string]Tool),
		resources:   make(map[string]Resource),
		prompts:     make(map[string]Prompt),
	}
}

// SetProtocolVersion overrides the default MCP protocol version.
func (s *Server) SetProtocolVersion(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.protocolVer = v
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
	return s.RunWithIO(os.Stdin, os.Stdout)
}

// RunWithIO starts the MCP server with custom I/O readers and writers,
// useful for testing with pipes or buffers.
//
// The input is newline-delimited JSON-RPC 2.0 (one message per line, the
// MCP stdio transport framing). A line that fails to parse never kills the
// dispatch loop: it is answered with a JSON-RPC error (-32700 for broken
// JSON, -32600 for well-formed JSON that is not a Request object) and the
// server keeps serving. RunWithIO returns only on clean EOF (nil), a read
// failure on r, or a write failure on w.
func (s *Server) RunWithIO(r io.Reader, w io.Writer) error {
	encoder := json.NewEncoder(w)
	br := bufio.NewReader(r)
	// lineBuf is reused across messages. Handlers run synchronously before
	// the next read, so nothing retains a reference to it across iterations.
	var lineBuf []byte

	for {
		line, err := readMessage(br, lineBuf)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read error: %w", err)
		}
		lineBuf = line[:0] // reclaim capacity for the next iteration

		msg := bytes.TrimSpace(line)
		if len(msg) == 0 {
			continue // blank separator line
		}

		var req JSONRPCRequest
		if derr := json.Unmarshal(msg, &req); derr != nil {
			if werr := writeDecodeError(encoder, derr); werr != nil {
				return fmt.Errorf("write error response: %w", werr)
			}
			continue
		}

		// Notifications have no ID — we silently consume them
		if req.ID == nil {
			continue
		}

		var respErr error
		switch req.Method {
		case "initialize":
			respErr = s.handleInitialize(req, encoder)
		case "initialized":
			// Notification — silently mark as initialized
			s.mu.Lock()
			s.initialized = true
			s.mu.Unlock()
		case "ping":
			respErr = encoder.Encode(JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  emptyObject{},
			})
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

// readMessage reads one newline-terminated message from br into buf,
// returning the bytes without the trailing newline. A final message not
// terminated by EOF is still returned; a subsequent call then reports
// io.EOF. Lines longer than the reader's buffer are accumulated, so there
// is no message-size limit (matching the previous json.Decoder behavior).
func readMessage(br *bufio.Reader, buf []byte) ([]byte, error) {
	buf = buf[:0]
	for {
		chunk, err := br.ReadSlice('\n')
		buf = append(buf, chunk...)
		switch err {
		case nil:
			return buf, nil
		case bufio.ErrBufferFull:
			continue // line longer than the buffer: keep accumulating
		case io.EOF:
			if len(buf) > 0 {
				return buf, nil
			}
			return nil, io.EOF
		default:
			return nil, err
		}
	}
}

// writeDecodeError answers an unparseable inbound line per JSON-RPC 2.0:
// -32700 (Parse error) for broken JSON, -32600 (Invalid Request) for
// well-formed JSON that cannot be a Request object. The id is null — the
// request was never understood, so there is nothing to correlate against.
func writeDecodeError(encoder *json.Encoder, derr error) error {
	code, message := -32700, "Parse error"
	var typeErr *json.UnmarshalTypeError
	if errors.As(derr, &typeErr) {
		code, message = -32600, "Invalid Request"
	}
	return encoder.Encode(NewJSONRPCError(nil, code, message))
}

// handleInitialize responds to the MCP initialize handshake.
func (s *Server) handleInitialize(req JSONRPCRequest, encoder *json.Encoder) error {
	s.mu.Lock()
	s.initialized = true
	ver := s.protocolVer
	s.mu.Unlock()

	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: initializeResult{
			ProtocolVersion: ver,
			ServerInfo: serverInfo{
				Name:    s.name,
				Version: s.version,
			},
		},
	}
	return encoder.Encode(resp)
}

// handleToolsList returns metadata for all registered tools.
func (s *Server) handleToolsList(req JSONRPCRequest, encoder *json.Encoder) error {
	s.mu.Lock()
	init := s.initialized
	s.mu.Unlock()
	if !init {
		return encoder.Encode(NewJSONRPCError(req.ID, -32600, "Not initialized"))
	}

	toolList := make([]Tool, 0, len(s.tools))
	for _, tool := range s.tools {
		toolList = append(toolList, tool)
	}
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  toolsListResult{Tools: toolList},
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
	s.mu.Lock()
	init := s.initialized
	s.mu.Unlock()
	if !init {
		return encoder.Encode(NewJSONRPCError(req.ID, -32600, "Not initialized"))
	}

	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		errResp := NewJSONRPCError(req.ID, -32602, "Invalid params")
		return encoder.Encode(errResp)
	}

	tool, ok := s.tools[params.Name]
	if !ok {
		// Return in-band error per MCP convention
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: toolCallResult{
				Content: []textContent{
					{Type: "text", Text: fmt.Sprintf("Unknown tool: %s", params.Name)},
				},
				IsError: true,
			},
		}
		return encoder.Encode(resp)
	}

	ctx := context.Background()
	result, err := tool.Handler(ctx, params.Arguments)
	if err != nil {
		// Return in-band error per MCP convention
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: toolCallResult{
				Content: []textContent{
					{Type: "text", Text: fmt.Sprintf("Error: %v", err)},
				},
				IsError: true,
			},
		}
		return encoder.Encode(resp)
	}

	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: toolCallResult{
			Content: []textContent{
				{Type: "text", Text: result},
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
		Result:  resourcesListResult{Resources: resourceList},
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
		Result: resourcesReadResult{
			Contents: []resourceContent{
				{
					URI:      res.URI,
					MimeType: res.MimeType,
					Text:     content,
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
		Result:  promptsListResult{Prompts: promptList},
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
		Result:  promptsGetResult{Messages: messages},
	}
	return encoder.Encode(resp)
}
