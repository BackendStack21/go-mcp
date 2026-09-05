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
	"sort"
	"sync"
	"time"
)

// DefaultMaxRequestBytes caps a single inbound JSON-RPC message when
// Server.MaxRequestBytes is unset. 10 MiB matches what a typical MCP client
// accepts for a response — requests larger than that are almost certainly
// hostile or malformed, and reading them unboundedly lets one line OOM the
// server process.
const DefaultMaxRequestBytes int64 = 10 << 20

// errMessageTooLarge reports that an inbound line exceeded the configured
// size cap. The rest of the line has already been discarded by readMessage.
var errMessageTooLarge = errors.New("message exceeds maximum size")

// Server is an MCP server that communicates over stdio using JSON-RPC 2.0.
// It handles the MCP handshake and dispatches tools, resources, and prompts
// to registered handlers.
type Server struct {
	name         string
	version      string
	protocolVer  string
	instructions string
	tools        map[string]Tool
	resources    map[string]Resource
	prompts      map[string]Prompt
	initialized  bool
	mu           sync.RWMutex

	// MaxRequestBytes caps one inbound JSON-RPC message (one newline-
	// delimited line). Zero selects DefaultMaxRequestBytes; a negative value
	// disables the cap (not recommended — a single unbounded line can
	// exhaust memory). An oversized message is answered in-band with a
	// -32600 error (id null) and the dispatch loop keeps serving.
	MaxRequestBytes int64

	// HandlerTimeout is a cooperative bound on a single tool, resource,
	// or prompt handler. Zero disables it (historical behavior). The
	// handler's context is cancelled when the deadline hits; a handler
	// that ignores ctx (blocking syscall, busy loop) is not preempted
	// and will still stall the sequential dispatch loop.
	HandlerTimeout time.Duration

	// ListPageSize caps items returned by tools/list, resources/list, and
	// prompts/list. Zero (default) returns the full list in one page —
	// the historical behavior. When set, clients page with the opaque
	// nextCursor from the previous response.
	ListPageSize int

	// ListTTLMs is the cache freshness hint (milliseconds) on list and
	// discover results for 2026-07-28 clients. Zero selects
	// DefaultListTTLMs (60s). A negative value sends ttlMs: 0 (always stale).
	ListTTLMs int64

	// ReadTTLMs is the cache freshness hint on resources/read for
	// 2026-07-28 clients. Zero (default) means immediately stale —
	// resource content is often dynamic.
	ReadTTLMs int64

	// CacheScope is the SEP-2549 cache scope advertised on cacheable
	// 2026-07-28 results. Empty selects "public". Use "private" when
	// list or read results are caller-specific.
	CacheScope string
}

// NewServer creates a new MCP server with the given name and version.
// These are reported to the client during the initialize handshake and
// on 2026-07-28 result _meta.
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

// SetProtocolVersion overrides the default MCP protocol version returned
// when a client does not request a specific supported revision.
func (s *Server) SetProtocolVersion(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.protocolVer = v
}

// SetInstructions sets optional natural-language guidance returned by
// initialize and server/discover. Use it to tell the model how to use
// this server effectively.
func (s *Server) SetInstructions(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instructions = text
}

// AddTool registers a tool with the server. Tools are callable functions
// that the AI client can invoke with arguments. Safe to call concurrently
// with request handling.
func (s *Server) AddTool(tool Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name] = tool
}

// AddResource registers a resource with the server. Resources are readable
// data sources identified by URI. Safe to call concurrently with request
// handling.
func (s *Server) AddResource(res Resource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources[res.URI] = res
}

// AddPrompt registers a prompt template with the server. Prompts are
// pre-defined conversation templates. Safe to call concurrently with
// request handling.
func (s *Server) AddPrompt(prompt Prompt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts[prompt.Name] = prompt
}

// Run starts the MCP server using os.Stdin and os.Stdout. It blocks until
// stdin closes. Errors are returned if reading or writing fails.
func (s *Server) Run() error {
	return s.RunContext(context.Background())
}

// RunContext is Run with a caller-supplied context. Handler invocations
// inherit ctx; cancelling it cancels in-flight handlers. The read loop
// still returns on stdin EOF.
func (s *Server) RunContext(ctx context.Context) error {
	return s.run(ctx, os.Stdin, os.Stdout)
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
	return s.run(context.Background(), r, w)
}

// RunWithIOContext is RunWithIO with a caller-supplied context. Handler
// invocations inherit ctx.
func (s *Server) RunWithIOContext(ctx context.Context, r io.Reader, w io.Writer) error {
	return s.run(ctx, r, w)
}

