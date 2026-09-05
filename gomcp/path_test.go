package gomcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeJoinStaysInsideRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := SafeJoin(root, "ok.txt")
	if err != nil {
		t.Fatalf("SafeJoin: %v", err)
	}
	if got != filepath.Join(root, "ok.txt") {
		t.Fatalf("got %q, want file inside root", got)
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	cases := []string{
		"..",
		"../etc/passwd",
		"foo/../../etc/passwd",
		filepath.Join("..", "..", "etc", "passwd"),
	}
	// An absolute path that is not under root must also be rejected.
	// On Unix Join(root, "/etc/passwd") becomes /etc/passwd.
	cases = append(cases, "/etc/passwd")

	for _, p := range cases {
		got, err := SafeJoin(root, p)
		if err == nil {
			t.Errorf("SafeJoin(%q) = %q, want error", p, got)
		}
	}
}

func TestSafeJoinRejectsAdjacentPrefix(t *testing.T) {
	// /tmp/project vs /tmp/project-evil — a naive HasPrefix check fails here.
	base := t.TempDir()
	root := filepath.Join(base, "project")
	evil := filepath.Join(base, "project-evil")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(evil, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evil, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := SafeJoin(root, filepath.Join("..", "project-evil", "secret"))
	if err == nil {
		t.Fatalf("adjacent-prefix escape succeeded: %s", got)
	}
}

func TestSafeJoinRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret")
	if err := os.WriteFile(target, []byte("classified"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "leak")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	got, err := SafeJoin(root, "leak")
	if err == nil {
		t.Fatalf("symlink escape succeeded: %s", got)
	}
	if strings.Contains(got, "secret") {
		t.Fatalf("escaped path leaked in return value: %s", got)
	}
}

func TestSafeJoinAbsoluteInsideRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "ok.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := SafeJoin(root, target)
	if err != nil {
		t.Fatalf("absolute path inside root: %v", err)
	}
	if got != target {
		t.Fatalf("got %q, want %q", got, target)
	}
}

func TestSafeJoinEmptyIsRoot(t *testing.T) {
	root := t.TempDir()
	got, err := SafeJoin(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("got %q, want root %q", got, root)
	}
}

func TestSafeJoinRejectsNUL(t *testing.T) {
	root := t.TempDir()
	if _, err := SafeJoin(root, "foo\x00bar"); err == nil {
		t.Fatal("expected error for NUL in path")
	}
}
