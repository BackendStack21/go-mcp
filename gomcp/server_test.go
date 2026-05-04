package gomcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

// ----- JSON-RPC Types -----

func TestJSONRPCRequestMarshal(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"greet","arguments":{"name":"World"}}`),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc '2.0', got %v", result["jsonrpc"])
	}
	if result["method"] != "tools/call" {
		t.Errorf("expected method 'tools/call', got %v", result["method"])
	}
}

func TestJSONRPCResponseMarshal(t *testing.T) {
	result := map[string]any{"content": []any{map[string]any{"type": "text", "text": "Hello!"}}}
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      1,
		Result:  result,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc '2.0', got %v", parsed["jsonrpc"])
	}
}

func TestJSONRPCErrorMarshal(t *testing.T) {
	resp := NewJSONRPCError(1, -32601, "Method not found")

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	errObj := parsed["error"].(map[string]any)
	if errObj["code"].(float64) != -32601 {
		t.Errorf("expected code -32601, got %v", errObj["code"])
	}
}

// ----- Server Creation -----

func TestNewServer(t *testing.T) {
	srv := NewServer("test-server", "1.0.0")
	if srv.name != "test-server" {
		t.Errorf("expected name 'test-server', got %q", srv.name)
	}
	if srv.version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", srv.version)
	}
}

func TestAddTool(t *testing.T) {
	srv := NewServer("test-server", "1.0.0")
	srv.AddTool(Tool{
		Name:        "greet",
		Description: "Greet someone",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name": {Type: "string", Description: "Name to greet"},
			},
			Required: []string{"name"},
		},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			return "Hello, " + args["name"].(string) + "!", nil
		},
	})

	if _, ok := srv.tools["greet"]; !ok {
		t.Fatal("expected tool 'greet' to be registered")
	}
}

// ----- Initialize -----

func TestServerInitialize(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	// Send initialize
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
		inWriter.Close()
	}()

	var initResp map[string]any
	if err := json.NewDecoder(outReader).Decode(&initResp); err != nil {
		t.Fatalf("failed to read init response: %v", err)
	}

	result := initResp["result"].(map[string]any)
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("expected protocolVersion '2024-11-05', got %v", result["protocolVersion"])
	}
	serverInfo := result["serverInfo"].(map[string]any)
	if serverInfo["name"] != "test-server" {
		t.Errorf("expected name 'test-server', got %v", serverInfo["name"])
	}
	if serverInfo["version"] != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %v", serverInfo["version"])
	}

	// Verify capabilities include tools, resources, prompts
	caps := result["capabilities"].(map[string]any)
	if _, ok := caps["tools"]; !ok {
		t.Error("expected capabilities to include 'tools'")
	}
	if _, ok := caps["resources"]; !ok {
		t.Error("expected capabilities to include 'resources'")
	}
	if _, ok := caps["prompts"]; !ok {
		t.Error("expected capabilities to include 'prompts'")
	}
}

// ----- Tools -----

func TestToolsList(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.AddTool(Tool{
		Name:        "greet",
		Description: "Greet someone",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name": {Type: "string", Description: "Name to greet"},
			},
			Required: []string{"name"},
		},
	})

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	// Initialize
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	json.NewDecoder(outReader).Decode(new(map[string]any))

	// tools/list
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"))
		inWriter.Close()
	}()

	var listResp map[string]any
	if err := json.NewDecoder(outReader).Decode(&listResp); err != nil {
		t.Fatalf("failed to decode tools/list response: %v", err)
	}

	result := listResp["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "greet" {
		t.Errorf("expected tool name 'greet', got %v", tool["name"])
	}
	// Handler should NOT be serialized
	if _, exists := tool["Handler"]; exists {
		t.Error("Handler field should not be serialized in tool metadata")
	}
}

func TestToolsCall(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.AddTool(Tool{
		Name:        "greet",
		Description: "Greet someone",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name": {Type: "string", Description: "Name to greet"},
			},
			Required: []string{"name"},
		},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			return "Hello, " + args["name"].(string) + "!", nil
		},
	})

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	// Initialize
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	json.NewDecoder(outReader).Decode(new(map[string]any))

	// tools/call
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"greet","arguments":{"name":"Molty"}}}` + "\n"))
		inWriter.Close()
	}()

	var callResp map[string]any
	if err := json.NewDecoder(outReader).Decode(&callResp); err != nil {
		t.Fatalf("failed to decode tools/call response: %v", err)
	}

	result := callResp["result"].(map[string]any)
	content := result["content"].([]any)
	textBlock := content[0].(map[string]any)
	if textBlock["text"] != "Hello, Molty!" {
		t.Errorf("expected 'Hello, Molty!', got %v", textBlock["text"])
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	json.NewDecoder(outReader).Decode(new(map[string]any))

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}` + "\n"))
		inWriter.Close()
	}()

	var errResp map[string]any
	if err := json.NewDecoder(outReader).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj := errResp["error"].(map[string]any)
	code := errObj["code"].(float64)
	if code != -32602 {
		t.Errorf("expected error code -32602, got %v", code)
	}
}

func TestToolsCallHandlerError(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.AddTool(Tool{
		Name:        "failing",
		Description: "Always fails",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			return "", fmt.Errorf("something went wrong")
		},
	})

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	json.NewDecoder(outReader).Decode(new(map[string]any))

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"failing","arguments":{}}}` + "\n"))
		inWriter.Close()
	}()

	var errResp map[string]any
	if err := json.NewDecoder(outReader).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj := errResp["error"].(map[string]any)
	code := errObj["code"].(float64)
	if code != -32000 {
		t.Errorf("expected error code -32000, got %v", code)
	}
	if errObj["message"] != "something went wrong" {
		t.Errorf("expected 'something went wrong', got %v", errObj["message"])
	}
}