func (s *Server) run(ctx context.Context, r io.Reader, w io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	encoder := json.NewEncoder(w)
	br := bufio.NewReader(r)
	// lineBuf is reused across messages. Handlers run synchronously before
	// the next read, so nothing retains a reference to it across iterations.
	var lineBuf []byte

	s.mu.RLock()
	maxReq := s.MaxRequestBytes
	s.mu.RUnlock()
	if maxReq == 0 {
		maxReq = DefaultMaxRequestBytes
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		line, err := readMessage(br, lineBuf, maxReq)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			if errors.Is(err, errMessageTooLarge) {
				// Bad input never kills the loop: answer in-band and keep
				// serving. The oversized line is unparseable by definition,
				// so the response carries a null id.
				if werr := encoder.Encode(NewJSONRPCError(nil, ErrCodeInvalidRequest,
					fmt.Sprintf("Invalid Request: message exceeds maximum size of %d bytes", maxReq))); werr != nil {
					return fmt.Errorf("write error response: %w", werr)
				}
				continue
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

		// Notifications have no ID — consume them (including the legacy
		// initialized notice) and do not write a response.
		if req.ID == nil {
			s.handleNotification(req)
			continue
		}

		if req.JSONRPC != "2.0" || req.Method == "" {
			if werr := encoder.Encode(NewJSONRPCError(req.ID, ErrCodeInvalidRequest, "Invalid Request")); werr != nil {
				return fmt.Errorf("write error response: %w", werr)
			}
			continue
		}

		// 2026-07-28 clients declare a version in params._meta. An
		// unknown _meta version is a hard error except on server/discover,
		// which is the compatibility probe that advertises what we speak.
		// The legacy initialize.protocolVersion field is negotiated, not
		// rejected — older clients send whatever they speak and expect a
		// successful handshake back.
		if req.Method != "server/discover" {
			if ver := metaProtocolVersion(req.Params); ver != "" && !isSupportedProtocolVersion(ver) {
				if werr := encoder.Encode(unsupportedVersionError(req.ID, ver)); werr != nil {
					return fmt.Errorf("write error response: %w", werr)
				}
				continue
			}
		}

		var respErr error
		switch req.Method {
		case "initialize":
			respErr = s.handleInitialize(req, encoder)
		case "initialized", "notifications/initialized":
			// A client that attaches an id to the initialized notice
			// is treating it as a request — answer so it is not left
			// hanging. The notice itself is still recorded.
			s.handleNotification(req)
			respErr = encoder.Encode(JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  emptyObject{},
			})
		case "notifications/cancelled":
			// Sequential dispatch: the named request has already
			// finished by the time this line is read. Acknowledge
			// so a mis-framed notice-with-id does not hang.
			respErr = encoder.Encode(JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  emptyObject{},
			})
		case "ping":
			respErr = encoder.Encode(JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  emptyObject{},
			})
		case "server/discover":
			respErr = s.handleDiscover(req, encoder)
		case "tools/list":
			respErr = s.handleToolsList(ctx, req, encoder)
		case "tools/call":
			respErr = s.handleToolsCall(ctx, req, encoder)
		case "resources/list":
			respErr = s.handleResourcesList(req, encoder)
		case "resources/read":
			respErr = s.handleResourcesRead(ctx, req, encoder)
		case "resources/templates/list":
			respErr = s.handleResourceTemplatesList(req, encoder)
		case "prompts/list":
			respErr = s.handlePromptsList(req, encoder)
		case "prompts/get":
			respErr = s.handlePromptsGet(ctx, req, encoder)
		default:
			errResp := NewJSONRPCError(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("Method not found: %s", req.Method))
			if err := encoder.Encode(errResp); err != nil {
				return fmt.Errorf("write error response: %w", err)
			}
		}

		if respErr != nil {
			return respErr
		}
	}
}

func (s *Server) handleNotification(req JSONRPCRequest) {
	switch req.Method {
	case "initialized", "notifications/initialized":
		s.mu.Lock()
		s.initialized = true
		s.mu.Unlock()
	}
}

func unsupportedVersionError(id any, requested string) *JSONRPCError {
	return NewJSONRPCErrorWithData(id, ErrCodeUnsupportedProtocolVersion,
		fmt.Sprintf("Unsupported protocol version: %s", requested),
		map[string]any{
			"supported": SupportedProtocolVersions,
			"requested": requested,
		})
}

