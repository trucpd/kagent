package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/kagent-dev/kagent/go/test/mockllm"
	"github.com/kagent-dev/kagent/go/test/mockmcp"
)

func startMcp() {

	mcpServer, err := mockmcp.NewServer(8092)
	if err != nil {
		log.Fatalf("Failed to create MCP server: %v", err)
	}
	mcpServer.Start()
	fmt.Printf("Mock MCP server started at: %s\n", mcpServer.Addr.String())
}

func main() {
	var mockFile string
	flag.StringVar(&mockFile, "mock-file", "", "Path to the mock configuration file")
	flag.Parse()

	if mockFile == "" {
		fmt.Fprintf(os.Stderr, "Error: -mock-file flag is required\n")
		flag.Usage()
		os.Exit(1)
	}

	data, err := os.ReadFile(mockFile)
	if err != nil {
		log.Fatalf("Failed to read mock file %s: %v", mockFile, err)
	}

	mockllmCfg, err := mockllm.LoadConfig(data)
	if err != nil {
		log.Fatalf("Failed to load config from file %s: %v", mockFile, err)
	}

	server := mockllm.NewServer(mockllmCfg)
	server.Address = "0.0.0.0:8091"
	baseURL, err := server.Start()
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	fmt.Printf("Mock LLM server started at: %s\n", baseURL)

	startMcp()
	// Keep the server running
	select {}
}
