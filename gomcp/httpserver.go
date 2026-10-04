package gomcp

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"
)

// Handler returns an http.Handler exposing the server over the MCP
// Streamable HTTP transport (protocol versions 2025-03-26 and later).
//
// Each POST body carries one JSON-RPC 2.0 message (a JSON object — arrays,
// i.e. JSON-RPC batches, are rejected with -32600, matching the stdio
// framing of one message per request). Responses are application/json,
// or text/event-stream (a single event) when the client's Accept header
// prefers SSE. Notifications are answered with 202 and no body.
//
// The transport is stateless: there is no session state, no server-initiated
// GET stream, and DELETE is not supported. GET, DELETE, and PUT return 405.
// Content-Type must be application/json; anything else is a 415.
//
// Message size and handler behavior follow the Server's existing settings
// (MaxRequestBytes, HandlerTimeout). Bad input is answered in-band with a
// JSON-RPC error and never fails the HTTP layer.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

// Default HTTP server timeouts. These bound header reads, full request
// reads, and idle keep-alive connections so a slow or hostile client
// cannot pin goroutines and file descriptors indefinitely (slowloris).
const (
	defaultReadHeaderTimeout = 10 * time.Second
	defaultReadTimeout       = 30 * time.Second
	defaultIdleTimeout       = 120 * time.Second
)

// newHTTPServer builds the http.Server used by ListenAndServe and Serve.
func (s *Server) newHTTPServer() *http.Server {
	return &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		IdleTimeout:       defaultIdleTimeout,
	}
}

// ListenAndServe starts an HTTP server on addr serving the MCP Streamable
// HTTP transport. It blocks until the listener fails or the process exits.
func (s *Server) ListenAndServe(addr string) error {
	hs := s.newHTTPServer()
	hs.Addr = addr
	return hs.ListenAndServe()
}

// Serve accepts connections on an existing listener, serving the MCP
// Streamable HTTP transport. It blocks until the listener fails.
func (s *Server) Serve(l net.Listener) error {
	return s.newHTTPServer().Serve(l)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	authToken := s.authToken
	allowedOrigins := s.allowedOrigins
	s.mu.RUnlock()

	// Origin check first: a cross-site request never reaches dispatch.
	if len(allowedOrigins) > 0 && r.Header.Get("Origin") != "" && !containsString(allowedOrigins, r.Header.Get("Origin")) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}

	if authToken != "" {
		const prefix = "Bearer "
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, prefix) || !constantTimeEqual(strings.TrimPrefix(authz, prefix), authToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the media type strictly: "application/json" (optionally with
	// charset parameters), not look-alikes such as "application/jsonx".
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}

	// Cap the body at maxReq+1 bytes so an oversized request is detected
	// without ever buffering it fully (constant memory, like readMessage).
	// A negative MaxRequestBytes disables the cap for stdio, but the HTTP
	// transport always enforces a floor so a remote peer can never OOM
	// the process.
	s.mu.RLock()
	maxReq := s.MaxRequestBytes
	s.mu.RUnlock()
	if maxReq == 0 {
		maxReq = DefaultMaxRequestBytes
	}
	if maxReq <= 0 || maxReq > DefaultMaxRequestBytes {
		maxReq = DefaultMaxRequestBytes
	}
	if maxReq > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxReq+1)
	}
	body, err := io.ReadAll(r.Body)
	if err != nil || (maxReq > 0 && int64(len(body)) > maxReq) {
		writeRawJSONHTTP(w, marshalHTTP(NewJSONRPCError(nil, ErrCodeInvalidRequest,
			fmt.Sprintf("Invalid Request: message exceeds maximum size of %d bytes", maxReq))))
		return
	}

	// Reuse the stdio dispatch loop: feed it one newline-terminated
	// message and capture the response it writes.
	var out bytes.Buffer
	in := bytes.NewReader(append(body, '\n'))
	_ = s.RunWithIOContext(r.Context(), in, &out)

	resp := out.Bytes()
	if len(bytes.TrimSpace(resp)) == 0 {
		// The loop only writes nothing for a notification (no id).
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if wantsSSE(r.Header.Get("Accept")) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", bytes.TrimSuffix(resp, []byte("\n")))
		return
	}
	writeRawJSONHTTP(w, resp)
}

// wantsSSE reports whether the client's Accept header asks for SSE in
// preference to plain JSON.
func wantsSSE(accept string) bool {
	for _, part := range strings.Split(accept, ",") {
		mt := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if mt == "text/event-stream" {
			return true
		}
		if mt == "application/json" {
			return false
		}
	}
	return false
}

func marshalHTTP(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal marshal error"}}`)
	}
	return b
}

func writeRawJSONHTTP(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// constantTimeEqual compares tokens without leaking value information
// through timing (crypto/subtle).
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
