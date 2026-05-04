// FS Navigator — scoped, safe filesystem access for AI agents.
//
// This example shows how to give AI agents controlled access to a
// directory tree. Tools for reading/writing/listing files, all scoped
// to a configured root directory with path traversal protection.
//
// Build: go build -o fs-navigator .
// Test:  echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | ./fs-navigator
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BackendStack21/go-mcp/gomcp"
)

func main() {
	root := os.Getenv("FS_ROOT")
	if root == "" {
		root = "/var/project"
	}

	srv := gomcp.NewServer("fs-navigator", "1.0.0")

	// Tool: list directory contents
	srv.AddTool(gomcp.Tool{
		Name:        "list_dir",
		Description: "List files and directories at the given path",
		InputSchema: gomcp.InputSchema{
			Type: "object",
			Properties: map[string]gomcp.Property{
				"path": {Type: "string", Description: "Directory path relative to project root"},
			},
			Required: []string{"path"},
		},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			dirPath := args["path"].(string)
			fullPath, err := safePath(root, dirPath)
			if err != nil {
				return "", err
			}

			entries, err := os.ReadDir(fullPath)
			if err != nil {
				return "", fmt.Errorf("read dir: %w", err)
			}

			// Sort: dirs first, then files
			sort.Slice(entries, func(i, j int) bool {
				if entries[i].IsDir() != entries[j].IsDir() {
					return entries[i].IsDir()
				}
				return entries[i].Name() < entries[j].Name()
			})

			var result strings.Builder
			for _, entry := range entries {
				prefix := "📄"
				if entry.IsDir() {
					prefix = "📁"
				}
				info, _ := entry.Info()
				size := ""
				if !entry.IsDir() && info != nil {
					size = fmt.Sprintf(" (%d bytes)", info.Size())
				}
				result.WriteString(fmt.Sprintf("%s %s%s\n", prefix, entry.Name(), size))
			}

			if result.Len() == 0 {
				return "(empty directory)", nil
			}
			return result.String(), nil
		},
	})

	// Tool: read file contents
	srv.AddTool(gomcp.Tool{
		Name:        "read_file",
		Description: "Read the contents of a file",
		InputSchema: gomcp.InputSchema{
			Type: "object",
			Properties: map[string]gomcp.Property{
				"path": {Type: "string", Description: "File path relative to project root"},
			},
			Required: []string{"path"},
		},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			filePath := args["path"].(string)
			fullPath, err := safePath(root, filePath)
			if err != nil {
				return "", err
			}

			data, err := os.ReadFile(fullPath)
			if err != nil {
				return "", fmt.Errorf("read file: %w", err)
			}

			if len(data) > 100*1024 { // 100KB limit
				return fmt.Sprintf("(file too large: %d bytes, showing first 100KB)\n\n%s", len(data), string(data[:100*1024])), nil
			}
			return string(data), nil
		},
	})

	// Tool: search files by pattern
	srv.AddTool(gomcp.Tool{
		Name:        "search_files",
		Description: "Search for files matching a pattern (supports globs like *.go)",
		InputSchema: gomcp.InputSchema{
			Type: "object",
			Properties: map[string]gomcp.Property{
				"pattern": {Type: "string", Description: "File glob pattern (e.g., '*.go', 'test_*.ts')"},
			},
			Required: []string{"pattern"},
		},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			pattern := args["pattern"].(string)

			var matches []string
			err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil // skip inaccessible files
				}
				relPath, _ := filepath.Rel(root, path)

				// Skip hidden directories
				if info.IsDir() && strings.HasPrefix(info.Name(), ".") && info.Name() != "." {
					return filepath.SkipDir
				}

				matched, _ := filepath.Match(pattern, info.Name())
				if matched {
					matches = append(matches, relPath)
				}
				return nil
			})

			if err != nil {
				return "", fmt.Errorf("search error: %w", err)
			}

			if len(matches) == 0 {
				return fmt.Sprintf("no files matching '%s'", pattern), nil
			}

			sort.Strings(matches)
			var result strings.Builder
			result.WriteString(fmt.Sprintf("%d files matching '%s':\n", len(matches), pattern))
			for _, m := range matches {
				result.WriteString(fmt.Sprintf("  %s\n", m))
			}

			if len(matches) > 50 {
				result.WriteString(fmt.Sprintf("\n... and %d more files", len(matches)-50))
			}
			return result.String(), nil
		},
	})

	// Resource: directory tree overview
	srv.AddResource(gomcp.Resource{
		URI:         "fs://tree",
		Name:        "Directory Tree",
		Description: "Overview of the project directory structure",
		MimeType:    "text/plain",
		Handler: func(ctx context.Context) (string, error) {
			var tree strings.Builder
			filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				relPath, _ := filepath.Rel(root, path)
				if relPath == "." {
					return nil
				}

				// Skip hidden dirs
				if info.IsDir() && strings.HasPrefix(info.Name(), ".") {
					return filepath.SkipDir
				}

				depth := strings.Count(relPath, string(os.PathSeparator))
				indent := strings.Repeat("  ", depth)
				prefix := "├── "

				if info.IsDir() {
					tree.WriteString(fmt.Sprintf("%s%s📁 %s/\n", indent, prefix, info.Name()))
				} else {
					size := ""
					if info.Size() > 1024 {
						size = fmt.Sprintf(" (%d KB)", info.Size()/1024)
					}
					tree.WriteString(fmt.Sprintf("%s%s%s\n", indent, prefix, info.Name()+size))
				}
				return nil
			})

			if tree.Len() == 0 {
				return "(empty project)", nil
			}
			return tree.String(), nil
		},
	})

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

// safePath resolves a user-supplied relative path against the root,
// preventing path traversal attacks.
func safePath(root, userPath string) (string, error) {
	if strings.Contains(userPath, "..") {
		return "", fmt.Errorf("path traversal not allowed: %s", userPath)
	}

	fullPath := filepath.Join(root, userPath)
	cleaned := filepath.Clean(fullPath)

	// Ensure we're still under root
	if !strings.HasPrefix(cleaned, filepath.Clean(root)) {
		return "", fmt.Errorf("path escapes root directory: %s", userPath)
	}

	return cleaned, nil
}
