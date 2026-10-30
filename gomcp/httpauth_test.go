package gomcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

const testPing = `{"jsonrpc":"2.0","id":1,"method":"ping"}`

func TestHTTPAuthTokenRequired(t *testing.T) {
	srv := NewServer("auth-test", "1.0.0")
	srv.SetAuthToken("secret-token")
	ts := newAuthTestServer(t, srv)

	// No credentials -> 401 with WWW-Authenticate.
	resp, body := postJSON(t, ts.URL, testPing, "application/json")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body: %s)", resp.StatusCode, body)
	}
	if wa := resp.Header.Get("WWW-Authenticate"); !strings.HasPrefix(wa, "Bearer") {
		t.Fatalf("WWW-Authenticate = %q, want Bearer challenge", wa)
	}

	// Wrong token -> 401.
	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(testPing))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong")
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	if r2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: status = %d, want 401", r2.StatusCode)
	}

	// Correct token -> 200 with result.
	req2, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(testPing))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer secret-token")
	r3, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Body.Close()
	if r3.StatusCode != http.StatusOK {
		t.Fatalf("correct token: status = %d, want 200", r3.StatusCode)
	}
}

func TestHTTPAuthDisabledByDefault(t *testing.T) {
	srv := NewServer("noauth", "1.0.0")
	ts := newAuthTestServer(t, srv)
	resp, _ := postJSON(t, ts.URL, testPing, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 without auth", resp.StatusCode)
	}
}

func TestHTTPOriginAllowlist(t *testing.T) {
	srv := NewServer("origin-test", "1.0.0")
	srv.SetAllowedOrigins([]string{"https://claude.ai", "http://localhost:3000"})
	ts := newAuthTestServer(t, srv)

	cases := []struct {
		origin string
		want   int
	}{
		{"https://claude.ai", http.StatusOK},
		{"http://localhost:3000", http.StatusOK},
		{"https://evil.example", http.StatusForbidden},
		{"", http.StatusOK}, // non-browser clients send no Origin
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(testPing))
		req.Header.Set("Content-Type", "application/json")
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("origin %q: status = %d, want %d", tc.origin, resp.StatusCode, tc.want)
		}
	}
}

func TestHTTPOriginUncheckedByDefault(t *testing.T) {
	srv := NewServer("noorigin", "1.0.0")
	ts := newAuthTestServer(t, srv)
	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(testPing))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://anything.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (no allowlist set)", resp.StatusCode)
	}
}