// ----- Resources -----

func TestResourcesListAndRead(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.AddResource(Resource{
		URI:         "file:///config.yaml",
		Name:        "Config",
		Description: "Application config",
		MimeType:    "text/yaml",
		Handler: func(ctx context.Context) (string, error) {
			return "debug: true", nil
		},
	})

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	// Initialize
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	json.NewDecoder(outReader).Decode(new(map[string]any))

	// resources/list
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"resources/list"}` + "\n"))
	}()
	var listResp map[string]any
	json.NewDecoder(outReader).Decode(&listResp)
	resources := listResp["result"].(map[string]any)["resources"].([]any)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	// resources/read
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"file:///config.yaml"}}` + "\n"))
		inWriter.Close()
	}()
	var readResp map[string]any
	if err := json.NewDecoder(outReader).Decode(&readResp); err != nil {
		t.Fatalf("failed to decode resources/read response: %v", err)
	}
	contents := readResp["result"].(map[string]any)["contents"].([]any)
	text := contents[0].(map[string]any)["text"].(string)
	if text != "debug: true" {
		t.Errorf("expected 'debug: true', got %q", text)
	}
}

func TestResourcesReadUnknown(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	json.NewDecoder(outReader).Decode(new(map[string]any))

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"file:///nonexistent"}}` + "\n"))
		inWriter.Close()
	}()

	var errResp map[string]any
	json.NewDecoder(outReader).Decode(&errResp)
	errObj := errResp["error"].(map[string]any)
	if errObj["code"].(float64) != -32602 {
		t.Errorf("expected code -32602, got %v", errObj["code"])
	}
}

// ----- Prompts -----

func TestPromptsListAndGet(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.AddPrompt(Prompt{
		Name:        "greet-prompt",
		Description: "A friendly greeting prompt",
		Arguments: []PromptArg{
			{Name: "name", Description: "Name to greet", Required: true},
		},
		Handler: func(ctx context.Context, args map[string]any) ([]PromptMessage, error) {
			name := args["name"].(string)
			return []PromptMessage{
				{Role: "user", Content: NewTextContent("Say hello to " + name)},
				{Role: "assistant", Content: NewTextContent("Hello, " + name + "! How can I help you today?")},
			}, nil
		},
	})

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	// Initialize
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	json.NewDecoder(outReader).Decode(new(map[string]any))

	// prompts/list
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"prompts/list"}` + "\n"))
	}()
	var listResp map[string]any
	json.NewDecoder(outReader).Decode(&listResp)
	prompts := listResp["result"].(map[string]any)["prompts"].([]any)
	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}
	p := prompts[0].(map[string]any)
	if p["name"] != "greet-prompt" {
		t.Errorf("expected name 'greet-prompt', got %v", p["name"])
	}

	// prompts/get
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":3,"method":"prompts/get","params":{"name":"greet-prompt","arguments":{"name":"Molty"}}}` + "\n"))
		inWriter.Close()
	}()
	var getResp map[string]any
	if err := json.NewDecoder(outReader).Decode(&getResp); err != nil {
		t.Fatalf("failed to decode prompts/get response: %v", err)
	}
	messages := getResp["result"].(map[string]any)["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	msg1 := messages[0].(map[string]any)
	if msg1["role"] != "user" {
		t.Errorf("expected role 'user', got %v", msg1["role"])
	}
}

