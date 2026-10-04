package gomcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
)

// Client is an MCP client speaking to a server over any transport.
// All methods are safe for concurrent use.
type Client interface {
	// Initialize performs the MCP handshake and returns the server's
	// capabilities. Call it once before other requests.
	Initialize(ctx context.Context, clientName, clientVersion string) (*InitializeResult, error)
	// NotifyInitialized sends the notifications/initialized notice.
	NotifyInitialized(ctx context.Context) error
	// ListTools returns the server's registered tools.
	ListTools(ctx context.Context) ([]Tool, error)
	// CallTool invokes a tool and returns its text output.
	CallTool(ctx context.Context, name string, args map[string]any) (string, error)
	// ListResources returns the server's registered resources.
	ListResources(ctx context.Context) ([]Resource, error)
	// ReadResource reads one resource by URI.
	ReadResource(ctx context.Context, uri string) (string, error)
	// ListPrompts returns the server's registered prompts.
	ListPrompts(ctx context.Context) ([]Prompt, error)
	// GetPrompt renders one prompt with arguments.
	GetPrompt(ctx context.Context, name string, args map[string]any) ([]PromptMessage, error)
	// Close releases transport resources (HTTP client: no-op; stdio:
	// terminates the subprocess).
	Close() error
}

// ServerInfo identifies a server in an InitializeResult.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the server's response to initialize.
type InitializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	ServerInfo      ServerInfo `json:"serverInfo"`
	Instructions    string     `json:"instructions,omitempty"`
}

// ClientProtocolVersion is the protocol version HTTPClient and StdioClient
// request by default.
const ClientProtocolVersion = DefaultProtocolVersion

// rpcCaller issues one JSON-RPC request and decodes the result into out.
type rpcCaller interface {
	call(ctx context.Context, method string, params any, out any) error
	notify(ctx context.Context, method string, params any) error
}

// --- shared JSON-RPC plumbing ---

type rpcWireParams struct {
	Name      string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
	URI       string         `json:"uri,omitempty"`
}

func decodeRPCResponse(body []byte, out any) error {
	var env struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("malformed response: %w", err)
	}
	if env.Error != nil {
		return fmt.Errorf("rpc error %d: %s", env.Error.Code, env.Error.Message)
	}
	if len(env.Result) == 0 {
		return fmt.Errorf("response has no result: %s", strings.TrimSpace(string(body)))
	}
	if out != nil {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("decode result: %w", err)
		}
	}
	return nil
}

func clientMethods(c rpcCaller, protoVersion func() string) clientCore {
	return clientCore{c: c, protoVersion: protoVersion}
}

type clientCore struct {
	c            rpcCaller
	protoVersion func() string
}

func (cc clientCore) Initialize(ctx context.Context, clientName, clientVersion string) (*InitializeResult, error) {
	var res InitializeResult
	err := cc.c.call(ctx, "initialize", map[string]any{
		"protocolVersion": cc.protoVersion(),
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": clientName, "version": clientVersion},
	}, &res)
	if err != nil {
		return nil, err
	}
	if !isSupportedProtocolVersion(res.ProtocolVersion) {
		return nil, fmt.Errorf("server speaks unsupported protocol version %q (client requested %q)", res.ProtocolVersion, cc.protoVersion())
	}
	return &res, nil
}

func (cc clientCore) NotifyInitialized(ctx context.Context) error {
	return cc.c.notify(ctx, "notifications/initialized", nil)
}

func (cc clientCore) ListTools(ctx context.Context) ([]Tool, error) {
	var res struct {
		Tools []Tool `json:"tools"`
	}
	if err := cc.c.call(ctx, "tools/list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return res.Tools, nil
}

func (cc clientCore) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := cc.c.call(ctx, "tools/call", rpcWireParams{Name: name, Arguments: args}, &res); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, c := range res.Content {
		sb.WriteString(c.Text)
	}
	if res.IsError {
		return sb.String(), fmt.Errorf("tool error: %s", sb.String())
	}
	return sb.String(), nil
}

