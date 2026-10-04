package gomcp

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// runWithRealStdio swaps os.Stdin/os.Stdout for pipes, runs fn, feeds one
// request, and returns the server's response line. Restores the originals.
func runWithRealStdio(t *testing.T, fn func() error, request string) string {
	t.Helper()
	oldIn, oldOut := os.Stdin, os.Stdout
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin, os.Stdout = inR, outW
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()

	done := make(chan error, 1)
	go func() { done <- fn() }()

	if _, err := inW.WriteString(request + "\n"); err != nil {
		t.Fatal(err)
	}
	outCh := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(outR).ReadString('\n')
		outCh <- line
	}()

	var resp string
	select {
	case resp = <-outCh:
	case <-time.After(5 * time.Second):
		t.Fatal("no response within 5s")
	}
	// EOF the input so fn returns.
	inW.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after stdin EOF")
	}
	return strings.TrimSpace(resp)
}

func TestRunUsesOsStdio(t *testing.T) {
	srv := NewServer("run-stdio", "1.0.0")
	resp := runWithRealStdio(t, srv.Run, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if !strings.Contains(resp, `"result"`) {
		t.Fatalf("ping response = %q", resp)
	}
}

func TestRunContextUsesOsStdio(t *testing.T) {
	srv := NewServer("runctx-stdio", "1.0.0")
	resp := runWithRealStdio(t, func() error {
		return srv.RunContext(context.Background())
	}, `{"jsonrpc":"2.0","id":7,"method":"ping"}`)
	if !strings.Contains(resp, `"id":7`) {
		t.Fatalf("ping response = %q", resp)
	}
}
