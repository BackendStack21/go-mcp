package gomcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SafeJoin resolves userPath against root and returns an absolute path that
// is guaranteed to stay inside root. Path traversal (`..`), absolute inputs
// outside root, and symlink escapes are rejected. Empty userPath names the
// root itself.
//
// Use this when a tool argument is a filesystem path supplied by a model.
//
// SafeJoin is a check, not an open: a concurrent writer can still replace a
// path component between this call and a later os.Open. For hostile trees,
// open with O_NOFOLLOW (or an openat walk) after joining.
func SafeJoin(root, userPath string) (string, error) {
	if strings.ContainsRune(userPath, 0) {
		return "", fmt.Errorf("invalid path")
	}

	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", err
		}
		root = abs
	}

	var cleaned string
	switch {
	case userPath == "":
		cleaned = root
	case filepath.IsAbs(userPath):
		// Keep the caller's absolute path only when it already sits
		// inside root. Do not Join() it — on Unix Join(root, "/etc/passwd")
		// becomes root+"/etc/passwd" and would hide the intent.
		cleaned = filepath.Clean(userPath)
	default:
		cleaned = filepath.Clean(filepath.Join(root, userPath))
	}

	if err := underRoot(root, cleaned); err != nil {
		return "", fmt.Errorf("path escapes root directory: %s", userPath)
	}

	// Resolve existing symlinks so a link planted inside root cannot
	// point the caller at /etc/passwd. A path that does not yet exist
	// (write-to-create) is returned as cleaned after the prefix check.
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		if err := underRoot(root, resolved); err != nil {
			return "", fmt.Errorf("path escapes root directory: %s", userPath)
		}
		return resolved, nil
	}

	return cleaned, nil
}

func underRoot(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return errEscapesRoot
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errEscapesRoot
	}
	return nil
}

var errEscapesRoot = fmt.Errorf("path escapes root")
