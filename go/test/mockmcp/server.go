package mockmcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type requestContextKey struct{}
type Server struct {
	Addr        net.Addr
	LastHeaders atomic.Pointer[http.Header]

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
	s := &Server{
		Addr:     listener.Addr(),
		listener: listener,
	}
	// Add tool handler
	srv.AddTool(tool, s.kebabHandler)
	mux := http.NewServeMux()
	mux.Handle("/mcp", server.NewStreamableHTTPServer(srv, server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
		return context.WithValue(ctx, requestContextKey{}, r)
	})))

	s.httpSrv = &http.Server{
		Addr:    ":0",
		Handler: mux,
	}
	return s, nil
}

func (s *Server) Start(ctx context.Context) string {
	s.httpSrv.BaseContext = func(net.Listener) context.Context {
		return ctx
	}
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

func (s *Server) kebabHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	httpRequest := ctx.Value(requestContextKey{}).(*http.Request)
	headers := httpRequest.Header
	s.LastHeaders.Store(&headers)
	name := request.GetString("type", "lamb")
	return mcp.NewToolResultText(fmt.Sprintf("Your kebab is ready. it is made from: %s!", name)), nil
}
