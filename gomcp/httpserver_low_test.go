package gomcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// LOW sweep regression tests.

// application/jsonx and other look-alikes must be rejected (415),
// while application/json; charset=utf-8 is accepted.
func TestHTTPStrictContentType(t *testing.T) {
	srv := NewServer("ct", "1.0.0")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	for _, ct := range []string{"application/jsonx", "text/json", "application/json-patch"} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(testPing))
		req.Header.Set("Content-Type", ct)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type %q: status = %d, want 415", ct, resp.StatusCode)
		}
	}

	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(testPing))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("charset parameter: status = %d, want 200", resp.StatusCode)
	}
}

// A negative MaxRequestBytes disables the stdio cap, but HTTP must still
// enforce the DefaultMaxRequestBytes floor.
func TestHTTPBodyCapFloor(t *testing.T) {
	srv := NewServer("floor", "1.0.0")
	srv.MaxRequestBytes = -1 // "uncapped" for stdio
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	big := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` +
		strings.Repeat("x", int(DefaultMaxRequestBytes)+1024) + `"}}`
	resp, body := postJSON(t, ts.URL, big, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "-32600") {
		t.Fatalf("want -32600 for oversized body despite negative MaxRequestBytes, got: %.100s", body)
	}
}
