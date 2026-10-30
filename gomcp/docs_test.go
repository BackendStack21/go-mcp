package gomcp

import (
	"os"
	"strings"
	"testing"
)

// TestDocsMentionAuthAPI pins AC-9: the README and AGENTS.md document the
// pluggable auth API and the typed client AuthError.
func TestDocsMentionAuthAPI(t *testing.T) {
	cases := []struct {
		file    string
		symbols []string
	}{
		{"../README.md", []string{"SetAuthFunc", "SetResourceMetadataURL", "SetProtectedResourceMetadata", "SetAuthProvider", "AuthError"}},
		{"../AGENTS.md", []string{"SetAuthFunc", "AuthError"}},
	}
	for _, c := range cases {
		b, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatalf("read %s: %v", c.file, err)
		}
		text := string(b)
		for _, s := range c.symbols {
			if !strings.Contains(text, s) {
				t.Errorf("%s does not mention %s", c.file, s)
			}
		}
	}
}