func (cc clientCore) ListResources(ctx context.Context) ([]Resource, error) {
	var res struct {
		Resources []Resource `json:"resources"`
	}
	if err := cc.c.call(ctx, "resources/list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return res.Resources, nil
}

func (cc clientCore) ReadResource(ctx context.Context, uri string) (string, error) {
	var res struct {
		Contents []struct {
			Text string `json:"text"`
		} `json:"contents"`
	}
	if err := cc.c.call(ctx, "resources/read", rpcWireParams{URI: uri}, &res); err != nil {
		return "", err
	}
	if len(res.Contents) == 0 {
		return "", fmt.Errorf("resource %q returned no contents", uri)
	}
	return res.Contents[0].Text, nil
}

func (cc clientCore) ListPrompts(ctx context.Context) ([]Prompt, error) {
	var res struct {
		Prompts []Prompt `json:"prompts"`
	}
	if err := cc.c.call(ctx, "prompts/list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return res.Prompts, nil
}

func (cc clientCore) GetPrompt(ctx context.Context, name string, args map[string]any) ([]PromptMessage, error) {
	var res struct {
		Messages []PromptMessage `json:"messages"`
	}
	if err := cc.c.call(ctx, "prompts/get", rpcWireParams{Name: name, Arguments: args}, &res); err != nil {
		return nil, err
	}
	return res.Messages, nil
}

// --- HTTP transport ---

// MaxResponseBytes caps one response body read by HTTPClient. A hostile
// or buggy server cannot OOM the client with an unbounded response.
const MaxResponseBytes int64 = 64 << 20 // 64 MiB

// HTTPClient is a Client over the MCP Streamable HTTP transport.
type HTTPClient struct {
	url    string
	token  string
	http   *http.Client
	core   clientCore
	nextID int64
	mu     sync.Mutex
}

// NewHTTPClient returns a Client posting JSON-RPC messages to url.
func NewHTTPClient(url string) *HTTPClient {
	return NewHTTPClientWithToken(url, "")
}

// NewHTTPClientWithToken returns a Client that authenticates every request
// with `Authorization: Bearer <token>`. Prefer this over embedding
// credentials in the URL — URLs leak into logs and error messages.
func NewHTTPClientWithToken(url, token string) *HTTPClient {
	hc := &HTTPClient{url: url, token: token, http: &http.Client{}}
	hc.core = clientMethods(hc, func() string { return ClientProtocolVersion })
	return hc
}

func (c *HTTPClient) setAuth(h http.Header) {
	if c.token != "" {
		h.Set("Authorization", "Bearer "+c.token)
	}
}

func (c *HTTPClient) call(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	c.setAuth(req.Header)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusAccepted {
		return fmt.Errorf("unexpected 202 for request %q", method)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	// Handle both plain JSON and single-event SSE responses.
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		body = sseData(body)
	}
	return decodeRPCResponse(body, out)
}

func (c *HTTPClient) notify(ctx context.Context, method string, params any) error {
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	c.setAuth(req.Header)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("notify %q: http %d: %s", method, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// sseData extracts the JSON payload of the first data: field. The space
// after the colon is optional per the SSE spec.
func sseData(body []byte) []byte {
	for _, line := range strings.Split(string(body), "\n") {
		d, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		return []byte(strings.TrimSpace(d))
	}
	return body
}

func (c *HTTPClient) Initialize(ctx context.Context, name, version string) (*InitializeResult, error) {
	return c.core.Initialize(ctx, name, version)
}
func (c *HTTPClient) NotifyInitialized(ctx context.Context) error {
	return c.core.NotifyInitialized(ctx)
}
func (c *HTTPClient) ListTools(ctx context.Context) ([]Tool, error) {
	return c.core.ListTools(ctx)
}
func (c *HTTPClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	return c.core.CallTool(ctx, name, args)
}
func (c *HTTPClient) ListResources(ctx context.Context) ([]Resource, error) {
	return c.core.ListResources(ctx)
}
func (c *HTTPClient) ReadResource(ctx context.Context, uri string) (string, error) {
	return c.core.ReadResource(ctx, uri)
}
func (c *HTTPClient) ListPrompts(ctx context.Context) ([]Prompt, error) {
	return c.core.ListPrompts(ctx)
}
func (c *HTTPClient) GetPrompt(ctx context.Context, name string, args map[string]any) ([]PromptMessage, error) {
	return c.core.GetPrompt(ctx, name, args)
}
func (c *HTTPClient) Close() error { return nil }

// --- stdio transport ---

// StdioClient is a Client that spawns a server subprocess and speaks MCP
// stdio (newline-delimited JSON-RPC) over its pipes.
//
// A single lifetime reader goroutine demultiplexes responses by JSON-RPC
// id. A timed-out call does not desync the stream: its late response is
// delivered to the abandoned channel (buffered, then discarded) and the
// next call still receives its own response.
type StdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	core   clientCore
	nextID int64
	mu     sync.Mutex

	pendingMu  sync.Mutex
	pending    map[int64]chan []byte
	closed     bool
	readerDone chan struct{}
}

// NewStdioClient starts the server binary at path (with optional args) and
// connects over stdio.
func NewStdioClient(path string, args ...string) (*StdioClient, error) {
	cmd := exec.Command(path, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	sc := &StdioClient{
		cmd:        cmd,
		stdin:      stdin,
		pending:    make(map[int64]chan []byte),
		readerDone: make(chan struct{}),
	}
	sc.core = clientMethods(sc, func() string { return ClientProtocolVersion })
	go sc.readLoop(stdout)
	return sc, nil
}

// readLoop is the single lifetime reader. It matches each response to the
// pending request id; unmatched responses (e.g. late replies to timed-out
// calls) are discarded.
func (c *StdioClient) readLoop(r io.Reader) {
	defer close(c.readerDone)
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var env struct {
				ID *int64 `json:"id"`
			}
			if json.Unmarshal(line, &env) == nil && env.ID != nil {
				c.pendingMu.Lock()
				ch, ok := c.pending[*env.ID]
				if ok {
					delete(c.pending, *env.ID)
				}
				c.pendingMu.Unlock()
				if ok {
					ch <- line // buffered cap 1; never blocks
				}
			}
		}
		if err != nil {
			c.failAllPending(err)
			return
		}
	}
}

// failAllPending unblocks every waiting call when the stream dies.
func (c *StdioClient) failAllPending(err error) {
	c.pendingMu.Lock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- []byte(fmt.Sprintf(`{"error":{"code":-32000,"message":"stdio stream closed: %s"}}`, err))
	}
	c.pendingMu.Unlock()
}

func (c *StdioClient) roundTrip(ctx context.Context, payload []byte, id *int64) ([]byte, error) {
	c.pendingMu.Lock()
	if c.closed {
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("client closed")
	}
	var ch chan []byte
	if id != nil {
		ch = make(chan []byte, 1)
		c.pending[*id] = ch
	}
	c.pendingMu.Unlock()

	if _, err := c.stdin.Write(append(payload, '\n')); err != nil {
		if id != nil {
			c.pendingMu.Lock()
			delete(c.pending, *id)
			c.pendingMu.Unlock()
		}
		return nil, fmt.Errorf("write: %w", err)
	}
	if id == nil {
		return nil, nil // notification: no response expected
	}

	select {
	case b := <-ch:
		return b, nil
	case <-ctx.Done():
		// Leave the pending entry; the reader deletes and drains it when
		// the late response arrives, so the stream stays in sync.
		return nil, ctx.Err()
	case <-c.readerDone:
		// Stream died; any pending entry was already failed.
		select {
		case b := <-ch:
			return b, nil
		default:
			return nil, fmt.Errorf("stdio stream closed")
		}
	}
}

func (c *StdioClient) call(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	})
	if err != nil {
		return err
	}
	body, err := c.roundTrip(ctx, payload, &id)
	if err != nil {
		return err
	}
	return decodeRPCResponse(body, out)
}