// ----- Unknown Method -----

func TestUnknownMethod(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	json.NewDecoder(outReader).Decode(new(map[string]any))

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"bogus/method","params":{}}` + "\n"))
		inWriter.Close()
	}()

	var errResp map[string]any
	if err := json.NewDecoder(outReader).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	errObj := errResp["error"].(map[string]any)
	if errObj["code"].(float64) != -32601 {
		t.Errorf("expected code -32601, got %v", errObj["code"])
	}
}

// ----- Edge Cases -----

func TestDecodeError(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.runWithIO(inReader, outWriter)
	}()

	// Send malformed JSON
	go func() {
		inWriter.Write([]byte(`this is not json` + "\n"))
		inWriter.Close()
	}()

	// Drain the output pipe to avoid blocking
	go io.Copy(io.Discard, outReader)

	err := <-errCh
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	if !strings.Contains(err.Error(), "decode error") {
		t.Errorf("expected 'decode error' in message, got: %v", err)
	}
}

func TestNotificationIgnored(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	// Send initialize
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	var initResp map[string]any
	json.NewDecoder(outReader).Decode(&initResp)

	// Send a notification (no id field) — should be consumed silently, no response
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"))
	}()

	// Send tools/list to verify server is still alive
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"))
		inWriter.Close()
	}()

	var listResp map[string]any
	if err := json.NewDecoder(outReader).Decode(&listResp); err != nil {
		t.Fatalf("server should still respond after notification: %v", err)
	}
	if listResp["id"].(float64) != 2 {
		t.Errorf("expected id 2, got %v", listResp["id"])
	}
}

func TestToolsCallInvalidParams(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	json.NewDecoder(outReader).Decode(new(map[string]any))

	// Send tools/call with params that fail unmarshal (params is a string, not object)
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":"bad"}` + "\n"))
		inWriter.Close()
	}()

	var errResp map[string]any
	if err := json.NewDecoder(outReader).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error: %v", err)
	}
	errObj := errResp["error"].(map[string]any)
	if errObj["code"].(float64) != -32602 {
		t.Errorf("expected code -32602, got %v", errObj["code"])
	}
}

func TestResourcesReadInvalidParams(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	json.NewDecoder(outReader).Decode(new(map[string]any))

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":"bad"}` + "\n"))
		inWriter.Close()
	}()

	var errResp map[string]any
	json.NewDecoder(outReader).Decode(&errResp)
	errObj := errResp["error"].(map[string]any)
	if errObj["code"].(float64) != -32602 {
		t.Errorf("expected code -32602, got %v", errObj["code"])
	}
}

func TestResourcesReadHandlerError(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.AddResource(Resource{
		URI:      "file:///failing",
		Name:     "Failing Resource",
		MimeType: "text/plain",
		Handler: func(ctx context.Context) (string, error) {
			return "", fmt.Errorf("resource unavailable")
		},
	})

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	json.NewDecoder(outReader).Decode(new(map[string]any))

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"file:///failing"}}` + "\n"))
		inWriter.Close()
	}()

	var errResp map[string]any
	json.NewDecoder(outReader).Decode(&errResp)
	errObj := errResp["error"].(map[string]any)
	if errObj["code"].(float64) != -32000 {
		t.Errorf("expected code -32000, got %v", errObj["code"])
	}
	if errObj["message"] != "resource unavailable" {
		t.Errorf("expected 'resource unavailable', got %v", errObj["message"])
	}
}

func TestPromptsGetInvalidParams(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	json.NewDecoder(outReader).Decode(new(map[string]any))

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":"bad"}` + "\n"))
		inWriter.Close()
	}()

	var errResp map[string]any
	json.NewDecoder(outReader).Decode(&errResp)
	errObj := errResp["error"].(map[string]any)
	if errObj["code"].(float64) != -32602 {
		t.Errorf("expected code -32602, got %v", errObj["code"])
	}
}

