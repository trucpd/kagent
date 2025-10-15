package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/common"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeployCfg struct {
	ProjectDir   string
	Image        string
	APIKey       string
	APIKeySecret string
	Config       *config.Config
}

// DeployCmd deploys an agent to Kubernetes
func DeployCmd(ctx context.Context, cfg *DeployCfg) error {
	// Validate project directory
	if cfg.ProjectDir == "" {
		return fmt.Errorf("project directory is required")
	}

	// Check if project directory exists
	if _, err := os.Stat(cfg.ProjectDir); os.IsNotExist(err) {
		return fmt.Errorf("project directory does not exist: %s", cfg.ProjectDir)
	}

	// Load the kagent.yaml manifest
	manifest, err := LoadManifest(cfg.ProjectDir)
	if err != nil {
		return fmt.Errorf("failed to load kagent.yaml: %v", err)
	}

	// Build the Docker image before deploying
	fmt.Println("Building Docker image...")
	buildCfg := &BuildCfg{
		ProjectDir: cfg.ProjectDir,
		Image:      cfg.Image,
		Push:       true, // Always push when deploying
		Config:     cfg.Config,
	}
	if err := BuildCmd(buildCfg); err != nil {
		return fmt.Errorf("failed to build Docker image: %v", err)
	}

	// Additional validation for deploy-specific requirements
	if manifest.ModelProvider == "" {
		return fmt.Errorf("model provider is required in kagent.yaml")
	}

	// Determine the API key environment variable name based on model provider
	apiKeyEnvVar := getAPIKeyEnvVar(manifest.ModelProvider)
	if apiKeyEnvVar == "" {
		return fmt.Errorf("unsupported model provider: %s", manifest.ModelProvider)
	}

	// Create Kubernetes client
	k8sClient, err := createKubernetesClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	// If namespace is not set, use default
	if cfg.Config.Namespace == "" {
		cfg.Config.Namespace = "default"
	}

	// Handle secret creation or reference to existing secret
	var secretName string
	if cfg.APIKeySecret != "" {
		// Use existing secret
		secretName = cfg.APIKeySecret
		// Verify the secret exists
		if err := verifySecretExists(ctx, k8sClient, cfg.Config.Namespace, secretName, apiKeyEnvVar); err != nil {
			return err
		}
		if IsVerbose(cfg.Config) {
			fmt.Printf("Using existing secret '%s' in namespace '%s'\n", secretName, cfg.Config.Namespace)
		}
	} else if cfg.APIKey != "" {
		// Create new secret with provided API key
		secretName = fmt.Sprintf("%s-%s", manifest.Name, strings.ToLower(manifest.ModelProvider))
		if err := createSecret(ctx, k8sClient, cfg.Config.Namespace, secretName, apiKeyEnvVar, cfg.APIKey, IsVerbose(cfg.Config)); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("either --api-key or --api-key-secret must be provided")
	}

	// Create the Agent CRD
	if err := createAgentCRD(ctx, k8sClient, cfg, manifest, secretName, apiKeyEnvVar, IsVerbose(cfg.Config)); err != nil {
		return err
	}

	// Deploy MCP servers if any are defined
	if len(manifest.McpServers) > 0 {
		if IsVerbose(cfg.Config) {
			fmt.Printf("Deploying %d MCP server(s)...\n", len(manifest.McpServers))
		}
		if err := deployMCPServers(ctx, k8sClient, cfg, manifest); err != nil {
			return fmt.Errorf("failed to deploy MCP servers: %v", err)
		}
	}

	fmt.Printf("Successfully deployed agent '%s' to namespace '%s'\n", manifest.Name, cfg.Config.Namespace)
	return nil
}

