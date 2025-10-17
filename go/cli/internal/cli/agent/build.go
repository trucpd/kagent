package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/common"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

type BuildCfg struct {
	ProjectDir string
	Image      string
	Push       bool
	Config     *config.Config
}

// BuildCmd builds a Docker image for an agent project
func BuildCmd(cfg *BuildCfg) error {
	// Validate project directory
	if cfg.ProjectDir == "" {
		return fmt.Errorf("project directory is required")
	}

	// Check if project directory exists
	if _, err := os.Stat(cfg.ProjectDir); os.IsNotExist(err) {
		return fmt.Errorf("project directory does not exist: %s", cfg.ProjectDir)
	}

	// Check if Dockerfile exists in project directory
	dockerfilePath := filepath.Join(cfg.ProjectDir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		return fmt.Errorf("dockerfile not found in project directory: %s", dockerfilePath)
	}

	// Check if Docker is available and running
	if err := checkDockerAvailability(); err != nil {
		return fmt.Errorf("docker check failed: %v", err)
	}

	// Load the manifest to check for MCP servers
	manifest := getManifestFromProjectDir(cfg.ProjectDir)
	if manifest != nil && len(manifest.McpServers) > 0 {
		// Regenerate mcp_tools.py to ensure it's up-to-date before building
		if err := regenerateMcpToolsFile(cfg.ProjectDir, manifest, cfg.Config.Verbose); err != nil {
			return fmt.Errorf("failed to regenerate mcp_tools.py: %v", err)
		}
	}

	// Build the Docker image
	if err := buildDockerImage(cfg); err != nil {
		return fmt.Errorf("failed to build Docker image: %v", err)
	}

	// Push the image if requested
	if cfg.Push {
		// Docker availability is already checked above, but we could add another check here if needed
		if err := pushDockerImage(cfg); err != nil {
			return fmt.Errorf("failed to push Docker image: %v", err)
		}
	}

	// Check if MCP servers exist and build the MCP server image
	if manifest != nil && len(manifest.McpServers) > 0 {
		if err := buildMcpServerImage(cfg); err != nil {
			return fmt.Errorf("failed to build MCP server image: %v", err)
		}

		// Push the MCP server image if requested
		if cfg.Push {
			if err := pushMcpServerImage(cfg); err != nil {
				return fmt.Errorf("failed to push MCP server image: %v", err)
			}
		}
	}

	return nil
}

// buildDockerImage builds the Docker image using docker build
func buildDockerImage(cfg *BuildCfg) error {
	// Construct the image name
	imageName := constructImageName(cfg)

	// Build command arguments
	args := []string{"build", "-t", imageName, "."}

	// Execute docker build command
	cmd := exec.Command("docker", args...)
	cmd.Dir = cfg.ProjectDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if cfg.Config.Verbose {
		fmt.Printf("Executing: docker %s\n", strings.Join(args, " "))
		fmt.Printf("Working directory: %s\n", cmd.Dir)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %v", err)
	}

	fmt.Printf("Successfully built Docker image: %s\n", imageName)
	return nil
}

