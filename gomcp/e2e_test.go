package gomcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"testing"
)

func TestE2EGreetServer(t *testing.T) {
	cmd := exec.Command("go", "run", "../examples/greet/main.go")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("failed to get stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to get stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer cmd.Process.Kill()

	scanner := bufio.NewScanner(stdout)

	// Initialize
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}`+"\n")
	if !scanner.Scan() {
		t.Fatal("no init response")
	}
	var initResp map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &initResp); err != nil {
		t.Fatalf("invalid init response: %v", err)
	}
	if initResp["id"].(float64) != 1 {
		t.Fatalf("expected id 1 in response")
	}

	// Initialized notification (no response expected)
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")

	// tools/list
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n")
	if !scanner.Scan() {
		t.Fatal("no tools/list response")
	}
	var listResp map[string]any
	json.Unmarshal(scanner.Bytes(), &listResp)
	tools := listResp["result"].(map[string]any)["tools"].([]any)
	if len(tools) == 0 {
		t.Fatal("expected at least one tool")
	}

	// tools/call
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"greet","arguments":{"name":"Molty"}}}`+"\n")
	if !scanner.Scan() {
		t.Fatal("no tools/call response")
	}
	var callResp map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &callResp); err != nil {
		t.Fatalf("invalid tools/call response: %v", err)
	}
	result := callResp["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if text != "Hello, Molty!" {
		t.Errorf("expected 'Hello, Molty!', got %q", text)
	}

	stdin.Close()
	cmd.Wait()
}