// readMessage reads one newline-terminated message from br into buf,
// returning the bytes without the trailing newline. A final message not
// terminated by newline is still returned at EOF; a subsequent call then
// reports io.EOF. Lines longer than the reader's buffer are accumulated up
// to max bytes; beyond that the remainder of the line is discarded
// (constant memory) and errMessageTooLarge is returned, so a hostile
// client cannot exhaust memory with a single oversized line.
func readMessage(br *bufio.Reader, buf []byte, max int64) ([]byte, error) {
	buf = buf[:0]
	for {
		chunk, err := br.ReadSlice('\n')
		if max > 0 && int64(len(buf))+int64(len(chunk)) > max {
			// Discard through the end of this line so the stream stays
			// framed and the next message parses normally.
			for err == bufio.ErrBufferFull {
				_, err = br.ReadSlice('\n')
			}
			return nil, errMessageTooLarge
		}
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
	code, message := ErrCodeParse, "Parse error"
	var typeErr *json.UnmarshalTypeError
	if errors.As(derr, &typeErr) {
		code, message = ErrCodeInvalidRequest, "Invalid Request"
	}
	return encoder.Encode(NewJSONRPCError(nil, code, message))
}

func (s *Server) snapshotInfo() (name, version, proto, instructions string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name, s.version, s.protocolVer, s.instructions
}

func (s *Server) resultMeta() *resultMeta {
	name, version, _, _ := s.snapshotInfo()
	return &resultMeta{ServerInfo: serverInfo{Name: name, Version: version}}
}

func (s *Server) cacheScope() string {
	s.mu.RLock()
	scope := s.CacheScope
	s.mu.RUnlock()
	if scope == cacheScopePrivate {
		return cacheScopePrivate
	}
	return cacheScopePublic
}

func (s *Server) listTTL() int64 {
	s.mu.RLock()
	ttl := s.ListTTLMs
	s.mu.RUnlock()
	if ttl < 0 {
		return 0
	}
	if ttl == 0 {
		return DefaultListTTLMs
	}
	return ttl
}

func (s *Server) readTTL() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ReadTTLMs < 0 {
		return 0
	}
	return s.ReadTTLMs
}

func (s *Server) listPageSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ListPageSize
}

// decorateCacheable fills 2026-07-28 cache / resultType / _meta fields.
// Legacy clients keep the pre-2026 shape (all of these stay empty/nil).
func (s *Server) decorateCacheable(params json.RawMessage, ttl int64) (resultType string, ttlMs *int64, scope string, meta *resultMeta) {
	if !speaks2026(params) {
		return "", nil, "", nil
	}
	t := ttl
	return resultTypeComplete, &t, s.cacheScope(), s.resultMeta()
}

func (s *Server) decorateResult(params json.RawMessage) (resultType string, meta *resultMeta) {
	if !speaks2026(params) {
		return "", nil
	}
	return resultTypeComplete, s.resultMeta()
}

func (s *Server) capabilities() serverCapabilities {
	return serverCapabilities{
		Tools:     emptyObject{},
		Resources: emptyObject{},
		Prompts:   emptyObject{},
	}
}

func (s *Server) handlerContext(parent context.Context, override time.Duration) (context.Context, context.CancelFunc) {
	s.mu.RLock()
	timeout := s.HandlerTimeout
	s.mu.RUnlock()
	if override < 0 {
		timeout = 0
	} else if override > 0 {
		timeout = override
	}
	if timeout > 0 {
		return context.WithTimeout(parent, timeout)
	}
	return context.WithCancel(parent)
}

// handleInitialize responds to the legacy MCP initialize handshake.
// 2026-07-28 made this optional; it is kept so existing clients continue
// to connect. The client's protocolVersion is echoed when we support it.
func (s *Server) handleInitialize(req JSONRPCRequest, encoder *json.Encoder) error {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(req.Params, &params)

	s.mu.Lock()
	s.initialized = true
	fallback := s.protocolVer
	name := s.name
	version := s.version
	instructions := s.instructions
	s.mu.Unlock()

	ver := negotiateProtocolVersion(params.ProtocolVersion, fallback)

	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: initializeResult{
			ProtocolVersion: ver,
			ServerInfo: serverInfo{
				Name:    name,
				Version: version,
			},
			Capabilities: s.capabilities(),
			Instructions: instructions,
		},
	}
	return encoder.Encode(resp)
}

