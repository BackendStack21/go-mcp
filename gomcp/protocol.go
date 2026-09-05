package gomcp

import (
	"encoding/json"
	"errors"
	"strconv"
)

// Published MCP protocol versions this server can speak. Newest first —
// server/discover advertises them in this order so clients can pick the
// highest mutually supported revision.
const (
	ProtocolVersion20260728 = "2026-07-28"
	ProtocolVersion20251125 = "2025-11-25"
	ProtocolVersion20250326 = "2025-03-26"
	ProtocolVersion20241105 = "2024-11-05"
)

// DefaultProtocolVersion is the MCP protocol version this server speaks
// when the client does not request a specific revision.
const DefaultProtocolVersion = ProtocolVersion20260728

// SupportedProtocolVersions is the set of protocol revisions this server
// can negotiate. 2026-07-28 is the current spec; older dates remain so
// existing clients that still send initialize keep working.
var SupportedProtocolVersions = []string{
	ProtocolVersion20260728,
	ProtocolVersion20251125,
	ProtocolVersion20250326,
	ProtocolVersion20241105,
}

// JSON-RPC / MCP application error codes.
const (
	// ErrCodeParse is JSON-RPC -32700 (broken JSON).
	ErrCodeParse = -32700
	// ErrCodeInvalidRequest is JSON-RPC -32600.
	ErrCodeInvalidRequest = -32600
	// ErrCodeMethodNotFound is JSON-RPC -32601.
	ErrCodeMethodNotFound = -32601
	// ErrCodeInvalidParams is JSON-RPC -32602.
	ErrCodeInvalidParams = -32602
	// ErrCodeApplication is an implementation-defined server error.
	ErrCodeApplication = -32000
	// ErrCodeUnsupportedProtocolVersion is MCP -32022 (SEP-2575).
	ErrCodeUnsupportedProtocolVersion = -32022
)

// DefaultListTTLMs is the cache freshness hint (SEP-2549) advertised on
// list and discover results when Server.ListTTLMs is unset.
const DefaultListTTLMs int64 = 60_000

const (
	resultTypeComplete = "complete"
	cacheScopePublic   = "public"
	cacheScopePrivate  = "private"
)

// protocolVersionFromParams reads the version a client declared. 2026-07-28
// clients put it in params._meta; initialize (legacy) puts it at the top
// level of params. An empty string means the client did not declare one.
func protocolVersionFromParams(params json.RawMessage) string {
	if ver := metaProtocolVersion(params); ver != "" {
		return ver
	}
	if len(params) == 0 {
		return ""
	}
	var envelope struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return ""
	}
	return envelope.ProtocolVersion
}

// metaProtocolVersion reads only the 2026-07-28 per-request _meta version.
// A missing key is not an error — legacy clients omit _meta entirely.
func metaProtocolVersion(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var envelope struct {
		Meta struct {
			ProtocolVersion string `json:"io.modelcontextprotocol/protocolVersion"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return ""
	}
	return envelope.Meta.ProtocolVersion
}

// cursorFromParams reads the optional pagination cursor from list params.
func cursorFromParams(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var envelope struct {
		Cursor string `json:"cursor"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return ""
	}
	return envelope.Cursor
}

// isSupportedProtocolVersion reports whether v is one of the revisions
// this server can speak.
func isSupportedProtocolVersion(v string) bool {
	for _, s := range SupportedProtocolVersions {
		if s == v {
			return true
		}
	}
	return false
}

// negotiateProtocolVersion picks the version the server will use for a
// legacy initialize handshake. A supported client version is echoed so
// older clients keep seeing the revision they asked for. Anything else
// (empty or unknown) falls back to the server's configured default.
func negotiateProtocolVersion(requested, fallback string) string {
	if isSupportedProtocolVersion(requested) {
		return requested
	}
	if fallback != "" {
		return fallback
	}
	return DefaultProtocolVersion
}

// speaks2026 reports whether this request is using the 2026-07-28 wire
// shape. Legacy clients omit _meta entirely; their responses stay in the
// pre-2026 shape so extra fields cannot trip a strict decoder.
func speaks2026(params json.RawMessage) bool {
	return metaProtocolVersion(params) == ProtocolVersion20260728
}

// paginate returns items[start:end] and an opaque next cursor. pageSize
// <= 0 means "return the rest" (the historical one-shot list). An empty
// or missing cursor starts at 0. A cursor that is not a decimal offset
// in [0, len(items)] is rejected.
func paginate[T any](items []T, cursor string, pageSize int) (page []T, next string, err error) {
	start := 0
	if cursor != "" {
		n, perr := strconv.Atoi(cursor)
		if perr != nil || n < 0 || n > len(items) {
			return nil, "", errInvalidCursor
		}
		start = n
	}
	rest := len(items) - start
	if pageSize <= 0 || pageSize >= rest {
		return items[start:], "", nil
	}
	end := start + pageSize
	return items[start:end], strconv.Itoa(end), nil
}

var errInvalidCursor = errors.New("invalid cursor")
