package e2e_test

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s_runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	"github.com/kagent-dev/kagent/go/test/mockllm"
	"github.com/kagent-dev/kagent/go/test/mockmcp"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

//go:embed mocks
var mocks embed.FS

func a2aUrl(namespace, name string) string {
	kagentURL := os.Getenv("KAGENT_URL")
	if kagentURL == "" {
		// if running locally on kind, do "kubectl port-forward -n kagent deployments/kagent-controller 8083"
		kagentURL = "http://localhost:8083"
	}
	// A2A URL format: <base_url>/<namespace>/<agent_name>
	return kagentURL + "/api/a2a/" + namespace + "/" + name
}

func generateModelCfg(baseURL string) *v1alpha2.ModelConfig {
	return &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model-config",
			Namespace: "kagent",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:           "gpt-4.1-mini",
			APIKeySecret:    "kagent-openai",
			APIKeySecretKey: "OPENAI_API_KEY",
			Provider:        v1alpha2.ModelProviderOpenAI,
			OpenAI: &v1alpha2.OpenAIConfig{
				BaseURL: baseURL,
			},
		},
	}
}

func generateRemoteMcpServer(s *mockmcp.Server) *v1alpha2.RemoteMCPServer {
	addr := s.Addr
	port := addr.(*net.TCPAddr).Port
	return &v1alpha2.RemoteMCPServer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha2.GroupVersion.String(),
			Kind:       "RemoteMCPServer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mock-mcp-server",
			Namespace: "kagent",
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description:      "Mock MCP Server",
			Protocol:         v1alpha2.RemoteMCPServerProtocolStreamableHttp,
			SseReadTimeout:   &metav1.Duration{Duration: 5 * time.Minute}, // 5 minutes
			TerminateOnClose: ptr.To(true),
			Timeout:          &metav1.Duration{Duration: 30 * time.Second}, // 30 seconds
			URL:              fmt.Sprintf("http://%s:%d/mcp", localhost(), port),
		},
	}
}

func generateAgent() *v1alpha2.Agent {
	return &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-agent",
			Namespace: "kagent",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				ModelConfig:   "test-model-config",
				SystemMessage: "You are a test agent. The system prompt doesn't matter because we're using a mock server.",
				Tools: []*v1alpha2.Tool{
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								ApiGroup: "kagent.dev",
								Kind:     "RemoteMCPServer",
								Name:     "kagent-tool-server",
							},
							ToolNames: []string{"k8s_get_resources"},
						},
					},
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedLocalReference: v1alpha2.TypedLocalReference{
								ApiGroup: "kagent.dev",
								Kind:     "RemoteMCPServer",
								Name:     "mock-mcp-server",
							},
							ToolNames: []string{"make_kebab"},
						},
					},
				},
			},
		},
	}
}

func localhost() string {
	switch runtime.GOOS {
	case "darwin":
		return "host.docker.internal"
	case "linux":
		return "172.17.0.1"
	}

	if h := os.Getenv("KAGENT_LOCAL_HOST"); h != "" {
		return h
	}
	return "localhost"
}

func buildK8sURL(baseURL string) string {
	// Get the port from the listener address
	splitted := strings.Split(baseURL, ":")
	port := splitted[len(splitted)-1]
	// Check local OS and use the correct local host
	return fmt.Sprintf("http://%s:%s", localhost(), port)
}

func getHelperService(ctx context.Context, cli client.Client) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	// create additional service to test-agent, with LB type
	namespace, name := "kagent", "test-agent2"

	svc := &v1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{
				{
					Name:       "http",
					Protocol:   v1.ProtocolTCP,
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
			Selector: map[string]string{
				"app":    "kagent",
				"kagent": "test-agent",
			},
		},
	}
	err := cli.Patch(ctx, svc, client.Apply, client.ForceOwnership, client.FieldOwner("e2e-test"))
	if err != nil {
		return "", fmt.Errorf("failed to patch service: %w", err)
	}

	for {
		err = cli.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, svc)
		if err != nil {
			return "", fmt.Errorf("failed to get service after patching: %w", err)
		}
		// get lb ip:
		if ip := getIp(svc); ip != "" {

			// verify that we can connect to the ip:8080
			for {
				if err := testConnect(ctx, ip); err != nil {
					// not ready yet
					time.Sleep(time.Second / 10)
					continue
				}
				break
			}
			return ip, nil
		}
		time.Sleep(time.Second / 2)
	}
}

