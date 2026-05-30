package gomcp

import (
	"context"
	"io"
	"strings"
	"testing"
)

// newInitializedServer returns a server marked as initialized so benchmarks can
// exercise post-handshake request handling directly.
func newInitializedServer() *Server {
	srv := NewServer("bench-server", "1.0.0")
	srv.initialized = true
	return srv
}

// runOnce feeds a single request followed by EOF through the server, discarding
// the encoded output. It models the per-request decode → dispatch → encode path.
func runOnce(b *testing.B, srv *Server, req string) {
	b.Helper()
	if err := srv.RunWithIO(strings.NewReader(req), io.Discard); err != nil {
		b.Fatalf("RunWithIO: %v", err)
	}
}

func BenchmarkInitialize(b *testing.B) {
	srv := NewServer("bench-server", "1.0.0")
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runOnce(b, srv, req)
	}
}

func BenchmarkToolsList(b *testing.B) {
	srv := newInitializedServer()
	for i := 0; i < 16; i++ {
		name := "tool" + string(rune('a'+i))
		srv.AddTool(Tool{
			Name:        name,
			Description: "a benchmark tool",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"value": {Type: "string", Description: "the value"},
				},
				Required: []string{"value"},
			},
		})
	}
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runOnce(b, srv, req)
	}
}

func BenchmarkToolsCall(b *testing.B) {
	srv := newInitializedServer()
	srv.AddTool(Tool{
		Name: "echo",
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			return "hello world", nil
		},
	})
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}` + "\n"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runOnce(b, srv, req)
	}
}