// getAPIKeyEnvVar returns the environment variable name for the given model provider
func getAPIKeyEnvVar(modelProvider string) string {
	switch modelProvider {
	case strings.ToLower(string(v1alpha2.ModelProviderAnthropic)):
		return "ANTHROPIC_API_KEY"
	case strings.ToLower(string(v1alpha2.ModelProviderOpenAI)):
		return "OPENAI_API_KEY"
	case strings.ToLower(string(v1alpha2.ModelProviderGemini)):
		return "GOOGLE_API_KEY"
	default:
		return ""
	}
}

// createKubernetesClient creates a Kubernetes client
func createKubernetesClient() (client.Client, error) {
	// Use the standard kubeconfig loading rules
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes config: %v", err)
	}

	schemes := runtime.NewScheme()
	if err := scheme.AddToScheme(schemes); err != nil {
		return nil, fmt.Errorf("failed to add core scheme: %v", err)
	}
	if err := v1alpha1.AddToScheme(schemes); err != nil {
		return nil, fmt.Errorf("failed to add kagent v1alpha1 scheme: %v", err)
	}
	if err := v1alpha2.AddToScheme(schemes); err != nil {
		return nil, fmt.Errorf("failed to add kagent v1alpha2 scheme: %v", err)
	}

	k8sClient, err := client.New(config, client.Options{Scheme: schemes})
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	return k8sClient, nil
}

// verifySecretExists verifies that a secret exists and contains the required key
func verifySecretExists(ctx context.Context, k8sClient client.Client, namespace, secretName, apiKeyEnvVar string) error {
	secret := &corev1.Secret{}
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, secret)
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("secret '%s' not found in namespace '%s'", secretName, namespace)
		}
		return fmt.Errorf("failed to check if secret exists: %v", err)
	}

	// Verify the secret contains the required key
	if _, exists := secret.Data[apiKeyEnvVar]; !exists {
		return fmt.Errorf("secret '%s' does not contain key '%s'", secretName, apiKeyEnvVar)
	}

	return nil
}

// createSecret creates a Kubernetes secret with the API key
func createSecret(ctx context.Context, k8sClient client.Client, namespace, secretName, apiKeyEnvVar, apiKeyValue string, verbose bool) error {
	// Check if secret already exists
	existingSecret := &corev1.Secret{}
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, existingSecret)

	if err != nil {
		if errors.IsNotFound(err) {
			// Create new secret
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
				Data: map[string][]byte{
					apiKeyEnvVar: []byte(apiKeyValue),
				},
			}
			if err := k8sClient.Create(ctx, secret); err != nil {
				return fmt.Errorf("failed to create secret: %v", err)
			}
			if verbose {
				fmt.Printf("Created secret '%s' in namespace '%s'\n", secretName, namespace)
			}
			return nil
		}
		return fmt.Errorf("failed to check if secret exists: %v", err)
	}

	// Secret exists, update it
	existingSecret.Data[apiKeyEnvVar] = []byte(apiKeyValue)
	if err := k8sClient.Update(ctx, existingSecret); err != nil {
		return fmt.Errorf("failed to update existing secret: %v", err)
	}
	if verbose {
		fmt.Printf("Updated existing secret '%s' in namespace '%s'\n", secretName, namespace)
	}
	return nil
}

