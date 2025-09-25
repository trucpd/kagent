package mockmcp

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type Server struct {
	Addr     net.Addr
	listener net.Listener
	httpSrv  *http.Server
}

func NewServer(port uint16) (*Server, error) {

	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}
	// Create a new MCP server
	srv := server.NewMCPServer(
		"Demo",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	// Add tool
	tool := mcp.NewTool("make_kebab",
		mcp.WithDescription("Makes a kebab for someone"),
		mcp.WithString("type",
			mcp.Description("Type of kebab to make"),
		),
	)

	// Add tool handler
	srv.AddTool(tool, kebabHandler)
	mux := http.NewServeMux()
	mux.Handle("/mcp", server.NewStreamableHTTPServer(srv))
	httpSrv := &http.Server{
		Addr:    ":0",
		Handler: mux,
	}
	return &Server{
		Addr:     listener.Addr(),
		listener: listener,
		httpSrv:  httpSrv,
	}, nil
}

func (s *Server) Start() string {
	// start the server in a goroutine, get the port it started on and return it
	go func() {
		if err := s.httpSrv.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			fmt.Printf("HTTP server Serve: %v", err)
		}
	}()
	return fmt.Sprintf("http://%s", s.listener.Addr().String())
}
func (s *Server) Stop() {
	s.httpSrv.Shutdown(context.Background())
}

func kebabHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := request.GetString("type", "lamb")
	return mcp.NewToolResultText(fmt.Sprintf("Your kebab is ready. it is made from: %s!", name)), nil
}
