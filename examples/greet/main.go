package main

import (
	"context"
	"fmt"
	"os"

	"github.com/BackendStack21/go-mcp/gomcp"
)

func main() {
	srv := gomcp.NewServer("greeter", "1.0.0")

	// Tool: greet
	srv.AddTool(gomcp.Tool{
		Name:        "greet",
		Description: "Greet a person by name",
		InputSchema: gomcp.InputSchema{
			Type: "object",
			Properties: map[string]gomcp.Property{
				"name": {Type: "string", Description: "The name to greet"},
			},
			Required: []string{"name"},
		},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			name, ok := args["name"].(string)
			if !ok {
				return "", fmt.Errorf("name must be a string")
			}
			return fmt.Sprintf("Hello, %s!", name), nil
		},
	})

	// Resource: greeting
	srv.AddResource(gomcp.Resource{
		URI:         "greeting://world",
		Name:        "World Greeting",
		Description: "A greeting for the world",
		MimeType:    "text/plain",
		Handler: func(ctx context.Context) (string, error) {
			return "Hello, World! 🌍", nil
		},
	})

	// Prompt: polite-greeting
	srv.AddPrompt(gomcp.Prompt{
		Name:        "polite-greeting",
		Description: "A polite greeting prompt template",
		Arguments: []gomcp.PromptArg{
			{Name: "name", Description: "The name to greet", Required: true},
		},
		Handler: func(ctx context.Context, args map[string]any) ([]gomcp.PromptMessage, error) {
			name := args["name"].(string)
			return []gomcp.PromptMessage{
				{Role: "user", Content: gomcp.NewTextContent("Please greet " + name + " in a friendly and polite manner.")},
			}, nil
		},
	})

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
