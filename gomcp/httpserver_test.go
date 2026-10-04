package gomcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newHTTPTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	srv := NewServer("http-test", "1.0.0")
	srv.AddTool(Tool{
		Name:        "echo",
		Description: "Echo back the message",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{"message": {Type: "string"}}},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("%v", args["message"]), nil
		},
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func postJSON(t *testing.T, url string, body string, accept string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(b)
}

func TestHTTPInitialize(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	resp, body := postJSON(t, ts.URL, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`, "application/json, text/event-stream")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	var out struct {
		Result struct {
			ProtocolVersion string                `json:"protocolVersion"`
			ServerInfo      struct{ Name string } `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("bad JSON: %v (%s)", err, body)
	}
	if out.Result.ProtocolVersion != "2025-11-25" {
		t.Fatalf("protocolVersion = %q", out.Result.ProtocolVersion)
	}
	if out.Result.ServerInfo.Name != "http-test" {
		t.Fatalf("serverInfo.name = %q", out.Result.ServerInfo.Name)
	}
}

func TestHTTPToolsCall(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	// initialize first, as clients do
	postJSON(t, ts.URL, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`, "application/json")
	_, body := postJSON(t, ts.URL, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"message":"hi"}}}`, "application/json")
	if !strings.Contains(body, `"hi"`) {
		t.Fatalf("echo result missing: %s", body)
	}
}

func TestHTTPSSEResponse(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q", ct)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "data: ") || !strings.Contains(string(b), `"result"`) {
		t.Fatalf("SSE body missing response: %q", string(b))
	}
}

func TestHTTPMethodNotAllowed(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		req, _ := http.NewRequest(method, ts.URL, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, resp.StatusCode)
		}
		if resp.Header.Get("Allow") != http.MethodPost {
			t.Errorf("%s: Allow = %q, want POST", method, resp.Header.Get("Allow"))
		}
	}
}

func TestHTTPBadJSON(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	resp, body := postJSON(t, ts.URL, `{not json`, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "-32700") {
		t.Fatalf("want -32700 parse error, got: %s", body)
	}
}

func TestHTTPNotificationReturns202(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
}

func TestHTTPOversizedBody(t *testing.T) {
	srv, _ := newHTTPTestServer(t)
	srv.MaxRequestBytes = 128
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	big := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + strings.Repeat("x", 1024) + `"}}`
	resp, body := postJSON(t, ts.URL, big, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "-32600") {
		t.Fatalf("want -32600, got: %s", body)
	}
}

func TestHTTPWrongContentType(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	resp, err := http.Post(ts.URL, "text/plain", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
}

func TestHTTPListenAndServe(t *testing.T) {
	// Grab a free port, close the listener, then serve on it for real.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	srv := NewServer("listen-test", "1.0.0")
	go func() { _ = srv.ListenAndServe(addr) }()

	// Retry until the listener is up.
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server never started listening")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, body := postJSON(t, "http://"+addr, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, "application/json")
	if !strings.Contains(body, `"result"`) {
		t.Fatalf("ping failed over real listener: %s", body)
	}
}
