package gomcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newFullTestServer registers a tool, a resource, and a prompt.
func newFullTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := NewServer("full", "1.0.0")
	srv.AddTool(Tool{
		Name:        "echo",
		InputSchema: InputSchema{Type: "object"},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("%v", args["message"]), nil
		},
	})
	srv.AddResource(Resource{
		URI:     "test://doc",
		Name:    "doc",
		Handler: func(ctx context.Context) (string, error) { return "resource-body", nil },
	})
	srv.AddPrompt(Prompt{
		Name: "p1",
		Handler: func(ctx context.Context, args map[string]any) ([]PromptMessage, error) {
			return []PromptMessage{{Role: "user", Content: fmt.Sprintf("prompt %v", args["x"])}}, nil
		},
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func mustInit(t *testing.T, c Client) {
	t.Helper()
	if _, err := c.Initialize(context.Background(), "t", "0"); err != nil {
		t.Fatal(err)
	}
	if err := c.NotifyInitialized(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// buildTestServer compiles a test subprocess server from src, from the
// module root so the gomcp import resolves, and returns the binary path.
func buildTestServer(t *testing.T, dir, src string) string {
	t.Helper()
	bin := filepath.Join(dir, "server")
	build := exec.Command("go", "build", "-o", bin, src)
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// Exercise the full HTTPClient interface surface.
func TestHTTPClientFullSurface(t *testing.T) {
	ts := newFullTestServer(t)
	c := NewHTTPClient(ts.URL)
	ctx := context.Background()
	mustInit(t, c)

	if _, err := c.ListTools(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CallTool(ctx, "echo", map[string]any{"message": "m"}); err != nil {
		t.Fatal(err)
	}

	resources, err := c.ListResources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].URI != "test://doc" {
		t.Fatalf("resources = %+v", resources)
	}
	body, err := c.ReadResource(ctx, "test://doc")
	if err != nil {
		t.Fatal(err)
	}
	if body != "resource-body" {
		t.Fatalf("body = %q", body)
	}
	// Resource that was never registered -> JSON-RPC error surfaced.
	if _, err := c.ReadResource(ctx, "test://missing"); err == nil {
		t.Fatal("expected error for unknown resource")
	}

	prompts, err := c.ListPrompts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0].Name != "p1" {
		t.Fatalf("prompts = %+v", prompts)
	}
	msgs, err := c.GetPrompt(ctx, "p1", map[string]any{"x": "arg"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Content != "prompt arg" {
		t.Fatalf("messages = %+v", msgs)
	}

	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

// HTTP error statuses surface as errors from call/notify.
func TestHTTPClientErrorPaths(t *testing.T) {
	var n int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted) // 202 for a request with an id
	}))
	defer ts.Close()
	c := NewHTTPClient(ts.URL)
	if _, err := c.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want 500 surfaced, got %v", err)
	}
	if _, err := c.ListTools(context.Background()); err == nil {
		t.Fatal("want error for 202 response to a request")
	}
}

// decodeRPCResponse error branches: broken JSON, missing result.
func TestDecodeRPCResponseErrors(t *testing.T) {
	if err := decodeRPCResponse([]byte("not json"), nil); err == nil {
		t.Fatal("want malformed-response error")
	}
	if err := decodeRPCResponse([]byte(`{"jsonrpc":"2.0","id":1}`), nil); err == nil {
		t.Fatal("want missing-result error")
	}
	var out struct{ A int }
	if err := decodeRPCResponse([]byte(`{"jsonrpc":"2.0","id":1,"result":"str-not-obj"}`), &out); err == nil {
		t.Fatal("want decode error")
	}
	// Happy path with nil out and a plain error envelope.
	if err := decodeRPCResponse([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`), nil); err != nil {
		t.Fatal(err)
	}
	if err := decodeRPCResponse([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-1,"message":"x"}}`), nil); err == nil {
		t.Fatal("want rpc error")
	}
}

// sseData handles "data:" without space and multi-line bodies.
func TestSSEDataVariants(t *testing.T) {
	if got := string(sseData([]byte("data:{\"a\":1}\n"))); got != `{"a":1}` {
		t.Fatalf("no-space variant = %q", got)
	}
	if got := string(sseData([]byte(": ping\ndata: one\ndata: two\n"))); got != "one" {
		t.Fatalf("first data line = %q", got)
	}
}

// marshalHTTP never fails for JSONRPCError; exercise it directly anyway.
func TestMarshalHTTP(t *testing.T) {
	b := marshalHTTP(NewJSONRPCError(1, -32600, "x"))
	if !strings.Contains(string(b), "-32600") {
		t.Fatalf("marshalled = %s", b)
	}
}

// Client interface compliance checks.
func TestClientInterfaceCompliance(t *testing.T) {
	var _ Client = (*HTTPClient)(nil)
	var _ Client = (*StdioClient)(nil)
}
