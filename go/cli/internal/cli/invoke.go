package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"trpc.group/trpc-go/trpc-a2a-go/auth"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

type InvokeCfg struct {
	Config      *config.Config
	Task        string
	File        string
	Session     string
	Agent       string
	Stream      bool
	URLOverride string
	Headers     []string
}

func InvokeCmd(ctx context.Context, cfg *InvokeCfg) {

	clientSet := cfg.Config.Client()

	if err := CheckServerConnection(clientSet); err != nil {
		// If a connection does not exist, start a short-lived port-forward.
		pf, err := NewPortForward(ctx, cfg.Config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
			return
		}
		defer pf.Stop()
	}

	var task string
	// If task is set, use it. Otherwise, read from file or stdin.
	if cfg.Task != "" {
		task = cfg.Task
	} else if cfg.File != "" {
		switch cfg.File {
		case "-":
			// Read from stdin
			content, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading from stdin: %v\n", err)
				return
			}
			task = string(content)
		default:
			// Read from file
			content, err := os.ReadFile(cfg.File)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading from file: %v\n", err)
				return
			}
			task = string(content)
		}
	} else {
		fmt.Fprintln(os.Stderr, "Task or file is required")
		return
	}

	var a2aClient *a2aclient.A2AClient
	var err error
	a2aURL := cfg.URLOverride
	if a2aURL == "" {
		if cfg.Agent == "" {
			fmt.Fprintln(os.Stderr, "Agent is required")
			return
		}
		a2aURL = fmt.Sprintf("%s/api/a2a/%s/%s", cfg.Config.KAgentURL, cfg.Config.Namespace, cfg.Agent)
	}
	a2aClient, err = a2aclient.NewA2AClient(a2aURL, a2aclient.WithTimeout(cfg.Config.Timeout), withHeaders(cfg.Headers))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating A2A client: %v\n", err)
		return
	}

	var sessionID *string
	if cfg.Session != "" {
		sessionID = &cfg.Session
	}

	// Use A2A client to send message
	if cfg.Stream {
		ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
		defer cancel()

		result, err := a2aClient.StreamMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:      protocol.KindMessage,
				Role:      protocol.MessageRoleUser,
				ContextID: sessionID,
				Parts:     []protocol.Part{protocol.NewTextPart(task)},
			},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error invoking session: %v\n", err)
			return
		}
		StreamA2AEvents(result, cfg.Config.Verbose)
	} else {
		ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
		defer cancel()

		result, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:      protocol.KindMessage,
				Role:      protocol.MessageRoleUser,
				ContextID: sessionID,
				Parts:     []protocol.Part{protocol.NewTextPart(task)},
			},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error invoking session: %v\n", err)
			return
		}

		jsn, err := result.MarshalJSON()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling result: %v\n", err)
			return
		}

		fmt.Fprintf(os.Stdout, "%+v\n", string(jsn)) //nolint:errcheck
	}
}

// headerAuthProvider implements auth.ClientProvider to add custom headers
type headerAuthProvider struct {
	headers map[string]string
}

// Authenticate implements auth.Provider interface
func (h *headerAuthProvider) Authenticate(r *http.Request) (*auth.User, error) {
	// Do nothing - headers are added by the transport
	return nil, nil
}

// ConfigureClient implements auth.ClientProvider interface
func (h *headerAuthProvider) ConfigureClient(client *http.Client) *http.Client {
	// Create a transport that adds the custom headers to requests
	transport := &headerAuthTransport{
		base:    client.Transport,
		headers: h.headers,
	}

	// If the client transport is nil, initialize with http.DefaultTransport
	if transport.base == nil {
		transport.base = http.DefaultTransport
	}

	// Return a new client with our custom transport
	newClient := *client
	newClient.Transport = transport
	return &newClient
}

// headerAuthTransport is an http.RoundTripper that adds custom headers.
type headerAuthTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

// RoundTrip implements http.RoundTripper.
func (t *headerAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid modifying the original
	reqClone := req.Clone(req.Context())

	// Add custom headers to the request
	for key, value := range t.headers {
		reqClone.Header.Set(key, value)
	}

	// Continue with the base transport
	return t.base.RoundTrip(reqClone)
}

func withHeaders(headers []string) a2aclient.Option {
	if len(headers) == 0 {
		return func(opts *a2aclient.A2AClient) {}
	}

	// Convert []string to map[string]string
	headerMap := make(map[string]string)
	for _, header := range headers {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			headerMap[key] = value
		}
	}

	return a2aclient.WithAuthProvider(&headerAuthProvider{headers: headerMap})
}