func newAuthTestServer(t *testing.T, srv *Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// authPost sends a JSON-RPC ping to url with optional headers and returns
// the response (body closed) and its body text.
func authPost(t *testing.T, url string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(testPing))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func wantChallenge(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); got != want {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestAuthFuncDenyAll(t *testing.T) {
	srv := NewServer("af", "1.0.0")
	srv.SetAuthFunc(func(r *http.Request) bool { return false })
	ts := newAuthTestServer(t, srv)
	resp, _ := authPost(t, ts.URL, nil)
	wantChallenge(t, resp, `Bearer realm="mcp"`)
}

func TestAuthFuncReadsRequestHeader(t *testing.T) {
	srv := NewServer("af", "1.0.0")
	srv.SetAuthFunc(func(r *http.Request) bool { return r.Header.Get("X-Key") == "k" })
	ts := newAuthTestServer(t, srv)

	resp, body := authPost(t, ts.URL, map[string]string{"X-Key": "k"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("with key: status = %d, want 200", resp.StatusCode)
	}
	var rpc struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &rpc); err != nil || rpc.Result == nil {
		t.Fatalf("with key: body %q is not a JSON-RPC result (err %v)", body, err)
	}

	resp, _ = authPost(t, ts.URL, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without key: status = %d, want 401", resp.StatusCode)
	}
}

func TestAuthFuncOverridesStaticToken(t *testing.T) {
	srv := NewServer("af", "1.0.0")
	srv.SetAuthToken("tok")
	srv.SetAuthFunc(func(r *http.Request) bool { return true })
	ts := newAuthTestServer(t, srv)
	resp, _ := authPost(t, ts.URL, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("allow-all func, no token: status = %d, want 200", resp.StatusCode)
	}

	srv.SetAuthFunc(func(r *http.Request) bool { return false })
	resp, _ = authPost(t, ts.URL, map[string]string{"Authorization": "Bearer tok"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("deny-all func, valid token: status = %d, want 401", resp.StatusCode)
	}

	srv.SetAuthFunc(nil)
	resp, _ = authPost(t, ts.URL, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("func cleared, no token: status = %d, want 401", resp.StatusCode)
	}
	resp, _ = authPost(t, ts.URL, map[string]string{"Authorization": "Bearer tok"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("func cleared, valid token: status = %d, want 200", resp.StatusCode)
	}
}

func TestResourceMetadataURLChallenge(t *testing.T) {
	const meta = "https://x/.well-known/oauth-protected-resource"
	want := `Bearer realm="mcp", resource_metadata="` + meta + `"`

	// Static token path: missing and wrong token.
	srv := NewServer("rm", "1.0.0")
	srv.SetAuthToken("tok")
	srv.SetResourceMetadataURL(meta)
	ts := newAuthTestServer(t, srv)
	resp, _ := authPost(t, ts.URL, nil)
	wantChallenge(t, resp, want)
	resp, _ = authPost(t, ts.URL, map[string]string{"Authorization": "Bearer wrong"})
	wantChallenge(t, resp, want)

	// Auth func path.
	srv2 := NewServer("rm2", "1.0.0")
	srv2.SetAuthFunc(func(r *http.Request) bool { return false })
	srv2.SetResourceMetadataURL(meta)
	ts2 := newAuthTestServer(t, srv2)
	resp, _ = authPost(t, ts2.URL, nil)
	wantChallenge(t, resp, want)

	// Empty restores the plain challenge.
	srv2.SetResourceMetadataURL("")
	resp, _ = authPost(t, ts2.URL, nil)
	wantChallenge(t, resp, `Bearer realm="mcp"`)
}

func TestResourceMetadataURLEscaping(t *testing.T) {
	srv := NewServer("rm", "1.0.0")
	srv.SetAuthToken("tok")
	srv.SetResourceMetadataURL(`https://x/a"b\c`)
	ts := newAuthTestServer(t, srv)
	resp, _ := authPost(t, ts.URL, nil)
	wantChallenge(t, resp, `Bearer realm="mcp", resource_metadata="https://x/a\"b\\c"`)
}

func TestResourceMetadataURLUnsetKeepsPlainChallenge(t *testing.T) {
	srv := NewServer("rm", "1.0.0")
	srv.SetAuthToken("tok")
	ts := newAuthTestServer(t, srv)
	resp, _ := authPost(t, ts.URL, nil)
	wantChallenge(t, resp, `Bearer realm="mcp"`)
}

const wellKnownPath = "/.well-known/oauth-protected-resource"

func newMetaServer(meta map[string]any) *Server {
	srv := NewServer("meta-test", "1.0.0")
	srv.SetAuthToken("tok")
	srv.SetAllowedOrigins([]string{"https://ok"})
	srv.SetProtectedResourceMetadata(meta)
	return srv
}

func TestProtectedResourceMetadataServedUnauthenticated(t *testing.T) {
	meta := map[string]any{
		"resource":              "https://x",
		"authorization_servers": []any{"https://as"},
	}
	ts := newAuthTestServer(t, newMetaServer(meta))

	req, err := http.NewRequest(http.MethodGet, ts.URL+wellKnownPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, body)
	}
	if !reflect.DeepEqual(got, meta) {
		t.Fatalf("body = %v, want %v", got, meta)
	}
}

func TestProtectedResourceMetadataCopiedAtSetTime(t *testing.T) {
	meta := map[string]any{"resource": "https://x"}
	ts := newAuthTestServer(t, newMetaServer(meta))
	meta["resource"] = "https://mutated"

	resp, err := http.Get(ts.URL + wellKnownPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v (status %d)", err, resp.StatusCode)
	}
	if got["resource"] != "https://x" {
		t.Fatalf("resource = %v, want https://x (map must be copied at set time)", got["resource"])
	}
}

func TestProtectedResourceMetadataMCPEndpointUnaffected(t *testing.T) {
	ts := newAuthTestServer(t, newMetaServer(map[string]any{"resource": "https://x"}))
	resp, body := authPost(t, ts.URL+"/", map[string]string{"Authorization": "Bearer tok"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"result"`) {
		t.Fatalf("body = %s, want JSON-RPC result", body)
	}
}

func TestProtectedResourceMetadataUnsetGetIs405(t *testing.T) {
	srv := NewServer("meta-unset", "1.0.0")
	ts := newAuthTestServer(t, srv)
	resp, err := http.Get(ts.URL + wellKnownPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestProtectedResourceMetadataPostNotIntercepted(t *testing.T) {
	ts := newAuthTestServer(t, newMetaServer(map[string]any{"resource": "https://x"}))
	resp, body := authPost(t, ts.URL+wellKnownPath, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body: %s)", resp.StatusCode, body)
	}
}

func TestHTTPClientOAuthDiscoveryEndToEnd(t *testing.T) {
	var base string
	srv := NewServer("oauth-e2e", "1.0.0")
	var mu sync.Mutex
	valid := "tok-1"
	srv.SetAuthFunc(func(r *http.Request) bool {
		mu.Lock()
		defer mu.Unlock()
		return r.Header.Get("Authorization") == "Bearer "+valid
	})
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.Start()
	t.Cleanup(ts.Close)
	base = ts.URL
	meta := map[string]any{
		"resource":              base,
		"authorization_servers": []any{"https://as.example"},
	}
	srv.SetResourceMetadataURL(base + "/.well-known/oauth-protected-resource")
	srv.SetProtectedResourceMetadata(meta)

	// Unauthenticated client is challenged with a typed error.
	anon := NewHTTPClient(base)
	_, err := anon.ListTools(context.Background())
	var ae *AuthError
	if !errors.As(err, &ae) || !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v (%T), want *AuthError", err, err)
	}
	if ae.ResourceMetadata != base+"/.well-known/oauth-protected-resource" {
		t.Fatalf("ResourceMetadata = %q", ae.ResourceMetadata)
	}

	// Discovery: fetch the advertised metadata document.
	resp, err := http.Get(ae.ResourceMetadata)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metadata status = %d", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, meta) {
		t.Fatalf("metadata = %v, want %v", got, meta)
	}

	// A client with a rotating provider succeeds across rotations.
	c := NewHTTPClient(base)
	token := "tok-1"
	c.SetAuthProvider(func() string { return "Bearer " + token })
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatalf("first call: %v", err)
	}
	mu.Lock()
	valid = "tok-2"
	mu.Unlock()
	if _, err := c.ListTools(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("stale token: err = %v, want ErrUnauthorized", err)
	}
	token = "tok-2"
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatalf("after rotation: %v", err)
	}
	if err := c.NotifyInitialized(context.Background()); err != nil {
		t.Fatalf("notify after rotation: %v", err)
	}
}
