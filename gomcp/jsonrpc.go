package gomcp

import "encoding/json"

// JSONRPCRequest is a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 success response.
type JSONRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result"`
}

// JSONRPCError is a JSON-RPC 2.0 error response.
type JSONRPCError struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Error   RPCErrorDetail `json:"error"`
}

// RPCErrorDetail holds the error code and message.
type RPCErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewJSONRPCError creates a new JSON-RPC error response.
func NewJSONRPCError(id any, code int, message string) *JSONRPCError {
	return &JSONRPCError{
		JSONRPC: "2.0",
		ID:      id,
		Error: RPCErrorDetail{
			Code:    code,
			Message: message,
		},
	}
}