func (c *StdioClient) notify(ctx context.Context, method string, params any) error {
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	_, err = c.roundTrip(ctx, payload, nil)
	return err
}

func (c *StdioClient) Initialize(ctx context.Context, name, version string) (*InitializeResult, error) {
	return c.core.Initialize(ctx, name, version)
}
func (c *StdioClient) NotifyInitialized(ctx context.Context) error {
	return c.core.NotifyInitialized(ctx)
}
func (c *StdioClient) ListTools(ctx context.Context) ([]Tool, error) {
	return c.core.ListTools(ctx)
}
func (c *StdioClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	return c.core.CallTool(ctx, name, args)
}
func (c *StdioClient) ListResources(ctx context.Context) ([]Resource, error) {
	return c.core.ListResources(ctx)
}
func (c *StdioClient) ReadResource(ctx context.Context, uri string) (string, error) {
	return c.core.ReadResource(ctx, uri)
}
func (c *StdioClient) ListPrompts(ctx context.Context) ([]Prompt, error) {
	return c.core.ListPrompts(ctx)
}
func (c *StdioClient) GetPrompt(ctx context.Context, name string, args map[string]any) ([]PromptMessage, error) {
	return c.core.GetPrompt(ctx, name, args)
}

func (c *StdioClient) Close() error {
	c.pendingMu.Lock()
	if c.closed {
		c.pendingMu.Unlock()
		<-c.readerDone
		return nil
	}
	c.closed = true
	c.pendingMu.Unlock()

	// Closing stdin makes the server's read loop EOF; the subprocess then
	// exits and its pipes close, ending our reader goroutine.
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	err := c.cmd.Wait()
	<-c.readerDone // reader observes pipe close and drains pending calls
	return err
}
