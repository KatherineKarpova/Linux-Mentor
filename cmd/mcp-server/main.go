package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type statusInput struct{}

type statusOutput struct {
	Message string `json:"message"`
}

// getstatus returns a small health response to verify the mcp connection.
func getStatus(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input statusInput,
) (*mcp.CallToolResult, statusOutput, error) {
	return nil, statusOutput{
		Message: "Linux-Mentor MCP server connected",
	}, nil
}

// main registers the status tool and serves mcp requests over standard input and output.
func main() {
	// create one server instance to own the tool registry and stdio transport.
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "linux-mentor",
			Version: "0.1.0",
		},
		nil,
	)

	// register a lightweight health tool so the host can verify connectivity.
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "get_status",
			Description: "Check Linux-Mentor server status",
		},
		getStatus,
	)

	// todo: register distribution detection and linux teaching tools when implemented.
	// run the server with stdio so protocol messages stay separate from terminal output.
	if err := server.Run(
		context.Background(),
		&mcp.StdioTransport{},
	); err != nil {
		log.Fatal(err)
	}
}
