package gomcp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func modernMeta() string {
	return `{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1.0"},"io.modelcontextprotocol/clientCapabilities":{}}}`
}

func TestServerDiscover(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.SetInstructions("Use tools to greet people.")
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":"discover-1","method":"server/discover","params":` + modernMeta() + `}` + "\n"))
		inWriter.Close()
	}()

	var resp map[string]any
	if err := json.NewDecoder(outReader).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result := resp["result"].(map[string]any)
	if result["resultType"] != "complete" {
		t.Errorf("resultType = %v, want complete", result["resultType"])
	}
	versions, _ := result["supportedVersions"].([]any)
	found := false
	for _, v := range versions {
		if v == ProtocolVersion20260728 {
			found = true
		}
	}
	if !found {
		t.Errorf("supportedVersions missing 2026-07-28: %v", versions)
	}
	if result["instructions"] != "Use tools to greet people." {
		t.Errorf("instructions = %v", result["instructions"])
	}
	if _, ok := result["ttlMs"]; !ok {
		t.Error("discover result missing ttlMs")
	}
	if result["cacheScope"] != "public" {
		t.Errorf("cacheScope = %v, want public", result["cacheScope"])
	}
	meta := result["_meta"].(map[string]any)
	info := meta["io.modelcontextprotocol/serverInfo"].(map[string]any)
	if info["name"] != "test-server" {
		t.Errorf("serverInfo.name = %v", info["name"])
	}

	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestServerDiscoverWithoutMeta(t *testing.T) {
	// stdio compatibility probe: a client that does not yet know our
	// version must still get a discover response.
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"server/discover"}` + "\n"))
		inWriter.Close()
	}()

	var resp map[string]any
	if err := json.NewDecoder(outReader).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["result"]; !ok {
		t.Fatalf("discover without _meta must succeed, got %v", resp)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestUnsupportedProtocolVersion(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"1999-01-01"}}}` + "\n"))
		inWriter.Close()
	}()

	var resp map[string]any
	if err := json.NewDecoder(outReader).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errObj := resp["error"].(map[string]any)
	if errObj["code"].(float64) != ErrCodeUnsupportedProtocolVersion {
		t.Errorf("code = %v, want %d", errObj["code"], ErrCodeUnsupportedProtocolVersion)
	}
	data := errObj["data"].(map[string]any)
	if data["requested"] != "1999-01-01" {
		t.Errorf("requested = %v", data["requested"])
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestInitializeNegotiatesLegacyVersion(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"))
		inWriter.Close()
	}()

	var resp map[string]any
	if err := json.NewDecoder(outReader).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result := resp["result"].(map[string]any)
	if result["protocolVersion"] != ProtocolVersion20241105 {
		t.Errorf("protocolVersion = %v, want %s", result["protocolVersion"], ProtocolVersion20241105)
	}
	// Legacy initialize must not grow 2026-only fields.
	if _, ok := result["resultType"]; ok {
		t.Error("initialize result must stay in the pre-2026 shape")
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestModernToolsListHasCacheHints(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.AddTool(Tool{Name: "zeta", Description: "z"})
	srv.AddTool(Tool{Name: "alpha", Description: "a", Title: "Alpha Tool",
		Annotations: &ToolAnnotations{ReadOnlyHint: true}})
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":` + modernMeta() + `}` + "\n"))
		inWriter.Close()
	}()

	var resp map[string]any
	if err := json.NewDecoder(outReader).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result := resp["result"].(map[string]any)
	if result["resultType"] != "complete" {
		t.Errorf("resultType = %v", result["resultType"])
	}
	if result["ttlMs"].(float64) != float64(DefaultListTTLMs) {
		t.Errorf("ttlMs = %v", result["ttlMs"])
	}
	tools := result["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %d", len(tools))
	}
	// Deterministic order by name.
	if tools[0].(map[string]any)["name"] != "alpha" {
		t.Errorf("first tool = %v, want alpha", tools[0])
	}
	if tools[1].(map[string]any)["name"] != "zeta" {
		t.Errorf("second tool = %v, want zeta", tools[1])
	}
	if tools[0].(map[string]any)["title"] != "Alpha Tool" {
		t.Errorf("title = %v", tools[0].(map[string]any)["title"])
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestLegacyToolsListOmitsModernFields(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.AddTool(Tool{Name: "greet"})
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		// No initialize, no _meta — the 2026-07-28 stateless path, but
		// a legacy client that skipped the version field.
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"))
		inWriter.Close()
	}()

	var resp map[string]any
	if err := json.NewDecoder(outReader).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result := resp["result"].(map[string]any)
	if _, ok := result["resultType"]; ok {
		t.Error("legacy tools/list must omit resultType")
	}
	if _, ok := result["ttlMs"]; ok {
		t.Error("legacy tools/list must omit ttlMs")
	}
	if _, ok := result["_meta"]; ok {
		t.Error("legacy tools/list must omit _meta")
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestToolsListWithoutInitialize(t *testing.T) {
	// 2026-07-28 dropped the required handshake. tools/list must work
	// without a prior initialize.
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.AddTool(Tool{Name: "greet"})
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"))
		inWriter.Close()
	}()

	var resp map[string]any
	if err := json.NewDecoder(outReader).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["error"]; ok {
		t.Fatalf("tools/list without initialize must succeed, got %v", resp)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestListPagination(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.ListPageSize = 1
	srv.AddTool(Tool{Name: "a"})
	srv.AddTool(Tool{Name: "b"})
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"))
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"cursor":"1"}}` + "\n"))
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{"cursor":"nope"}}` + "\n"))
		inWriter.Close()
	}()

	msgs := readResponses(t, outReader, 3)
	page1 := msgs[0]["result"].(map[string]any)
	tools1 := page1["tools"].([]any)
	if len(tools1) != 1 || tools1[0].(map[string]any)["name"] != "a" {
		t.Errorf("page 1 = %v", tools1)
	}
	if page1["nextCursor"] != "1" {
		t.Errorf("nextCursor = %v, want 1", page1["nextCursor"])
	}

	page2 := msgs[1]["result"].(map[string]any)
	tools2 := page2["tools"].([]any)
	if len(tools2) != 1 || tools2[0].(map[string]any)["name"] != "b" {
		t.Errorf("page 2 = %v", tools2)
	}
	if _, ok := page2["nextCursor"]; ok {
		t.Errorf("last page must omit nextCursor, got %v", page2["nextCursor"])
	}

	errObj := msgs[2]["error"].(map[string]any)
	if errObj["code"].(float64) != ErrCodeInvalidParams {
		t.Errorf("bad cursor code = %v", errObj["code"])
	}

	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestResourceTemplatesList(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"resources/templates/list"}` + "\n"))
		inWriter.Close()
	}()

	var resp map[string]any
	if err := json.NewDecoder(outReader).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result := resp["result"].(map[string]any)
	templates := result["resourceTemplates"].([]any)
	if len(templates) != 0 {
		t.Errorf("expected empty templates, got %v", templates)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestHandlerPanicKeepsServing(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.AddTool(Tool{
		Name: "boom",
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			panic("kaboom")
		},
	})
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"boom"}}` + "\n"))
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"ping"}` + "\n"))
		inWriter.Close()
	}()

	msgs := readResponses(t, outReader, 2)
	result := msgs[0]["result"].(map[string]any)
	if result["isError"] != true {
		t.Error("expected isError after panic")
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "panicked") {
		t.Errorf("error text = %q", text)
	}
	if strings.Contains(text, "kaboom") {
		t.Errorf("panic value must not leak to the client: %q", text)
	}
	if msgs[1]["id"].(float64) != 2 {
		t.Errorf("server did not survive handler panic: %v", msgs[1])
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestNilHandlerAndNilArguments(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.AddTool(Tool{
		Name: "echo",
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			if args == nil {
				t.Error("arguments must not be nil")
			}
			return "ok", nil
		},
	})
	srv.AddTool(Tool{Name: "nohandler"})
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo"}}` + "\n"))
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nohandler"}}` + "\n"))
		inWriter.Close()
	}()

	msgs := readResponses(t, outReader, 2)
	text := msgs[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if text != "ok" {
		t.Errorf("echo = %q", text)
	}
	errText := msgs[1]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(errText, "no handler") {
		t.Errorf("nil handler text = %q", errText)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestHandlerTimeout(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.HandlerTimeout = 20 * time.Millisecond
	srv.AddTool(Tool{
		Name: "slow",
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	})
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"slow"}}` + "\n"))
		inWriter.Close()
	}()

	var resp map[string]any
	if err := json.NewDecoder(outReader).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result := resp["result"].(map[string]any)
	if result["isError"] != true {
		t.Error("expected timeout to surface as isError")
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "deadline") && !strings.Contains(text, "canceled") {
		t.Errorf("timeout text = %q", text)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestInvalidJSONRPCRejected(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"1.0","id":1,"method":"ping"}` + "\n"))
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2}` + "\n"))
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":3,"method":"ping"}` + "\n"))
		inWriter.Close()
	}()

	msgs := readResponses(t, outReader, 3)
	if msgs[0]["error"].(map[string]any)["code"].(float64) != ErrCodeInvalidRequest {
		t.Errorf("jsonrpc 1.0: %v", msgs[0])
	}
	if msgs[1]["error"].(map[string]any)["code"].(float64) != ErrCodeInvalidRequest {
		t.Errorf("missing method: %v", msgs[1])
	}
	if _, ok := msgs[2]["result"]; !ok {
		t.Errorf("valid ping after rejects: %v", msgs[2])
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestConcurrentRegisterAndList(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	// Register while the server is serving. The race detector must stay quiet.
	go func() {
		for i := 0; i < 50; i++ {
			srv.AddTool(Tool{Name: "t", Handler: func(ctx context.Context, args map[string]any) (string, error) {
				return "ok", nil
			}})
		}
	}()

	go func() {
		for i := 0; i < 20; i++ {
			inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"))
		}
		inWriter.Close()
	}()

	dec := json.NewDecoder(outReader)
	for i := 0; i < 20; i++ {
		var resp map[string]any
		if err := dec.Decode(&resp); err != nil {
			t.Fatalf("decode %d: %v", i, err)
		}
		if _, ok := resp["result"]; !ok {
			t.Fatalf("list %d failed: %v", i, resp)
		}
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestModernToolsCallDecoratesResult(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.CacheScope = "private"
	srv.ListTTLMs = -1
	srv.AddTool(Tool{
		Name: "echo",
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			return "hi", nil
		},
	})
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}` + "\n"))
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":` + modernMeta() + `}` + "\n"))
		inWriter.Close()
	}()

	msgs := readResponses(t, outReader, 2)
	call := msgs[0]["result"].(map[string]any)
	if call["resultType"] != "complete" {
		t.Errorf("tools/call resultType = %v", call["resultType"])
	}
	if call["_meta"] == nil {
		t.Error("tools/call missing _meta")
	}
	list := msgs[1]["result"].(map[string]any)
	if list["ttlMs"].(float64) != 0 {
		t.Errorf("negative ListTTLMs should send ttlMs 0, got %v", list["ttlMs"])
	}
	if list["cacheScope"] != "private" {
		t.Errorf("cacheScope = %v", list["cacheScope"])
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestInitializedAndCancelledWithID(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialized"}` + "\n"))
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"notifications/cancelled","params":{"requestId":1}}` + "\n"))
		inWriter.Close()
	}()

	msgs := readResponses(t, outReader, 2)
	if _, ok := msgs[0]["result"]; !ok {
		t.Errorf("initialized with id must be answered: %v", msgs[0])
	}
	if _, ok := msgs[1]["result"]; !ok {
		t.Errorf("cancelled with id must be answered: %v", msgs[1])
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestResourceAndPromptPanicRecovery(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.AddResource(Resource{
		URI:  "file:///boom",
		Name: "Boom",
		Handler: func(ctx context.Context) (string, error) {
			panic("resource boom")
		},
	})
	srv.AddResource(Resource{URI: "file:///empty", Name: "Empty"})
	srv.AddPrompt(Prompt{
		Name: "boom",
		Handler: func(ctx context.Context, args map[string]any) ([]PromptMessage, error) {
			panic("prompt boom")
		},
	})
	srv.AddPrompt(Prompt{Name: "empty"})
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIO(inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"file:///boom"}}` + "\n"))
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"file:///empty"}}` + "\n"))
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":3,"method":"prompts/get","params":{"name":"boom"}}` + "\n"))
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":4,"method":"prompts/get","params":{"name":"empty"}}` + "\n"))
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":5,"method":"resources/list","params":{"cursor":"bad"}}` + "\n"))
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":6,"method":"prompts/list","params":{"cursor":"bad"}}` + "\n"))
		inWriter.Close()
	}()

	msgs := readResponses(t, outReader, 6)
	for i, want := range []string{"resource handler panicked", "no handler", "prompt handler panicked", "no handler"} {
		msg := msgs[i]["error"].(map[string]any)["message"].(string)
		if !strings.Contains(msg, want) {
			t.Errorf("msg %d = %q, want %q", i+1, msg, want)
		}
	}
	if msgs[4]["error"].(map[string]any)["code"].(float64) != ErrCodeInvalidParams {
		t.Errorf("resources/list bad cursor: %v", msgs[4])
	}
	if msgs[5]["error"].(map[string]any)["code"].(float64) != ErrCodeInvalidParams {
		t.Errorf("prompts/list bad cursor: %v", msgs[5])
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIO: %v", err)
	}
}

