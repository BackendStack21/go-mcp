package gomcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// slowServer replies to tool "slow" only after a delay, controlled by the
// argument. Everything else replies instantly.
const slowServerSrc = `package main

import (
	"context"
	"fmt"
	"time"

	"github.com/BackendStack21/go-mcp/gomcp"
)

func main() {
	srv := gomcp.NewServer("slow", "1.0.0")
	srv.AddTool(gomcp.Tool{
		Name: "slow",
		InputSchema: gomcp.InputSchema{Type: "object"},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			ms := 50
			if v, ok := args["ms"].(float64); ok {
				ms = int(v)
			}
			time.Sleep(time.Duration(ms) * time.Millisecond)
			return fmt.Sprintf("slept %dms", ms), nil
		},
	})
	srv.Run()
}
`

func buildSlowServer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(slowServerSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "slowserver")
	build := exec.Command("go", "build", "-o", bin, src)
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// TestStdioClientTimeoutDoesNotDesync: a call that times out must not
// corrupt the stream — the NEXT call must still get its OWN response,
// not the abandoned one.
func TestStdioClientTimeoutDoesNotDesync(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns subprocess")
	}
	bin := buildSlowServer(t)
	c, err := NewStdioClient(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := context.Background()

	if _, err := c.Initialize(ctx, "t", "0"); err != nil {
		t.Fatal(err)
	}

	// Call 1: 2s server-side delay, 100ms client deadline -> timeout.
	tctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if _, err := c.CallTool(tctx, "slow", map[string]any{"ms": 2000}); err == nil {
		t.Fatal("expected timeout error")
	}

	// Call 2 must succeed and receive ITS OWN result, not the stale one.
	done := make(chan string, 1)
	go func() {
		out, err := c.CallTool(ctx, "slow", map[string]any{"ms": 10})
		if err != nil {
			done <- "err:" + err.Error()
			return
		}
		done <- out
	}()
	select {
	case got := <-done:
		if got != "slept 10ms" {
			t.Fatalf("second call got stale/desynced response: %q", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("second call never returned — stream desynced after timeout")
	}
}

// TestStdioClientCloseDuringInflight: Close while a call is in flight must
// return without hanging and without racing the reader.
func TestStdioClientCloseDuringInflight(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns subprocess")
	}
	bin := buildSlowServer(t)
	c, err := NewStdioClient(bin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Initialize(context.Background(), "t", "0"); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.CallTool(context.Background(), "slow", map[string]any{"ms": 3000})
	}()
	time.Sleep(100 * time.Millisecond)

	closed := make(chan error, 1)
	go func() { closed <- c.Close() }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung while a call was in flight")
	}
	<-done
}