// createAgentCRD creates the Agent CRD
func createAgentCRD(ctx context.Context, k8sClient client.Client, cfg *DeployCfg, manifest *common.AgentManifest, secretName, apiKeyEnvVar string, verbose bool) error {
	// Determine image name
	imageName := cfg.Image
	if imageName == "" {
		// Use default registry and tag
		registry := "localhost:5001"
		tag := "latest"
		imageName = fmt.Sprintf("%s/%s:%s", registry, manifest.Name, tag)
	}

	// Create the Agent CRD
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      manifest.Name,
			Namespace: cfg.Config.Namespace,
		},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_BYO,
			Description: manifest.Description,
			BYO: &v1alpha2.BYOAgentSpec{
				Deployment: &v1alpha2.ByoDeploymentSpec{
					Image: imageName,
					SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
						Env: []corev1.EnvVar{
							{
								Name: apiKeyEnvVar,
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: secretName,
										},
										Key: apiKeyEnvVar,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Check if agent already exists
	existingAgent := &v1alpha2.Agent{}
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cfg.Config.Namespace, Name: manifest.Name}, existingAgent)

	if err != nil {
		if errors.IsNotFound(err) {
			// Agent does not exist, create it
			if err := k8sClient.Create(ctx, agent); err != nil {
				return fmt.Errorf("failed to create agent: %v", err)
			}
			if verbose {
				fmt.Printf("Created agent '%s' in namespace '%s'\n", manifest.Name, cfg.Config.Namespace)
			}
			return nil
		}
		return fmt.Errorf("failed to check if agent exists: %v", err)
	}

	// Agent exists, update it
	existingAgent.Spec = agent.Spec
	if err := k8sClient.Update(ctx, existingAgent); err != nil {
		return fmt.Errorf("failed to update existing agent: %v", err)
	}
	if verbose {
		fmt.Printf("Updated existing agent '%s' in namespace '%s'\n", manifest.Name, cfg.Config.Namespace)
	}
	return nil
}

// deployMCPServers deploys all MCP servers defined in the manifest
func deployMCPServers(ctx context.Context, k8sClient client.Client, cfg *DeployCfg, manifest *common.AgentManifest) error {
	verbose := IsVerbose(cfg.Config)

	for i, mcpServer := range manifest.McpServers {
		if verbose {
			fmt.Printf("Deploying MCP server '%s' (type: %s)...\n", mcpServer.Name, mcpServer.Type)
		}

		switch mcpServer.Type {
		case "remote":
			// Deploy RemoteMCPServer (v1alpha2)
			if err := deployRemoteMCPServer(ctx, k8sClient, cfg.Config.Namespace, &mcpServer, verbose); err != nil {
				return fmt.Errorf("failed to deploy remote MCP server '%s': %v", mcpServer.Name, err)
			}
		case "command":
			// Deploy MCPServer (v1alpha1)
			if err := deployCommandMCPServer(ctx, k8sClient, cfg.Config.Namespace, &mcpServer, verbose); err != nil {
				return fmt.Errorf("failed to deploy command MCP server '%s': %v", mcpServer.Name, err)
			}
		default:
			return fmt.Errorf("mcpServers[%d]: unsupported type '%s'", i, mcpServer.Type)
		}
	}

	return nil
}

// deployRemoteMCPServer creates a RemoteMCPServer resource
func deployRemoteMCPServer(ctx context.Context, k8sClient client.Client, namespace string, mcpServer *common.McpServerType, verbose bool) error {
	// Create secrets for any header values that might be environment variables
	headerRefs, err := createSecretsForHeaders(ctx, k8sClient, namespace, mcpServer, verbose)
	if err != nil {
		return fmt.Errorf("failed to create secrets for headers: %v", err)
	}

	// Default timeout
	timeout := metav1.Duration{Duration: 5 * time.Second}
	sseReadTimeout := metav1.Duration{Duration: 5 * time.Minute}
	terminateOnClose := true

	// Create the RemoteMCPServer CRD
	remoteMCPServer := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: namespace,
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description:      fmt.Sprintf("Remote MCP server: %s", mcpServer.Name),
			Protocol:         v1alpha2.RemoteMCPServerProtocolStreamableHttp,
			URL:              mcpServer.URL,
			HeadersFrom:      headerRefs,
			Timeout:          &timeout,
			SseReadTimeout:   &sseReadTimeout,
			TerminateOnClose: &terminateOnClose,
		},
	}

	// Check if RemoteMCPServer already exists
	existingRemoteMCPServer := &v1alpha2.RemoteMCPServer{}
	err = k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: mcpServer.Name}, existingRemoteMCPServer)

	if err != nil {
		if errors.IsNotFound(err) {
			// Create new RemoteMCPServer
			if err := k8sClient.Create(ctx, remoteMCPServer); err != nil {
				return fmt.Errorf("failed to create RemoteMCPServer: %v", err)
			}
			if verbose {
				fmt.Printf("Created RemoteMCPServer '%s' in namespace '%s'\n", mcpServer.Name, namespace)
			}
			return nil
		}
		return fmt.Errorf("failed to check if RemoteMCPServer exists: %v", err)
	}

	// RemoteMCPServer exists, update it
	existingRemoteMCPServer.Spec = remoteMCPServer.Spec
	if err := k8sClient.Update(ctx, existingRemoteMCPServer); err != nil {
		return fmt.Errorf("failed to update existing RemoteMCPServer: %v", err)
	}
	if verbose {
		fmt.Printf("Updated existing RemoteMCPServer '%s' in namespace '%s'\n", mcpServer.Name, namespace)
	}
	return nil
}

