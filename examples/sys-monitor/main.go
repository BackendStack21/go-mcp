// System Monitor — expose CPU, memory, and disk as MCP resources.
//
// This example shows how to give AI agents visibility into system health.
// Resources are queried on-demand via resources/read. Tools let the AI
// check specific metrics.
//
// Build: go build -o sys-monitor .
// Test:  echo '{"jsonrpc":"2.0","id":1,"method":"resources/list"}' | ./sys-monitor
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/BackendStack21/go-mcp/gomcp"
)

func main() {
	srv := gomcp.NewServer("sys-monitor", "1.0.0")

	// Resource: CPU info
	srv.AddResource(gomcp.Resource{
		URI:         "system://cpu",
		Name:        "CPU Information",
		Description: "CPU architecture, core count, and Go runtime stats",
		MimeType:    "application/json",
		Handler: func(ctx context.Context) (string, error) {
			info := map[string]any{
				"cores":       runtime.NumCPU(),
				"goroutines":  runtime.NumGoroutine(),
				"go_version":  runtime.Version(),
				"go_os":       runtime.GOOS,
				"go_arch":     runtime.GOARCH,
				"timestamp":   time.Now().Format(time.RFC3339),
			}
			data, _ := json.MarshalIndent(info, "", "  ")
			return string(data), nil
		},
	})

	// Resource: Memory stats
	srv.AddResource(gomcp.Resource{
		URI:         "system://memory",
		Name:        "Memory Usage",
		Description: "Current Go runtime memory statistics",
		MimeType:    "application/json",
		Handler: func(ctx context.Context) (string, error) {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			info := map[string]any{
				"alloc_mb":         m.Alloc / 1e6,
				"total_alloc_mb":   m.TotalAlloc / 1e6,
				"sys_mb":           m.Sys / 1e6,
				"heap_alloc_mb":    m.HeapAlloc / 1e6,
				"heap_sys_mb":      m.HeapSys / 1e6,
				"heap_objects":     m.HeapObjects,
				"gc_cycles":        m.NumGC,
				"gc_pause_total_ms": m.PauseTotalNs / 1e6,
				"timestamp":        time.Now().Format(time.RFC3339),
			}
			data, _ := json.MarshalIndent(info, "", "  ")
			return string(data), nil
		},
	})

	// Resource: Process info
	srv.AddResource(gomcp.Resource{
		URI:         "system://process",
		Name:        "Process Information",
		Description: "Process ID, working directory, and command line",
		MimeType:    "application/json",
		Handler: func(ctx context.Context) (string, error) {
			wd, _ := os.Getwd()
			host, _ := os.Hostname()
			info := map[string]any{
				"pid":      os.Getpid(),
				"ppid":     os.Getppid(),
				"uid":      os.Getuid(),
				"gid":      os.Getgid(),
				"hostname": host,
				"cwd":      wd,
				"args":     os.Args,
			}
			data, _ := json.MarshalIndent(info, "", "  ")
			return string(data), nil
		},
	})

	// Tool: Run a health check
	srv.AddTool(gomcp.Tool{
		Name:        "health_check",
		Description: "Run a comprehensive health check and return status",
		InputSchema: gomcp.InputSchema{
			Type:       "object",
			Properties: map[string]gomcp.Property{},
		},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			status := "healthy"
			issues := []string{}

			if m.HeapAlloc > 500*1e6 {
				status = "degraded"
				issues = append(issues, fmt.Sprintf("high memory usage: %d MB allocated", m.HeapAlloc/1e6))
			}

			if runtime.NumGoroutine() > 10000 {
				status = "degraded"
				issues = append(issues, fmt.Sprintf("high goroutine count: %d", runtime.NumGoroutine()))
			}

			info := map[string]any{
				"status":    status,
				"issues":    issues,
				"uptime_go": time.Now().Format(time.RFC3339),
			}

			data, _ := json.MarshalIndent(info, "", "  ")
			return string(data), nil
		},
	})

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
