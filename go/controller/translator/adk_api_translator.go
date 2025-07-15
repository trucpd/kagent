package translator

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type StreamableHTTPConnectionParams struct {
	Url              string            `json:"url"`
	Headers          map[string]string `json:"headers"`
	Timeout          float64           `json:"timeout"`
	ReadTimeout      float64           `json:"read_timeout"`
	TerminateOnClose bool              `json:"terminate_on_close"`
}

type StdioServerParameters struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Cwd     *string           `json:"cwd"`
}

type StdioConnectionParams struct {
	ServerParams StdioServerParameters `json:"server_params"`
	Timeout      float64               `json:"timeout"`
}

type AgentConfig struct {
	Name        string                           `json:"name"`
	Model       string                           `json:"model"`
	Description string                           `json:"description"`
	Instruction string                           `json:"instruction"`
	HttpTools   []StreamableHTTPConnectionParams `json:"http_tools"`
	StdioTools  []StdioConnectionParams          `json:"stdio_tools"`
	Agents      []string                         `json:"agents"`
}

var adkLog = ctrllog.Log.WithName("adk")

type AdkApiTranslator interface {
	TranslateAgent(
		ctx context.Context,
		agent *v1alpha1.Agent,
	) (*AgentConfig, error)
}

type adkApiTranslator struct {
	kube               client.Client
	defaultModelConfig types.NamespacedName
}

func (a *adkApiTranslator) TranslateAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
) (*AgentConfig, error) {
	stream := true
	if agent.Spec.Stream != nil {
		stream = *agent.Spec.Stream
	}
	opts := defaultTeamOptions()
	opts.stream = stream
	return a.translateAgent(ctx, agent, opts, &tState{})
}

func (a *adkApiTranslator) translateAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
	opts *teamOptions,
	state *tState,
) (*AgentConfig, error) {

	cfg := &AgentConfig{
		Name:        common.GetObjectRef(agent),
		Model:       "gemini-2.0-flash",
		Description: agent.Spec.Description,
		Instruction: agent.Spec.SystemMessage,
	}

	toolsByServer := make(map[string][]string)
	for _, tool := range agent.Spec.Tools {
		// Skip tools that are not applicable to the model provider
		switch {
		case tool.Type == v1alpha1.ToolProviderType_McpServer && tool.McpServer != nil:
			for _, toolName := range tool.McpServer.ToolNames {
				toolsByServer[tool.McpServer.ToolServer] = append(toolsByServer[tool.McpServer.ToolServer], toolName)
			}
		case tool.Type == v1alpha1.ToolProviderType_Agent && tool.Agent != nil:
			toolNamespacedName, err := common.ParseRefString(tool.Agent.Ref, agent.Namespace)
			if err != nil {
				return nil, err
			}

			toolRef := toolNamespacedName.String()
			agentRef := common.GetObjectRef(agent)

			if toolRef == agentRef {
				return nil, fmt.Errorf("agent tool cannot be used to reference itself, %s", agentRef)
			}

			if state.isVisited(toolRef) {
				return nil, fmt.Errorf("cycle detected in agent tool chain: %s -> %s", agentRef, toolRef)
			}

			if state.depth > MAX_DEPTH {
				return nil, fmt.Errorf("recursion limit reached in agent tool chain: %s -> %s", agentRef, toolRef)
			}

			// Translate a nested tool
			toolAgent := &v1alpha1.Agent{}

			err = common.GetObject(
				ctx,
				a.kube,
				toolAgent,
				toolRef,
				agent.Namespace, // redundant
			)
			if err != nil {
				return nil, err
			}

			cfg.Agents = append(cfg.Agents, common.GetObjectRef(toolAgent))

		default:
			return nil, fmt.Errorf("tool must have a provider or tool server")
		}
	}
	for server, tools := range toolsByServer {
		err := a.translateToolServerTool(ctx, cfg, server, tools, agent.Namespace)
		if err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func (a *adkApiTranslator) translateHttpTool(ctx context.Context, tool *v1alpha1.StreamableHttpServerConfig, namespace string) (*StreamableHTTPConnectionParams, error) {
	headers := make(map[string]string)
	for _, header := range tool.HeadersFrom {
		if header.Value != "" {
			headers[header.Name] = header.Value
		} else if header.ValueFrom != nil {
			value, err := resolveValueSource(ctx, a.kube, header.ValueFrom, namespace)
			if err != nil {
				return nil, err
			}
			headers[header.Name] = value
		}
	}
	return &StreamableHTTPConnectionParams{
		Url:              tool.URL,
		Headers:          headers,
		Timeout:          tool.Timeout.Seconds(),
		ReadTimeout:      tool.SseReadTimeout.Seconds(),
		TerminateOnClose: tool.TerminateOnClose,
	}, nil
}

func (a *adkApiTranslator) translateStdioTool(ctx context.Context, tool *v1alpha1.StdioMcpServerConfig, namespace string) (*StdioConnectionParams, error) {
	envMap := make(map[string]string)
	for key, val := range tool.Env {
		envMap[key] = val
	}
	for _, envVar := range tool.EnvFrom {
		if envVar.Value != "" {
			envMap[envVar.Name] = envVar.Value
		} else if envVar.ValueFrom != nil {
			value, err := resolveValueSource(ctx, a.kube, envVar.ValueFrom, namespace)
			if err != nil {
				return nil, err
			}
			envMap[envVar.Name] = value
		}
	}
	return &StdioConnectionParams{
		ServerParams: StdioServerParameters{
			Command: tool.Command,
			Args:    tool.Args,
			Env:     envMap,
		},
		Timeout: float64(tool.ReadTimeoutSeconds),
	}, nil
}

func (a *adkApiTranslator) translateToolServerTool(ctx context.Context, agent *AgentConfig, toolServerRef string, toolNames []string, defaultNamespace string) error {
	toolServerObj := &v1alpha1.ToolServer{}
	err := common.GetObject(
		ctx,
		a.kube,
		toolServerObj,
		toolServerRef,
		defaultNamespace,
	)
	if err != nil {
		return err
	}

	switch {
	case toolServerObj.Spec.Config.Stdio != nil:
		tool, err := a.translateStdioTool(ctx, toolServerObj.Spec.Config.Stdio, defaultNamespace)
		if err != nil {
			return err
		}
		agent.StdioTools = append(agent.StdioTools, *tool)
	case toolServerObj.Spec.Config.Sse != nil:
		return fmt.Errorf("SSE tool servers are not supported")
	case toolServerObj.Spec.Config.StreamableHttp != nil:
		tool, err := a.translateHttpTool(ctx, toolServerObj.Spec.Config.StreamableHttp, defaultNamespace)
		if err != nil {
			return err
		}
		agent.HttpTools = append(agent.HttpTools, *tool)
	}
	return nil
}