func testConnect(ctx context.Context, ip string) error {
	var dialer net.Dialer
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, "8080"))
	if err != nil {
		return fmt.Errorf("failed to connect to %s:8080: %w", ip, err)
	}
	conn.Close()
	return nil
}

func getIp(svc *v1.Service) string {
	if len(svc.Status.LoadBalancer.Ingress) == 0 {
		return ""
	}
	return svc.Status.LoadBalancer.Ingress[0].IP
}

func TestInvokeInlineAgent(t *testing.T) {

	mockllmCfg, err := mockllm.LoadConfigFromFile("mocks/invoke_inline_agent.json", mocks)
	require.NoError(t, err)

	server := mockllm.NewServer(mockllmCfg)
	baseURL, err := server.Start()
	baseURL = buildK8sURL(baseURL)
	require.NoError(t, err)
	t.Cleanup(func() { server.Stop() })

	mcpServer, err := mockmcp.NewServer(0)
	require.NoError(t, err)
	mcpServer.Start()
	require.NoError(t, err)
	t.Cleanup(func() { mcpServer.Stop() })

	cfg, err := config.GetConfig()
	require.NoError(t, err)

	scheme := k8s_runtime.NewScheme()
	err = v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)
	err = v1.AddToScheme(scheme)
	require.NoError(t, err)

	cli, err := client.New(cfg, client.Options{
		Scheme: scheme,
	})
	require.NoError(t, err)

	remoteMcp := generateRemoteMcpServer(mcpServer)
	err = cli.Patch(t.Context(), remoteMcp, client.Apply, client.ForceOwnership, client.FieldOwner("e2e-test"))
	require.NoError(t, err)

	defer cli.Delete(t.Context(), remoteMcp) //nolint:errcheck

	modelCfg := generateModelCfg(baseURL + "/v1")
	agent := generateAgent()
	cli.Delete(t.Context(), modelCfg) //nolint:errcheck
	cli.Delete(t.Context(), agent)    //nolint:errcheck
	err = cli.Create(t.Context(), modelCfg)
	require.NoError(t, err)
	err = cli.Create(t.Context(), agent)
	require.NoError(t, err)

	defer func() {
		cli.Delete(t.Context(), modelCfg) //nolint:errcheck
		cli.Delete(t.Context(), agent)    //nolint:errcheck
	}()

	args := []string{
		"wait",
		"--for",
		"condition=Ready",
		"--timeout=1m",
		"agents.kagent.dev",
		"test-agent",
		"-n",
		"kagent",
	}

	cmd := exec.CommandContext(t.Context(), "kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())

	// Setup
	a2aURL := a2aUrl("kagent", "test-agent")

	a2aClient, err := a2aclient.NewA2AClient(a2aURL)
	require.NoError(t, err)

	t.Run("sync_invocation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		msg, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:  protocol.KindMessage,
				Role:  protocol.MessageRoleUser,
				Parts: []protocol.Part{protocol.NewTextPart("List all nodes in the cluster")},
			},
		})
		require.NoError(t, err)

		taskResult, ok := msg.Result.(*protocol.Task)
		require.True(t, ok)
		text := a2a.ExtractText(taskResult.History[len(taskResult.History)-1])
		jsn, err := json.Marshal(taskResult)
		require.NoError(t, err)
		require.Contains(t, text, "kagent-control-plane", string(jsn))
	})

	t.Run("sync invocation with token propagation", func(t *testing.T) {
		agentIp, err := getHelperService(t.Context(), cli)
		require.NoError(t, err)
		defer func() {
			svc := &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "kagent",
					Name:      "test-agent2",
				},
			}
			cli.Delete(t.Context(), svc) //nolint:errcheck
		}()

		a2aClient, err := a2aclient.NewA2AClient(fmt.Sprintf("http://%s:8080/", agentIp), a2aclient.WithAPIKeyAuth("Bearer token-to-propagate", "authorization"))
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		msg, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:  protocol.KindMessage,
				Role:  protocol.MessageRoleUser,
				Parts: []protocol.Part{protocol.NewTextPart("Make a kebab")},
			},
		})
		require.NoError(t, err)

		taskResult, ok := msg.Result.(*protocol.Task)
		require.True(t, ok)
		require.Equal(t, protocol.TaskStateCompleted, taskResult.Status.State)

		text := a2a.ExtractText(taskResult.History[len(taskResult.History)-1])
		jsn, err := json.Marshal(taskResult)
		require.NoError(t, err)
		require.Contains(t, text, "I have made you a lamb kebab. Enjoy!", string(jsn))
	})

	t.Run("streaming_invocation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		msg, err := a2aClient.StreamMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:  protocol.KindMessage,
				Role:  protocol.MessageRoleUser,
				Parts: []protocol.Part{protocol.NewTextPart("List all nodes in the cluster")},
			},
		})
		require.NoError(t, err)

		resultList := []protocol.StreamingMessageEvent{}
		var text string
		for event := range msg {
			msgResult, ok := event.Result.(*protocol.TaskStatusUpdateEvent)
			if !ok {
				continue
			}
			if msgResult.Status.Message != nil {
				text += a2a.ExtractText(*msgResult.Status.Message)
			}
			resultList = append(resultList, event)
		}
		jsn, err := json.Marshal(resultList)
		require.NoError(t, err)
		require.Contains(t, string(jsn), "kagent-control-plane", string(jsn))
	})
}