func TestPromptsGetUnknown(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	json.NewDecoder(outReader).Decode(new(map[string]any))

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"nonexistent","arguments":{}}}` + "\n"))
		inWriter.Close()
	}()

	var errResp map[string]any
	json.NewDecoder(outReader).Decode(&errResp)
	errObj := errResp["error"].(map[string]any)
	if errObj["code"].(float64) != -32602 {
		t.Errorf("expected code -32602, got %v", errObj["code"])
	}
}

func TestPromptsGetHandlerError(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.AddPrompt(Prompt{
		Name:        "failing-prompt",
		Description: "Always fails",
		Arguments:   []PromptArg{},
		Handler: func(ctx context.Context, args map[string]any) ([]PromptMessage, error) {
			return nil, fmt.Errorf("prompt generation failed")
		},
	})

	go func() {
		srv.runWithIO(inReader, outWriter)
	}()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
	}()
	json.NewDecoder(outReader).Decode(new(map[string]any))

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"failing-prompt","arguments":{}}}` + "\n"))
		inWriter.Close()
	}()

	var errResp map[string]any
	json.NewDecoder(outReader).Decode(&errResp)
	errObj := errResp["error"].(map[string]any)
	if errObj["code"].(float64) != -32000 {
		t.Errorf("expected code -32000, got %v", errObj["code"])
	}
	if errObj["message"] != "prompt generation failed" {
		t.Errorf("expected 'prompt generation failed', got %v", errObj["message"])
	}
}

// failWriter is a writer that fails after N writes, used to test error propagation.
type failWriter struct {
	failAfter int
	count     int
}

func (w *failWriter) Write(p []byte) (int, error) {
	w.count++
	if w.count > w.failAfter {
		return 0, fmt.Errorf("write failed")
	}
	return len(p), nil
}

func TestEncoderWriteError(t *testing.T) {
	inReader, inWriter := io.Pipe()
	w := &failWriter{failAfter: 0} // fails on first write

	srv := NewServer("test-server", "1.0.0")

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.runWithIO(inReader, w)
	}()

	// Send initialize — encoder will fail
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
		inWriter.Close()
	}()

	err := <-errCh
	if err == nil {
		t.Fatal("expected encoder write error, got nil")
	}
}

func TestDefaultHandlerEncoderError(t *testing.T) {
	inReader, inWriter := io.Pipe()
	w := &failWriter{failAfter: 0} // fails on first write

	srv := NewServer("test-server", "1.0.0")

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.runWithIO(inReader, w)
	}()

	// Send unknown method — triggers default handler, encoder will fail writing error response
	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"bogus","params":{}}` + "\n"))
		inWriter.Close()
	}()

	err := <-errCh
	if err == nil {
		t.Fatal("expected encoder write error, got nil")
	}
}

func TestToolsListEncoderError(t *testing.T) {
	inReader, inWriter := io.Pipe()
	w := &failWriter{failAfter: 0}

	srv := NewServer("test-server", "1.0.0")
	srv.AddTool(Tool{
		Name:        "t",
		Description: "d",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}},
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.runWithIO(inReader, w)
	}()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"))
		inWriter.Close()
	}()

	err := <-errCh
	if err == nil {
		t.Fatal("expected encoder write error, got nil")
	}
}

func TestResourcesListEncoderError(t *testing.T) {
	inReader, inWriter := io.Pipe()
	w := &failWriter{failAfter: 0}

	srv := NewServer("test-server", "1.0.0")
	srv.AddResource(Resource{
		URI: "file:///x", Name: "X", MimeType: "text/plain",
		Handler: func(ctx context.Context) (string, error) { return "ok", nil },
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.runWithIO(inReader, w)
	}()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"resources/list"}` + "\n"))
		inWriter.Close()
	}()

	err := <-errCh
	if err == nil {
		t.Fatal("expected encoder write error, got nil")
	}
}

func TestPromptsListEncoderError(t *testing.T) {
	inReader, inWriter := io.Pipe()
	w := &failWriter{failAfter: 0}

	srv := NewServer("test-server", "1.0.0")
	srv.AddPrompt(Prompt{
		Name: "p", Description: "d", Arguments: []PromptArg{},
		Handler: func(ctx context.Context, args map[string]any) ([]PromptMessage, error) {
			return []PromptMessage{{Role: "user", Content: NewTextContent("hi")}}, nil
		},
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.runWithIO(inReader, w)
	}()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"prompts/list"}` + "\n"))
		inWriter.Close()
	}()

	err := <-errCh
	if err == nil {
		t.Fatal("expected encoder write error, got nil")
	}
}
