package gomcp

import (
	"encoding/json"
	"testing"
	"time"
)

// protocolVersionFromParams: _meta wins, then top-level, then none.
func TestProtocolVersionFromParams(t *testing.T) {
	cases := []struct {
		params string
		want   string
	}{
		{`{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"},"protocolVersion":"2024-11-05"}`, "2026-07-28"},
		{`{"protocolVersion":"2025-03-26"}`, "2025-03-26"},
		{`{}`, ""},
		{``, ""},
		{`null`, ""},
	}
	for _, tc := range cases {
		var raw json.RawMessage
		if tc.params != "" && tc.params != "null" {
			raw = json.RawMessage(tc.params)
		} else if tc.params == "null" {
			raw = json.RawMessage("null")
		}
		if got := protocolVersionFromParams(raw); got != tc.want {
			t.Errorf("params %s: got %q, want %q", tc.params, got, tc.want)
		}
	}
}

// negotiateProtocolVersion fallback chain.
func TestNegotiateProtocolVersionFallback(t *testing.T) {
	if got := negotiateProtocolVersion("2025-11-25", "fallback-ver"); got != "2025-11-25" {
		t.Errorf("supported echo = %q", got)
	}
	if got := negotiateProtocolVersion("1999-01-01", "fallback-ver"); got != "fallback-ver" {
		t.Errorf("fallback = %q", got)
	}
	if got := negotiateProtocolVersion("", ""); got != DefaultProtocolVersion {
		t.Errorf("default = %q", got)
	}
}

// handlerContext override semantics.
func TestHandlerContextOverride(t *testing.T) {
	s := &Server{}
	base := time.Second
	ctx, cancel := s.handlerContext(t.Context(), base)
	defer cancel()
	dl, ok := ctx.Deadline()
	if !ok || time.Until(dl) > base {
		t.Fatalf("deadline missing or too far: %v %v", ok, dl)
	}

	// Negative override disables the timeout entirely.
	ctx2, cancel2 := s.handlerContext(t.Context(), -1)
	defer cancel2()
	if _, ok := ctx2.Deadline(); ok {
		t.Fatal("negative override should disable deadline")
	}
}

// decodeRPCResponse: literal null result is present (len 4) and accepted
// with a nil out; it is not treated as missing.
func TestDecodeRPCResponseNullResult(t *testing.T) {
	if err := decodeRPCResponse([]byte(`{"jsonrpc":"2.0","id":1,"result":null}`), nil); err != nil {
		t.Fatalf("null result with nil out should pass, got %v", err)
	}
}