// deployCommandMCPServer creates an MCPServer resource for command/stdio type
func deployCommandMCPServer(ctx context.Context, k8sClient client.Client, namespace string, mcpServer *common.McpServerType, verbose bool) error {
	// Create secrets for any environment variables
	envMap, secretRefs, err := createSecretsForEnv(ctx, k8sClient, namespace, mcpServer, verbose)
	if err != nil {
		return fmt.Errorf("failed to create secrets for env vars: %v", err)
	}

	// Determine the image
	image := mcpServer.Image
	if image == "" {
		// Default to node image for npx/uvx commands
		if strings.HasPrefix(mcpServer.Command, "npx") {
			image = "node:24-alpine3.21"
		} else if strings.HasPrefix(mcpServer.Command, "uvx") {
			image = "ghcr.io/astral-sh/uv:python3.12-alpine"
		} else {
			image = "node:24-alpine3.21" // default
		}
	}

	// Create the MCPServer CRD
	mcpServerResource := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: namespace,
		},
		Spec: v1alpha1.MCPServerSpec{
			TransportType:  v1alpha1.TransportTypeStdio,
			StdioTransport: &v1alpha1.StdioTransport{},
			Deployment: v1alpha1.MCPServerDeployment{
				Image:      image,
				Port:       3000,
				Cmd:        mcpServer.Command,
				Args:       mcpServer.Args,
				Env:        envMap,
				SecretRefs: secretRefs,
			},
		},
	}

	// Check if MCPServer already exists
	existingMCPServer := &v1alpha1.MCPServer{}
	err = k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: mcpServer.Name}, existingMCPServer)

	if err != nil {
		if errors.IsNotFound(err) {
			// Create new MCPServer
			if err := k8sClient.Create(ctx, mcpServerResource); err != nil {
				return fmt.Errorf("failed to create MCPServer: %v", err)
			}
			if verbose {
				fmt.Printf("Created MCPServer '%s' in namespace '%s'\n", mcpServer.Name, namespace)
			}
			return nil
		}
		return fmt.Errorf("failed to check if MCPServer exists: %v", err)
	}

	// MCPServer exists, update it
	existingMCPServer.Spec = mcpServerResource.Spec
	if err := k8sClient.Update(ctx, existingMCPServer); err != nil {
		return fmt.Errorf("failed to update existing MCPServer: %v", err)
	}
	if verbose {
		fmt.Printf("Updated existing MCPServer '%s' in namespace '%s'\n", mcpServer.Name, namespace)
	}
	return nil
}

