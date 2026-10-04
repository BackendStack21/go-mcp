package gomcp

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

// TestE2EHTTPLoopback exercises the HTTP server and HTTP client together
// over a real loopback listener: handshake, tool listing, tool call,
// resource read, and prompt rendering end to end.
func TestE2EHTTPLoopback(t *testing.T) {
	if testing.Short() {
		t.Skip("binds a real listener")
	}

	srv := NewServer("e2e-http", "1.0.0")
	srv.AddTool(Tool{
		Name:        "greet",
		Description: "Greet someone",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{"name": {Type: "string"}}},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("hello %v", args["name"]), nil
		},
	})
	srv.AddResource(Resource{
		URI:         "config://app",
		Name:        "app-config",
		Description: "App configuration",
		MimeType:    "application/json",
		Handler: func(ctx context.Context) (string, error) {
			return `{"mode":"e2e"}`, nil
		},
	})
	srv.AddPrompt(Prompt{
		Name:        "review",
		Description: "Review code",
		Handler: func(ctx context.Context, args map[string]any) ([]PromptMessage, error) {
			return []PromptMessage{{Role: "user", Content: fmt.Sprintf("review %v", args["file"])}}, nil
		},
	})

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(l) }()
	defer func() { <-done }()
	defer l.Close()

	c := NewHTTPClient("http://" + l.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := c.Initialize(ctx, "e2e-client", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if res.ServerInfo.Name != "e2e-http" {
		t.Fatalf("serverInfo.name = %q", res.ServerInfo.Name)
	}
	if err := c.NotifyInitialized(ctx); err != nil {
		t.Fatal(err)
	}

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "greet" {
		t.Fatalf("tools = %+v", tools)
	}

	out, err := c.CallTool(ctx, "greet", map[string]any{"name": "e2e"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello e2e" {
		t.Fatalf("tool output = %q", out)
	}

	body, err := c.ReadResource(ctx, "config://app")
	if err != nil {
		t.Fatal(err)
	}
	if body != `{"mode":"e2e"}` {
		t.Fatalf("resource body = %q", body)
	}

	msgs, err := c.GetPrompt(ctx, "review", map[string]any{"file": "main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Content != "review main.go" {
		t.Fatalf("prompt messages = %+v", msgs)
	}
}
