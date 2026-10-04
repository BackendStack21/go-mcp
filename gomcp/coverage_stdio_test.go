package gomcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fullServerSrc registers a tool, resource, and prompt for stdio tests.
const fullServerSrc = `package main

import (
	"context"
	"fmt"

	"github.com/BackendStack21/go-mcp/gomcp"
)

func main() {
	srv := gomcp.NewServer("stdio-full", "1.0.0")
	srv.AddTool(gomcp.Tool{
		Name: "upper",
		InputSchema: gomcp.InputSchema{Type: "object"},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("%v", args["text"]), nil
		},
	})
	srv.AddResource(gomcp.Resource{
		URI:  "test://doc",
		Name: "doc",
		Handler: func(ctx context.Context) (string, error) {
			return "res-body", nil
		},
	})
	srv.AddPrompt(gomcp.Prompt{
		Name: "p1",
		Handler: func(ctx context.Context, args map[string]any) ([]gomcp.PromptMessage, error) {
			return []gomcp.PromptMessage{{Role: "user", Content: "prompt-body"}}, nil
		},
	})
	srv.Run()
}
`

func buildFullServer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(fullServerSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	return buildTestServer(t, dir, src)
}

// Exercise the full StdioClient interface surface.
func TestStdioClientFullSurface(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns subprocess")
	}
	c, err := NewStdioClient(buildFullServer(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := c.Initialize(ctx, "t", "0"); err != nil {
		t.Fatal(err)
	}
	if err := c.NotifyInitialized(ctx); err != nil {
		t.Fatal(err)
	}

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %+v", tools)
	}
	out, err := c.CallTool(ctx, "upper", map[string]any{"text": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "hi" {
		t.Fatalf("out = %q", out)
	}

	resources, err := c.ListResources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 {
		t.Fatalf("resources = %+v", resources)
	}
	body, err := c.ReadResource(ctx, "test://doc")
	if err != nil {
		t.Fatal(err)
	}
	if body != "res-body" {
		t.Fatalf("body = %q", body)
	}
	if _, err := c.ReadResource(ctx, "test://missing"); err == nil {
		t.Fatal("expected error for unknown resource")
	}

	prompts, err := c.ListPrompts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 {
		t.Fatalf("prompts = %+v", prompts)
	}
	msgs, err := c.GetPrompt(ctx, "p1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Content != "prompt-body" {
		t.Fatalf("messages = %+v", msgs)
	}
}

// StdioClient call after Close must fail, not hang.
func TestStdioClientCallAfterClose(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns subprocess")
	}
	c, err := NewStdioClient(buildFullServer(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil && !strings.Contains(err.Error(), "signal") {
		t.Fatalf("Close returned unexpected error: %v", err)
	}
	if _, err := c.ListTools(context.Background()); err == nil {
		t.Fatal("expected error after Close")
	}
}

// A call against a dead server (killed externally) fails via failAllPending.
func TestStdioClientServerDeath(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns subprocess")
	}
	c, err := NewStdioClient(buildFullServer(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Initialize(context.Background(), "t", "0"); err != nil {
		t.Fatal(err)
	}
	if err := c.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err = c.ListTools(context.Background())
		if err != nil {
			break // server death surfaced as expected
		}
		if time.Now().After(deadline) {
			t.Fatal("calls kept succeeding after server death")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
