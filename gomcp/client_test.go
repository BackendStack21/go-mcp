package gomcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

// authRecorder is a stub server recording the Authorization header of every
// request and answering with a canned status.
func authRecorder(t *testing.T, status int, challenge string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	seen := []string{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		if challenge != "" {
			w.Header().Set("WWW-Authenticate", challenge)
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("denied"))
			return
		}
		var req struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[]}}`, req.ID)
	}))
	t.Cleanup(ts.Close)
	return ts, &seen
}

func TestHTTPClientAuthProviderSetsHeader(t *testing.T) {
	ts, seen := authRecorder(t, http.StatusOK, "")
	c := NewHTTPClient(ts.URL)
	c.SetAuthProvider(func() string { return "Bearer xyz" })
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0] != "Bearer xyz" {
		t.Fatalf("server saw %q, want [Bearer xyz]", *seen)
	}
}

func TestHTTPClientAuthProviderOverridesStaticToken(t *testing.T) {
	ts, seen := authRecorder(t, http.StatusOK, "")
	c := NewHTTPClientWithToken(ts.URL, "static")
	c.SetAuthProvider(func() string { return "Bearer dyn" })
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0] != "Bearer dyn" {
		t.Fatalf("server saw %q, want [Bearer dyn]", *seen)
	}
}

func TestHTTPClientAuthProviderCalledPerRequest(t *testing.T) {
	ts, seen := authRecorder(t, http.StatusOK, "")
	c := NewHTTPClient(ts.URL)
	n := 0
	c.SetAuthProvider(func() string { n++; return fmt.Sprintf("Bearer t%d", n) })
	for i := 0; i < 2; i++ {
		if _, err := c.ListTools(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(*seen) != 2 || (*seen)[0] != "Bearer t1" || (*seen)[1] != "Bearer t2" {
		t.Fatalf("server saw %q, want [Bearer t1 Bearer t2]", *seen)
	}
}

func TestHTTPClientAuthProviderOnNotify(t *testing.T) {
	ts, seen := authRecorder(t, http.StatusOK, "")
	c := NewHTTPClient(ts.URL)
	c.SetAuthProvider(func() string { return "Bearer note" })
	if err := c.NotifyInitialized(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0] != "Bearer note" {
		t.Fatalf("server saw %q, want [Bearer note]", *seen)
	}
}

func TestHTTPClientWithAuthHeaderChains(t *testing.T) {
	ts, seen := authRecorder(t, http.StatusOK, "")
	c := NewHTTPClient(ts.URL)
	got := c.WithAuthHeader(func() string { return "Bearer chained" })
	if got != c {
		t.Fatal("WithAuthHeader must return the same client")
	}
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0] != "Bearer chained" {
		t.Fatalf("server saw %q, want [Bearer chained]", *seen)
	}
}

func TestHTTPClientAuthProviderEmptyFallsBackToStatic(t *testing.T) {
	ts, seen := authRecorder(t, http.StatusOK, "")
	c := NewHTTPClientWithToken(ts.URL, "static")
	c.SetAuthProvider(func() string { return "" })
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0] != "Bearer static" {
		t.Fatalf("server saw %q, want [Bearer static]", *seen)
	}
}

func TestHTTPClientAuthProviderEmptyNoTokenSendsNoHeader(t *testing.T) {
	ts, seen := authRecorder(t, http.StatusOK, "")
	c := NewHTTPClient(ts.URL)
	c.SetAuthProvider(func() string { return "" })
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0] != "" {
		t.Fatalf("server saw %q, want [\"\"]", *seen)
	}
}

func TestHTTPClientSetAuthProviderNilClears(t *testing.T) {
	ts, seen := authRecorder(t, http.StatusOK, "")
	c := NewHTTPClientWithToken(ts.URL, "static")
	c.SetAuthProvider(func() string { return "Bearer dyn" })
	c.SetAuthProvider(nil)
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0] != "Bearer static" {
		t.Fatalf("server saw %q, want [Bearer static] after nil clear", *seen)
	}
}

func TestHTTPClient401IsAuthError(t *testing.T) {
	ts, _ := authRecorder(t, http.StatusUnauthorized,
		`Bearer realm="mcp", resource_metadata="https://x/meta"`)
	c := NewHTTPClient(ts.URL)
	_, err := c.ListTools(context.Background())
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v (%T), want *AuthError", err, err)
	}
	if ae.ResourceMetadata != "https://x/meta" {
		t.Fatalf("ResourceMetadata = %q", ae.ResourceMetadata)
	}
	if ae.Body != "denied" {
		t.Fatalf("Body = %q, want denied", ae.Body)
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatal("errors.Is(err, ErrUnauthorized) = false")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("Error() = %q, want it to contain 401", err.Error())
	}
}

func TestHTTPClient401WithoutResourceMetadata(t *testing.T) {
	for _, ch := range []string{`Bearer realm="mcp"`, ""} {
		ts, _ := authRecorder(t, http.StatusUnauthorized, ch)
		c := NewHTTPClient(ts.URL)
		_, err := c.ListTools(context.Background())
		var ae *AuthError
		if !errors.As(err, &ae) {
			t.Fatalf("challenge %q: err = %v, want *AuthError", ch, err)
		}
		if ae.ResourceMetadata != "" {
			t.Fatalf("challenge %q: ResourceMetadata = %q, want empty", ch, ae.ResourceMetadata)
		}
	}
}

func TestHTTPClient401OnNotify(t *testing.T) {
	ts, _ := authRecorder(t, http.StatusUnauthorized,
		`Bearer realm="mcp", resource_metadata="https://x/meta"`)
	c := NewHTTPClient(ts.URL)
	err := c.NotifyInitialized(context.Background())
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v (%T), want *AuthError", err, err)
	}
	if ae.ResourceMetadata != "https://x/meta" || !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ae = %+v, Is = %v", ae, errors.Is(err, ErrUnauthorized))
	}
}

func TestHTTPClient500IsNotAuthError(t *testing.T) {
	ts, _ := authRecorder(t, http.StatusInternalServerError, "")
	c := NewHTTPClient(ts.URL)
	_, err := c.ListTools(context.Background())
	if err == nil {
		t.Fatal("want error")
	}
	var ae *AuthError
	if errors.As(err, &ae) || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("500 must not be an AuthError: %v", err)
	}
	if !strings.Contains(err.Error(), "http 500") {
		t.Fatalf("Error() = %q, want it to contain \"http 500\"", err.Error())
	}
}