// pushDockerImage pushes the Docker image to the specified registry
func pushDockerImage(cfg *BuildCfg) error {
	// Construct the image name
	imageName := constructImageName(cfg)

	// Execute docker push command
	cmd := exec.Command("docker", "push", imageName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if cfg.Config.Verbose {
		fmt.Printf("Executing: docker push %s\n", imageName)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker push failed: %v", err)
	}

	fmt.Printf("Successfully pushed Docker image: %s\n", imageName)
	return nil
}

// constructImageName constructs the full image name from the provided image or defaults
func constructImageName(cfg *BuildCfg) string {
	// If a full image specification is provided, use it as-is
	if cfg.Image != "" {
		return cfg.Image
	}

	// Otherwise, construct from defaults
	// Get agent name from kagent.yaml file
	agentName := getAgentNameFromManifest(cfg.ProjectDir)

	// If no agent name found in manifest, fall back to directory name
	if agentName == "" {
		agentName = filepath.Base(cfg.ProjectDir)
	}

	// Use default registry and tag
	registry := "localhost:5001"
	tag := "latest"

	// Construct full image name: registry/agent-name:tag
	return fmt.Sprintf("%s/%s:%s", registry, agentName, tag)
}

// getAgentNameFromManifest attempts to load the agent name from kagent.yaml
func getAgentNameFromManifest(projectDir string) string {
	// Use the Manager to load the manifest
	manager := common.NewManifestManager(projectDir)
	manifest, err := manager.Load()
	if err != nil {
		// Silently fail and return empty string to fall back to directory name
		return ""
	}

	return manifest.Name
}

// checkDockerAvailability checks if Docker is installed and running
func checkDockerAvailability() error {
	// Check if docker command exists
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker command not found in PATH. Please install Docker")
	}

	// Check if Docker daemon is running by running docker version
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("docker daemon is not running or not accessible. Please start Docker Desktop or Docker daemon")
	}

	// Check if we got a valid version string
	version := strings.TrimSpace(string(output))
	if version == "" {
		return fmt.Errorf("docker daemon returned empty version. Docker may not be properly installed")
	}

	return nil
}

// getManifestFromProjectDir loads the agent manifest from the project directory
func getManifestFromProjectDir(projectDir string) *common.AgentManifest {
	manager := common.NewManifestManager(projectDir)
	manifest, err := manager.Load()
	if err != nil {
		// Silently fail and return nil
		return nil
	}
	return manifest
}

// buildMcpServerImage builds the MCP server Docker image
func buildMcpServerImage(cfg *BuildCfg) error {
	mcpServerDir := filepath.Join(cfg.ProjectDir, "mcp_server")
	if _, err := os.Stat(mcpServerDir); os.IsNotExist(err) {
		// No mcp_server directory, skip building
		return nil
	}

	// Check if Dockerfile exists in mcp_server directory
	dockerfilePath := filepath.Join(mcpServerDir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		return fmt.Errorf("Dockerfile not found in mcp_server directory: %s", dockerfilePath)
	}

	// Construct the MCP server image name
	imageName := constructMcpServerImageName(cfg)

	// Build command arguments
	args := []string{"build", "-t", imageName, "."}

	// Execute docker build command
	cmd := exec.Command("docker", args...)
	cmd.Dir = mcpServerDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if cfg.Config.Verbose {
		fmt.Printf("Executing: docker %s\n", strings.Join(args, " "))
		fmt.Printf("Working directory: %s\n", cmd.Dir)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %v", err)
	}

	fmt.Printf("Successfully built MCP server Docker image: %s\n", imageName)
	return nil
}

// pushMcpServerImage pushes the MCP server Docker image to the specified registry
func pushMcpServerImage(cfg *BuildCfg) error {
	imageName := constructMcpServerImageName(cfg)

	// Execute docker push command
	cmd := exec.Command("docker", "push", imageName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if cfg.Config.Verbose {
		fmt.Printf("Executing: docker push %s\n", imageName)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker push failed: %v", err)
	}

	fmt.Printf("Successfully pushed MCP server Docker image: %s\n", imageName)
	return nil
}

// constructMcpServerImageName constructs the MCP server image name
func constructMcpServerImageName(cfg *BuildCfg) string {
	// Get agent name from kagent.yaml file
	agentName := getAgentNameFromManifest(cfg.ProjectDir)

	// If no agent name found in manifest, fall back to directory name
	if agentName == "" {
		agentName = filepath.Base(cfg.ProjectDir)
	}

	// Use default registry and tag
	registry := "localhost:5001"
	tag := "latest"

	// Construct full image name: registry/agent-name-mcp-server:tag
	return fmt.Sprintf("%s/%s-mcp-server:%s", registry, agentName, tag)
}
