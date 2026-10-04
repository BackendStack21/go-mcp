package gomcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
