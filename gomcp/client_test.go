package gomcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientInitializeAndCallTool(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	c := NewHTTPClient(ts.URL)

	res, err := c.Initialize(context.Background(), "test-client", "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if res.ServerInfo.Name != "http-test" {
		t.Fatalf("serverInfo.name = %q", res.ServerInfo.Name)
	}
	if err := c.NotifyInitialized(context.Background()); err != nil {
		t.Fatal(err)
	}

	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", tools)
	}

	out, err := c.CallTool(context.Background(), "echo", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("CallTool output = %q", out)
	}
}

func TestHTTPClientRPCError(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	c := NewHTTPClient(ts.URL)
	if _, err := c.Initialize(context.Background(), "t", "0"); err != nil {
		t.Fatal(err)
	}
	_, err := c.CallTool(context.Background(), "nope", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestStdioClientAgainstSubprocess(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns subprocess")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(`package main

import (
	"context"
	"fmt"

	"github.com/BackendStack21/go-mcp/gomcp"
)

func main() {
	srv := gomcp.NewServer("stdio-echo", "1.0.0")
	srv.AddTool(gomcp.Tool{
		Name: "upper",
		InputSchema: gomcp.InputSchema{Type: "object"},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("%v", args["text"]), nil
		},
	})
	srv.Run()
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "server")
	// Build from the module root so the gomcp import resolves.
	build := exec.Command("go", "build", "-o", bin, src)
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	c, err := NewStdioClient(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := c.Initialize(ctx, "t", "0")
	if err != nil {
		t.Fatal(err)
	}
	if res.ServerInfo.Name != "stdio-echo" {
		t.Fatalf("serverInfo.name = %q", res.ServerInfo.Name)
	}
	out, err := c.CallTool(ctx, "upper", map[string]any{"text": "works"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "works") {
		t.Fatalf("output = %q", out)
	}
}
