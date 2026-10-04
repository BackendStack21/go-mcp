package gomcp

import "testing"

// TestHTTPServerTimeoutsSet guards against slowloris exposure: every
// serve path must build an http.Server with bounded header/idle timeouts.
func TestHTTPServerTimeoutsSet(t *testing.T) {
	srv := NewServer("timeout-test", "1.0.0")
	hs := srv.newHTTPServer()
	if hs.ReadHeaderTimeout <= 0 {
		t.Errorf("ReadHeaderTimeout = %v, want > 0", hs.ReadHeaderTimeout)
	}
	if hs.ReadTimeout <= 0 {
		t.Errorf("ReadTimeout = %v, want > 0", hs.ReadTimeout)
	}
	if hs.IdleTimeout <= 0 {
		t.Errorf("IdleTimeout = %v, want > 0", hs.IdleTimeout)
	}
}