// handleDiscover responds to server/discover (2026-07-28). Always succeeds:
// this is the stdio backward-compatibility probe and must not reject a
// client that has not yet chosen a version.
func (s *Server) handleDiscover(req JSONRPCRequest, encoder *json.Encoder) error {
	_, _, _, instructions := s.snapshotInfo()
	ttl := s.listTTL()
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: discoverResult{
			ResultType:        resultTypeComplete,
			SupportedVersions: append([]string(nil), SupportedProtocolVersions...),
			Capabilities:      s.capabilities(),
			Instructions:      instructions,
			TTLMs:             ttl,
			CacheScope:        s.cacheScope(),
			Meta:              *s.resultMeta(),
		},
	}
	return encoder.Encode(resp)
}

// handleToolsList returns metadata for all registered tools.
func (s *Server) handleToolsList(ctx context.Context, req JSONRPCRequest, encoder *json.Encoder) error {
	_ = ctx
	s.mu.RLock()
	toolList := make([]Tool, 0, len(s.tools))
	for _, tool := range s.tools {
		toolList = append(toolList, tool)
	}
	s.mu.RUnlock()

	sort.Slice(toolList, func(i, j int) bool { return toolList[i].Name < toolList[j].Name })

	page, next, err := paginate(toolList, cursorFromParams(req.Params), s.listPageSize())
	if err != nil {
		return encoder.Encode(NewJSONRPCError(req.ID, ErrCodeInvalidParams, "Invalid cursor"))
	}

	rt, ttl, scope, meta := s.decorateCacheable(req.Params, s.listTTL())
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: toolsListResult{
			Tools:      page,
			NextCursor: next,
			ResultType: rt,
			TTLMs:      ttl,
			CacheScope: scope,
			Meta:       meta,
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
func (s *Server) handleToolsCall(ctx context.Context, req JSONRPCRequest, encoder *json.Encoder) error {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return encoder.Encode(NewJSONRPCError(req.ID, ErrCodeInvalidParams, "Invalid params"))
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}

	s.mu.RLock()
	tool, ok := s.tools[params.Name]
	s.mu.RUnlock()
	if !ok {
		// Return in-band error per MCP convention (kept for compatibility
		// with existing clients; 2026 also allows -32602 here).
		rt, meta := s.decorateResult(req.Params)
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: toolCallResult{
				Content: []textContent{
					{Type: "text", Text: fmt.Sprintf("Unknown tool: %s", params.Name)},
				},
				IsError:    true,
				ResultType: rt,
				Meta:       meta,
			},
		}
		return encoder.Encode(resp)
	}

	result, err := s.invokeTool(ctx, tool, params.Arguments)
	rt, meta := s.decorateResult(req.Params)
	if err != nil {
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: toolCallResult{
				Content: []textContent{
					{Type: "text", Text: fmt.Sprintf("Error: %v", err)},
				},
				IsError:    true,
				ResultType: rt,
				Meta:       meta,
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
			ResultType: rt,
			Meta:       meta,
		},
	}
	return encoder.Encode(resp)
}

func (s *Server) invokeTool(ctx context.Context, tool Tool, args map[string]any) (result string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Fprintf(os.Stderr, "gomcp: tool %q panicked: %v\n", tool.Name, rec)
			err = fmt.Errorf("tool handler panicked")
		}
	}()
	if tool.Handler == nil {
		return "", fmt.Errorf("tool has no handler")
	}
	hctx, cancel := s.handlerContext(ctx, tool.Timeout)
	defer cancel()
	return tool.Handler(hctx, args)
}

// handleResourcesList returns metadata for all registered resources.
func (s *Server) handleResourcesList(req JSONRPCRequest, encoder *json.Encoder) error {
	s.mu.RLock()
	resourceList := make([]Resource, 0, len(s.resources))
	for _, res := range s.resources {
		resourceList = append(resourceList, res)
	}
	s.mu.RUnlock()

	sort.Slice(resourceList, func(i, j int) bool { return resourceList[i].URI < resourceList[j].URI })

	page, next, err := paginate(resourceList, cursorFromParams(req.Params), s.listPageSize())
	if err != nil {
		return encoder.Encode(NewJSONRPCError(req.ID, ErrCodeInvalidParams, "Invalid cursor"))
	}

	rt, ttl, scope, meta := s.decorateCacheable(req.Params, s.listTTL())
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: resourcesListResult{
			Resources:  page,
			NextCursor: next,
			ResultType: rt,
			TTLMs:      ttl,
			CacheScope: scope,
			Meta:       meta,
		},
	}
	return encoder.Encode(resp)
}

