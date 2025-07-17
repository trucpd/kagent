package translator

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/internal/adk"
	"github.com/kagent-dev/kagent/go/internal/database"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

var adkLog = ctrllog.Log.WithName("adk")

type AdkApiTranslator interface {
	TranslateAgent(
		ctx context.Context,
		agent *v1alpha1.Agent,
	) (*database.Agent, error)
	TranslateToolServer(ctx context.Context, toolServer *v1alpha1.ToolServer) (*database.ToolServer, error)
}

func NewAdkApiTranslator(kube client.Client, defaultModelConfig types.NamespacedName) AdkApiTranslator {
	return &adkApiTranslator{
		kube:               kube,
		defaultModelConfig: defaultModelConfig,
	}
}

type adkApiTranslator struct {
	kube               client.Client
	defaultModelConfig types.NamespacedName
}

func (a *adkApiTranslator) TranslateAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
) (*database.Agent, error) {
	stream := true
	if agent.Spec.Stream != nil {
		stream = *agent.Spec.Stream
	}
	opts := defaultTeamOptions()
	opts.stream = stream
	adkAgent, err := a.translateAgent(ctx, agent, opts, &tState{})
	if err != nil {
		return nil, err
	}
	return &database.Agent{
		Name:   adkAgent.Name,
		Config: adkAgent,
	}, nil
}

func (a *adkApiTranslator) translateAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
	opts *teamOptions,
	state *tState,
) (*adk.AgentConfig, error) {

	cfg := &adk.AgentConfig{
		Name:        common.ConvertToPythonIdentifier(common.GetObjectRef(agent)),
		Model:       "gemini-2.0-flash",
		Description: agent.Spec.Description,
		Instruction: agent.Spec.SystemMessage,
	}

	toolsByServer := make(map[string][]string)
	for _, tool := range agent.Spec.Tools {
		// Skip tools that are not applicable to the model provider
		switch {
		case tool.McpServer != nil:
			for _, toolName := range tool.McpServer.ToolNames {
				toolsByServer[tool.McpServer.ToolServer] = append(toolsByServer[tool.McpServer.ToolServer], toolName)
			}
		case tool.Agent != nil:
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

			toolAgentCfg, err := a.translateAgent(ctx, toolAgent, opts, state.with(agent))
			if err != nil {
				return nil, err
			}

			cfg.Agents = append(cfg.Agents, *toolAgentCfg)

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

func (a *adkApiTranslator) translateStreamableHttpTool(ctx context.Context, tool *v1alpha1.ToolServerConfig, namespace string) (*adk.StreamableHTTPConnectionParams, error) {
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
	return &adk.StreamableHTTPConnectionParams{
		Url:              tool.URL,
		Headers:          headers,
		Timeout:          tool.Timeout.Seconds(),
		ReadTimeout:      tool.SseReadTimeout.Seconds(),
		TerminateOnClose: tool.TerminateOnClose,
	}, nil
}

func (a *adkApiTranslator) translateSseHttpTool(ctx context.Context, tool *v1alpha1.ToolServerConfig, namespace string) (*adk.SseConnectionParams, error) {
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
	return &adk.SseConnectionParams{
		Url:         tool.URL,
		Headers:     headers,
		Timeout:     tool.Timeout.Seconds(),
		ReadTimeout: tool.SseReadTimeout.Seconds(),
	}, nil
}

func (a *adkApiTranslator) translateToolServerTool(ctx context.Context, agent *adk.AgentConfig, toolServerRef string, toolNames []string, defaultNamespace string) error {
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
	case toolServerObj.Spec.Config.Protocol == v1alpha1.ToolServerProtocolSse:
		tool, err := a.translateSseHttpTool(ctx, &toolServerObj.Spec.Config, defaultNamespace)
		if err != nil {
			return err
		}
		agent.SseTools = append(agent.SseTools, adk.SseMcpServerConfig{
			Params: *tool,
			Tools:  toolNames,
		})
	case toolServerObj.Spec.Config.Protocol == v1alpha1.ToolServerProtocolStreamableHttp:
		tool, err := a.translateStreamableHttpTool(ctx, &toolServerObj.Spec.Config, defaultNamespace)
		if err != nil {
			return err
		}
		agent.HttpTools = append(agent.HttpTools, adk.HttpMcpServerConfig{
			Params: *tool,
			Tools:  toolNames,
		})
	}
	return nil
}

func (a *adkApiTranslator) TranslateToolServer(ctx context.Context, toolServer *v1alpha1.ToolServer) (*database.ToolServer, error) {
	return &database.ToolServer{
		Name:        common.GetObjectRef(toolServer),
		Description: toolServer.Spec.Description,
		Config:      toolServer.Spec.Config,
	}, nil
}
