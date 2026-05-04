//go:build ignore
// +build ignore

// DB Explorer — safe read-only database access for AI agents.
//
// This example shows how to wrap a PostgreSQL database as MCP tools
// and resources. The AI can explore the schema, list tables, and run
// read-only queries — without raw database credentials.
//
// Requires: github.com/lib/pq driver
// Build:    go build -tags ignore -o db-explorer .
//           or remove the build tag and: go get github.com/lib/pq
// Test:     echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | ./db-explorer
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	_ "github.com/lib/pq"
	"github.com/BackendStack21/go-mcp/gomcp"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://localhost:5432/mydb?sslmode=disable"
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db connect: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	srv := gomcp.NewServer("db-explorer", "1.0.0")

	// Tool: run read-only queries
	srv.AddTool(gomcp.Tool{
		Name:        "db_query",
		Description: "Run a read-only SQL query. Only SELECT statements allowed.",
		InputSchema: gomcp.InputSchema{
			Type: "object",
			Properties: map[string]gomcp.Property{
				"query": {Type: "string", Description: "SQL SELECT query to execute"},
			},
			Required: []string{"query"},
		},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			query := args["query"].(string)
			trimmed := strings.TrimSpace(query)
			if !strings.HasPrefix(strings.ToUpper(trimmed), "SELECT") {
				return "", fmt.Errorf("only SELECT queries are allowed. Received: %s", trimmed[:min(50, len(trimmed))])
			}

			rows, err := db.QueryContext(ctx, query)
			if err != nil {
				return "", fmt.Errorf("query failed: %w", err)
			}
			defer rows.Close()

			columns, err := rows.Columns()
			if err != nil {
				return "", err
			}

			var result strings.Builder
			result.WriteString(strings.Join(columns, "\t") + "\n")
			result.WriteString(strings.Repeat("-", len(columns)*12) + "\n")

			values := make([]any, len(columns))
			valuePtrs := make([]any, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			count := 0
			for rows.Next() {
				if err := rows.Scan(valuePtrs...); err != nil {
					return "", err
				}
				row := make([]string, len(columns))
				for i, val := range values {
					switch v := val.(type) {
					case nil:
						row[i] = "NULL"
					case []byte:
						row[i] = string(v)
					default:
						row[i] = fmt.Sprintf("%v", v)
					}
				}
				result.WriteString(strings.Join(row, "\t") + "\n")
				count++
			}

			return fmt.Sprintf("%d rows\n\n%s", count, result.String()), rows.Err()
		},
	})

	// Tool: list all tables
	srv.AddTool(gomcp.Tool{
		Name:        "db_tables",
		Description: "List all tables in the database",
		InputSchema: gomcp.InputSchema{
			Type:       "object",
			Properties: map[string]gomcp.Property{},
		},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			rows, err := db.QueryContext(ctx,
				"SELECT table_schema, table_name FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog', 'information_schema') ORDER BY table_schema, table_name")
			if err != nil {
				return "", err
			}
			defer rows.Close()

			var result strings.Builder
			for rows.Next() {
				var schema, name string
				rows.Scan(&schema, &name)
				result.WriteString(fmt.Sprintf("%s.%s\n", schema, name))
			}
			return result.String(), rows.Err()
		},
	})

	// Resource: full database schema
	srv.AddResource(gomcp.Resource{
		URI:         "db://schema",
		Name:        "Database Schema",
		Description: "Complete database schema including columns, types, and constraints",
		MimeType:    "text/plain",
		Handler: func(ctx context.Context) (string, error) {
			rows, err := db.QueryContext(ctx, `
				SELECT
					c.table_schema,
					c.table_name,
					c.column_name,
					c.data_type,
					c.is_nullable,
					c.column_default
				FROM information_schema.columns c
				WHERE c.table_schema NOT IN ('pg_catalog', 'information_schema')
				ORDER BY c.table_schema, c.table_name, c.ordinal_position`)
			if err != nil {
				return "", err
			}
			defer rows.Close()

			var result strings.Builder
			currentTable := ""
			for rows.Next() {
				var schema, table, column, dataType, nullable, defaultVal sql.NullString
				rows.Scan(&schema, &table, &column, &dataType, &nullable, &defaultVal)

				fullTable := fmt.Sprintf("%s.%s", schema.String, table.String)
				if fullTable != currentTable {
					if currentTable != "" {
						result.WriteString("\n")
					}
					result.WriteString(fmt.Sprintf("## %s\n", fullTable))
					currentTable = fullTable
				}

				nullStr := "NOT NULL"
				if nullable.String == "YES" {
					nullStr = "NULLABLE"
				}

				line := fmt.Sprintf("  %-30s %-15s %s", column.String, dataType.String, nullStr)
				if defaultVal.Valid {
					line += fmt.Sprintf(" DEFAULT %s", defaultVal.String)
				}
				result.WriteString(line + "\n")
			}
			return result.String(), rows.Err()
		},
	})

	// Prompt: query helper
	srv.AddPrompt(gomcp.Prompt{
		Name:        "explore-table",
		Description: "Generate a prompt to explore a specific database table",
		Arguments: []gomcp.PromptArg{
			{Name: "table", Description: "Table name (schema.name format)", Required: true},
			{Name: "question", Description: "What do you want to know about this table?", Required: true},
		},
		Handler: func(ctx context.Context, args map[string]any) ([]gomcp.PromptMessage, error) {
			table := args["table"].(string)
			question := args["question"].(string)

			return []gomcp.PromptMessage{
				{Role: "user", Content: gomcp.NewTextContent(fmt.Sprintf(
					"I need to explore the %s table.\n\n"+
						"First, look at the database schema to understand the columns.\n"+
						"Then, run a query to answer this question: %s\n\n"+
						"Only use SELECT statements. Show me the SQL before executing.",
					table, question,
				))},
			}, nil
		},
	})

	// Prompt template for data analysis
	srv.AddPrompt(gomcp.Prompt{
		Name:        "data-analysis",
		Description: "Prompt template for analyzing data patterns",
		Arguments: []gomcp.PromptArg{
			{Name: "topic", Description: "What aspect of the data to analyze", Required: true},
		},
		Handler: func(ctx context.Context, args map[string]any) ([]gomcp.PromptMessage, error) {
			return []gomcp.PromptMessage{
				{Role: "user", Content: gomcp.NewTextContent(fmt.Sprintf(
					"Analyze the database for %s.\n"+
						"1. First, list all tables to understand the schema\n"+
						"2. Read the full schema resource to understand columns\n"+
						"3. Run targeted queries to extract insights\n"+
						"4. Summarize findings with specific data points\n\n"+
						"Use only SELECT queries. Be thorough.",
					args["topic"],
				))},
			}, nil
		},
	})

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// init registers unused import usage for compilation
var _ = json.Marshal