func (s *Server) handleResourceTemplatesList(req JSONRPCRequest, encoder *json.Encoder) error {
	rt, ttl, scope, meta := s.decorateCacheable(req.Params, s.listTTL())
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: resourceTemplatesListResult{
			ResourceTemplates: []resourceTemplate{},
			ResultType:        rt,
			TTLMs:             ttl,
			CacheScope:        scope,
			Meta:              meta,
		},
	}
	return encoder.Encode(resp)
}

// handleResourcesRead reads a registered resource by URI and returns its content.
func (s *Server) handleResourcesRead(ctx context.Context, req JSONRPCRequest, encoder *json.Encoder) error {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return encoder.Encode(NewJSONRPCError(req.ID, ErrCodeInvalidParams, "Invalid params"))
	}

	s.mu.RLock()
	res, ok := s.resources[params.URI]
	s.mu.RUnlock()
	if !ok {
		return encoder.Encode(NewJSONRPCError(req.ID, ErrCodeInvalidParams, fmt.Sprintf("Unknown resource: %s", params.URI)))
	}

	content, err := s.invokeResource(ctx, res)
	if err != nil {
		return encoder.Encode(NewJSONRPCError(req.ID, ErrCodeApplication, err.Error()))
	}

	rt, ttl, scope, meta := s.decorateCacheable(req.Params, s.readTTL())
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
			ResultType: rt,
			TTLMs:      ttl,
			CacheScope: scope,
			Meta:       meta,
		},
	}
	return encoder.Encode(resp)
}

func (s *Server) invokeResource(ctx context.Context, res Resource) (content string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Fprintf(os.Stderr, "gomcp: resource %q panicked: %v\n", res.URI, rec)
			err = fmt.Errorf("resource handler panicked")
		}
	}()
	if res.Handler == nil {
		return "", fmt.Errorf("resource has no handler")
	}
	hctx, cancel := s.handlerContext(ctx, res.Timeout)
	defer cancel()
	return res.Handler(hctx)
}

// handlePromptsList returns metadata for all registered prompts.
func (s *Server) handlePromptsList(req JSONRPCRequest, encoder *json.Encoder) error {
	s.mu.RLock()
	promptList := make([]Prompt, 0, len(s.prompts))
	for _, p := range s.prompts {
		promptList = append(promptList, p)
	}
	s.mu.RUnlock()

	sort.Slice(promptList, func(i, j int) bool { return promptList[i].Name < promptList[j].Name })

	page, next, err := paginate(promptList, cursorFromParams(req.Params), s.listPageSize())
	if err != nil {
		return encoder.Encode(NewJSONRPCError(req.ID, ErrCodeInvalidParams, "Invalid cursor"))
	}

	rt, ttl, scope, meta := s.decorateCacheable(req.Params, s.listTTL())
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: promptsListResult{
			Prompts:    page,
			NextCursor: next,
			ResultType: rt,
			TTLMs:      ttl,
			CacheScope: scope,
			Meta:       meta,
		},
	}
	return encoder.Encode(resp)
}

// handlePromptsGet builds and returns a prompt from the registered handler.
func (s *Server) handlePromptsGet(ctx context.Context, req JSONRPCRequest, encoder *json.Encoder) error {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return encoder.Encode(NewJSONRPCError(req.ID, ErrCodeInvalidParams, "Invalid params"))
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}

	s.mu.RLock()
	prompt, ok := s.prompts[params.Name]
	s.mu.RUnlock()
	if !ok {
		return encoder.Encode(NewJSONRPCError(req.ID, ErrCodeInvalidParams, fmt.Sprintf("Unknown prompt: %s", params.Name)))
	}

	messages, err := s.invokePrompt(ctx, prompt, params.Arguments)
	if err != nil {
		return encoder.Encode(NewJSONRPCError(req.ID, ErrCodeApplication, err.Error()))
	}

	rt, meta := s.decorateResult(req.Params)
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: promptsGetResult{
			Messages:   messages,
			ResultType: rt,
			Meta:       meta,
		},
	}
	return encoder.Encode(resp)
}

func (s *Server) invokePrompt(ctx context.Context, prompt Prompt, args map[string]any) (messages []PromptMessage, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Fprintf(os.Stderr, "gomcp: prompt %q panicked: %v\n", prompt.Name, rec)
			err = fmt.Errorf("prompt handler panicked")
		}
	}()
	if prompt.Handler == nil {
		return nil, fmt.Errorf("prompt has no handler")
	}
	hctx, cancel := s.handlerContext(ctx, prompt.Timeout)
	defer cancel()
	return prompt.Handler(hctx, args)
}
