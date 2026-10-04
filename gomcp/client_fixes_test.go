package gomcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// M2: token-authenticated HTTP client works against a token-protected server.
func TestHTTPClientWithToken(t *testing.T) {
	srv := NewServer("tok", "1.0.0")
	srv.SetAuthToken("s3cret")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Anonymous client is rejected at handshake.
	if _, err := NewHTTPClient(ts.URL).Initialize(t.Context(), "t", "0"); err == nil {
		t.Fatal("anonymous client should fail against protected server")
	}

	// Token client succeeds end to end.
	c := NewHTTPClientWithToken(ts.URL, "s3cret")
	res, err := c.Initialize(t.Context(), "t", "0")
	if err != nil {
		t.Fatal(err)
	}
	if res.ServerInfo.Name != "tok" {
		t.Fatalf("serverInfo.name = %q", res.ServerInfo.Name)
	}
	if err := c.NotifyInitialized(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// M1: notify surfaces HTTP errors instead of swallowing them.
func TestHTTPClientNotifySurfacesErrors(t *testing.T) {
	srv := NewServer("notify-err", "1.0.0")
	srv.SetAuthToken("k")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	err := NewHTTPClient(ts.URL).NotifyInitialized(t.Context())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("want 401 surfaced from notify, got %v", err)
	}
}

// M4: unsupported negotiated protocol version is rejected.
func TestClientRejectsUnsupportedProtocolVersion(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"1999-01-01","serverInfo":{"name":"x","version":"0"}}}`))
	}))
	defer ts.Close()
	if _, err := NewHTTPClient(ts.URL).Initialize(t.Context(), "t", "0"); err == nil {
		t.Fatal("expected unsupported-protocol-version error")
	}
}
