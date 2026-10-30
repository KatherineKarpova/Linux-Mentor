
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

func getStatus(
    ctx context.Context,
    req *mcp.CallToolRequest,
    input statusInput,
) (*mcp.CallToolResult, statusOutput, error) {
    return nil, statusOutput{
        Message: "Linux-Mentor MCP server connected",
    }, nil
}

func main() {
    server := mcp.NewServer(		// creates MCP server object
        &mcp.Implementation{
            Name:    "linux-mentor",
            Version: "0.1.0",
        },
        nil,
    )

    mcp.AddTool(		// registers function Gemini CLI can discover and call
        server,
        &mcp.Tool{
            Name:        "get_status",
            Description: "Check Linux-Mentor server status",
        },
        getStatus,
    )

    if err := server.Run(		// starts the server
        context.Background(),
        &mcp.StdioTransport{},		// exchanges MCP messages through the standard input & output
    ); err != nil {
        log.Fatal(err)
    }
}