func TestRunContextCancel(t *testing.T) {
	srv := NewServer("test-server", "1.0.0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := srv.RunWithIOContext(ctx, strings.NewReader(""), io.Discard); err == nil {
		t.Fatal("expected cancelled context to stop RunWithIOContext")
	}
}

func TestSetProtocolVersionAndRunContextNil(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := NewServer("test-server", "1.0.0")
	srv.SetProtocolVersion(ProtocolVersion20250326)
	done := make(chan error, 1)
	go func() { done <- srv.RunWithIOContext(nil, inReader, outWriter) }()

	go func() {
		inWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2099-01-01"}}` + "\n"))
		inWriter.Close()
	}()

	var resp map[string]any
	if err := json.NewDecoder(outReader).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Unknown initialize version falls back to the configured default.
	if resp["result"].(map[string]any)["protocolVersion"] != ProtocolVersion20250326 {
		t.Errorf("fallback version = %v", resp["result"])
	}
	if err := <-done; err != nil {
		t.Fatalf("RunWithIOContext: %v", err)
	}
}

func TestPaginate(t *testing.T) {
	items := []string{"a", "b", "c"}
	page, next, err := paginate(items, "", 2)
	if err != nil || next != "2" || strings.Join(page, "") != "ab" {
		t.Fatalf("first page: %v %q %v", page, next, err)
	}
	page, next, err = paginate(items, "2", 2)
	if err != nil || next != "" || strings.Join(page, "") != "c" {
		t.Fatalf("second page: %v %q %v", page, next, err)
	}
	if _, _, err := paginate(items, "x", 2); err == nil {
		t.Fatal("expected invalid cursor")
	}
	if _, _, err := paginate(items, "9", 2); err == nil {
		t.Fatal("expected cursor past end to fail")
	}
	// A huge page size must not overflow start+pageSize and panic.
	page, next, err = paginate(items, "1", int(^uint(0)>>1))
	if err != nil || next != "" || strings.Join(page, "") != "bc" {
		t.Fatalf("max page size: %v %q %v", page, next, err)
	}
}