func TestInvokeExternalAgent(t *testing.T) {
	// Setup
	a2aURL := a2aUrl("kagent", "kebab-agent")

	a2aClient, err := a2aclient.NewA2AClient(a2aURL)
	require.NoError(t, err)

	t.Run("sync_invocation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		msg, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:  protocol.KindMessage,
				Role:  protocol.MessageRoleUser,
				Parts: []protocol.Part{protocol.NewTextPart("What can you do?")},
			},
		})
		require.NoError(t, err)

		taskResult, ok := msg.Result.(*protocol.Task)
		require.True(t, ok)
		text := a2a.ExtractText(taskResult.History[len(taskResult.History)-1])
		jsn, err := json.Marshal(taskResult)
		require.NoError(t, err)
		require.Contains(t, text, "kebab", string(jsn))
	})

	t.Run("streaming_invocation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		msg, err := a2aClient.StreamMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:  protocol.KindMessage,
				Role:  protocol.MessageRoleUser,
				Parts: []protocol.Part{protocol.NewTextPart("What can you do?")},
			},
		})
		require.NoError(t, err)

		resultList := []protocol.StreamingMessageEvent{}
		var text string
		for event := range msg {
			msgResult, ok := event.Result.(*protocol.TaskStatusUpdateEvent)
			if !ok {
				continue
			}
			if msgResult.Status.Message != nil {
				text += a2a.ExtractText(*msgResult.Status.Message)
			}
			resultList = append(resultList, event)
		}
		jsn, err := json.Marshal(resultList)
		require.NoError(t, err)
		require.Contains(t, string(jsn), "kebab", string(jsn))
	})

	t.Run("invocation with different user", func(t *testing.T) {

		a2aClient, err := a2aclient.NewA2AClient(a2aURL, a2aclient.WithAPIKeyAuth("user@example.com", "x-user-id"))
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		msg, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:  protocol.KindMessage,
				Role:  protocol.MessageRoleUser,
				Parts: []protocol.Part{protocol.NewTextPart("What can you do?")},
			},
		})
		require.NoError(t, err)

		taskResult, ok := msg.Result.(*protocol.Task)
		require.True(t, ok)
		text := a2a.ExtractText(taskResult.History[len(taskResult.History)-1])
		jsn, err := json.Marshal(taskResult)
		require.NoError(t, err)
		require.Contains(t, text, "kebab for user@example.com", string(jsn))
	})
}