// createSecretsForHeaders creates secrets for header values that reference environment variables
func createSecretsForHeaders(ctx context.Context, k8sClient client.Client, namespace string, mcpServer *common.McpServerType, verbose bool) ([]v1alpha2.ValueRef, error) {
	var headerRefs []v1alpha2.ValueRef

	// Regular expression to match environment variable references like ${VAR_NAME} or $VAR_NAME
	envVarRegex := regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

	for headerName, headerValue := range mcpServer.Headers {
		// Check if the header value is an environment variable reference
		matches := envVarRegex.FindStringSubmatch(headerValue)
		if len(matches) > 0 {
			// Extract the environment variable name
			envVarName := matches[1]
			if envVarName == "" {
				envVarName = matches[2]
			}

			// Get the actual value from environment
			envValue := os.Getenv(envVarName)
			if envValue == "" {
				return nil, fmt.Errorf("environment variable '%s' referenced in header '%s' is not set", envVarName, headerName)
			}

			// Create a secret for this value
			secretName := fmt.Sprintf("%s-%s", mcpServer.Name, strings.ToLower(strings.ReplaceAll(headerName, "-", "")))
			secretKey := strings.ToLower(strings.ReplaceAll(headerName, "-", "_"))

			if err := createSecret(ctx, k8sClient, namespace, secretName, secretKey, envValue, verbose); err != nil {
				return nil, fmt.Errorf("failed to create secret for header '%s': %v", headerName, err)
			}

			// Add the header reference
			headerRefs = append(headerRefs, v1alpha2.ValueRef{
				Name: headerName,
				ValueFrom: &v1alpha2.ValueSource{
					Type: v1alpha2.SecretValueSource,
					Name: secretName,
					Key:  secretKey,
				},
			})
		} else {
			// Static value, add directly
			headerRefs = append(headerRefs, v1alpha2.ValueRef{
				Name:  headerName,
				Value: headerValue,
			})
		}
	}

	return headerRefs, nil
}

// createSecretsForEnv creates secrets for environment variables and returns env map and secret refs
func createSecretsForEnv(ctx context.Context, k8sClient client.Client, namespace string, mcpServer *common.McpServerType, verbose bool) (map[string]string, []corev1.LocalObjectReference, error) {
	envMap := make(map[string]string)
	var secretRefs []corev1.LocalObjectReference

	// Regular expression to match environment variable references like ${VAR_NAME} or $VAR_NAME
	envVarRegex := regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

	// Track secrets we've created to avoid duplicates
	createdSecrets := make(map[string]bool)

	for _, envVar := range mcpServer.Env {
		// Parse env var in format KEY=VALUE
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) != 2 {
			return nil, nil, fmt.Errorf("invalid env var format '%s', expected KEY=VALUE", envVar)
		}

		envKey := parts[0]
		envValue := parts[1]

		// Check if the value is an environment variable reference
		matches := envVarRegex.FindStringSubmatch(envValue)
		if len(matches) > 0 {
			// Extract the environment variable name
			refEnvVarName := matches[1]
			if refEnvVarName == "" {
				refEnvVarName = matches[2]
			}

			// Get the actual value from environment
			actualValue := os.Getenv(refEnvVarName)
			if actualValue == "" {
				return nil, nil, fmt.Errorf("environment variable '%s' referenced in env var '%s' is not set", refEnvVarName, envKey)
			}

			// Create a secret for sensitive values
			secretName := fmt.Sprintf("%s-env", mcpServer.Name)

			// Only create the secret once
			if !createdSecrets[secretName] {
				if err := createSecret(ctx, k8sClient, namespace, secretName, strings.ToLower(envKey), actualValue, verbose); err != nil {
					return nil, nil, fmt.Errorf("failed to create secret for env var '%s': %v", envKey, err)
				}
				secretRefs = append(secretRefs, corev1.LocalObjectReference{
					Name: secretName,
				})
				createdSecrets[secretName] = true
			} else {
				// Update the existing secret with the new key
				existingSecret := &corev1.Secret{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, existingSecret)
				if err != nil {
					return nil, nil, fmt.Errorf("failed to get existing secret: %v", err)
				}
				existingSecret.Data[strings.ToLower(envKey)] = []byte(actualValue)
				if err := k8sClient.Update(ctx, existingSecret); err != nil {
					return nil, nil, fmt.Errorf("failed to update existing secret: %v", err)
				}
			}
		} else {
			// Static value, add to env map
			envMap[envKey] = envValue
		}
	}

	return envMap, secretRefs, nil
}
