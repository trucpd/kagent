package translator

import (
	"context"
	"fmt"
	"slices"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/internal/adk"
	"github.com/kagent-dev/kagent/go/internal/database"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type AgentOutputs struct {
	Deployment *appsv1.Deployment `json:"deployment,omitempty"`
	Service    *corev1.Service    `json:"service,omitempty"`
	ConfigMap  *corev1.ConfigMap  `json:"configMap,omitempty"`

	Config *adk.AgentConfig `json:"config,omitempty"`
}

var adkLog = ctrllog.Log.WithName("adk")

type AdkApiTranslator interface {
	TranslateAgent(
		ctx context.Context,
		agent *v1alpha1.Agent,
	) (*AgentOutputs, error)
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

const MAX_DEPTH = 10

type tState struct {
	// used to prevent infinite loops
	// The recursion limit is 10
	depth uint8
	// used to enforce DAG
	// The final member of the list will be the "parent" agent
	visitedAgents []string
}

func (s *tState) with(agent *v1alpha1.Agent) *tState {
	s.depth++
	s.visitedAgents = append(s.visitedAgents, common.GetObjectRef(agent))
	return s
}

func (t *tState) isVisited(agentName string) bool {
	return slices.Contains(t.visitedAgents, agentName)
}

func (a *adkApiTranslator) TranslateAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
) (*AgentOutputs, error) {
	outputs, err := a.translateOutputs(ctx, agent)
	if err != nil {
		return nil, err
	}

	switch agent.Spec.Type {
	case v1alpha1.AgentType_Declarative:
		adkAgent, err := a.translateDeclarativeAgent(ctx, agent, &tState{})
		if err != nil {
			return nil, err
		}
		outputs.Config = adkAgent
	case v1alpha1.AgentType_Framework:
		return nil, fmt.Errorf("framework agents are not supported yet")
	}

	return outputs, nil
}

func (a *adkApiTranslator) translateOutputs(ctx context.Context, agent *v1alpha1.Agent) (*AgentOutputs, error) {
	outputs := &AgentOutputs{}
	if agent.Spec.Deployment == nil {
		spec := defaultDeploymentSpec()
		outputs.Deployment = &appsv1.Deployment{
			ObjectMeta: agent.ObjectMeta,
			Spec: appsv1.DeploymentSpec{
				Replicas: spec.Replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app":     "kagent",
						"kagent":  agent.Name,
						"version": "v1alpha1",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: agent.Name + "-",
						Annotations:  agent.Annotations,
						Labels: map[string]string{
							"app":     "kagent",
							"kagent":  agent.Name,
							"version": "v1alpha1",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{spec.Container},
						Volumes:    spec.Volumes,
					},
				},
			},
		}
	} else {
		return nil, fmt.Errorf("We need to figure out merging the deployment spec")
	}

	outputs.Service = &corev1.Service{
		ObjectMeta: agent.ObjectMeta,
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app":     "kagent",
				"kagent":  agent.Name,
				"version": "v1alpha1",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	outputs.ConfigMap = &corev1.ConfigMap{
		ObjectMeta: agent.ObjectMeta,
		Data:       map[string]string{},
	}

	if err := controllerutil.SetControllerReference(agent, outputs.Deployment, a.kube.Scheme()); err != nil {
		return nil, err
	}

	if err := controllerutil.SetControllerReference(agent, outputs.Service, a.kube.Scheme()); err != nil {
		return nil, err
	}

	if err := controllerutil.SetControllerReference(agent, outputs.ConfigMap, a.kube.Scheme()); err != nil {
		return nil, err
	}

	return outputs, nil
}

func defaultDeploymentSpec() *v1alpha1.DeploymentSpec {
	return &v1alpha1.DeploymentSpec{
		Replicas: ptr.To(int32(1)),
		Container: corev1.Container{
			Name:            "kagent",
			Image:           "kagent-controller:latest",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"uv", "run", "kagent", "start", "--config", "/config/config.yaml"},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "config",
					MountPath: "/config",
				},
			},
		},
		Volumes: []corev1.Volume{
			{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "kagent-config",
						},
					},
				},
			},
		},
	}
}

func (a *adkApiTranslator) translateDeclarativeAgent(ctx context.Context, agent *v1alpha1.Agent, state *tState) (*adk.AgentConfig, error) {
	if agent.Spec.Declarative == nil {
		return nil, fmt.Errorf("declarative agent spec is nil")
	}

	declarativeAgentSpec := agent.Spec.Declarative

	cfg := &adk.AgentConfig{
		Name:        common.ConvertToPythonIdentifier(common.GetObjectRef(agent)),
		Model:       "gemini-2.0-flash",
		Description: declarativeAgentSpec.Description,
		Instruction: declarativeAgentSpec.SystemMessage,
	}

	toolsByServer := make(map[string][]string)
	for _, tool := range declarativeAgentSpec.Tools {
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

			var toolAgentCfg *adk.AgentConfig
			switch toolAgent.Spec.Type {
			case v1alpha1.AgentType_Declarative:
				toolAgentCfg, err = a.translateDeclarativeAgent(ctx, toolAgent, state.with(agent))
				if err != nil {
					return nil, err
				}
			case v1alpha1.AgentType_Framework:
				return nil, fmt.Errorf("cannot delegate to framework agents yet")
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

// resolveValueSource resolves a value from a ValueSource
func resolveValueSource(ctx context.Context, kube client.Client, source *v1alpha1.ValueSource, namespace string) (string, error) {
	if source == nil {
		return "", fmt.Errorf("source cannot be nil")
	}

	switch source.Type {
	case v1alpha1.ConfigMapValueSource:
		return getConfigMapValue(ctx, kube, source, namespace)
	case v1alpha1.SecretValueSource:
		return getSecretValue(ctx, kube, source, namespace)
	default:
		return "", fmt.Errorf("unknown value source type: %s", source.Type)
	}
}

// getConfigMapValue fetches a value from a ConfigMap
func getConfigMapValue(ctx context.Context, kube client.Client, source *v1alpha1.ValueSource, namespace string) (string, error) {
	if source == nil {
		return "", fmt.Errorf("source cannot be nil")
	}

	configMap := &corev1.ConfigMap{}
	err := common.GetObject(
		ctx,
		kube,
		configMap,
		source.ValueRef,
		namespace,
	)
	if err != nil {
		return "", fmt.Errorf("failed to find ConfigMap for %s: %v", source.ValueRef, err)
	}

	value, exists := configMap.Data[source.Key]
	if !exists {
		return "", fmt.Errorf("key %s not found in ConfigMap %s/%s", source.Key, configMap.Namespace, configMap.Name)
	}
	return value, nil
}

// getSecretValue fetches a value from a Secret
func getSecretValue(ctx context.Context, kube client.Client, source *v1alpha1.ValueSource, namespace string) (string, error) {
	if source == nil {
		return "", fmt.Errorf("source cannot be nil")
	}

	secret := &corev1.Secret{}
	err := common.GetObject(
		ctx,
		kube,
		secret,
		source.ValueRef,
		namespace,
	)
	if err != nil {
		return "", fmt.Errorf("failed to find Secret for %s: %v", source.ValueRef, err)
	}

	value, exists := secret.Data[source.Key]
	if !exists {
		return "", fmt.Errorf("key %s not found in Secret %s/%s", source.Key, secret.Namespace, secret.Name)
	}
	return string(value), nil
}
