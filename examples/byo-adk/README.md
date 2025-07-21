# BYO-ADK Agent Example

This is an example agent using Google's ADK (Agent Development Kit) that demonstrates how to build a simple agent with tool capabilities.

## Features

- Roll dice with configurable number of sides
- Check if numbers are prime
- Maintains state of previous rolls

## Building the Docker Image

Since this example depends on the `kagent` package located in the `python/` directory, you need to build the Docker image from the project root directory to include the necessary context:

```bash
# From the project root directory (/var/home/yuval/Projects/solo/kagent)
docker build -f examples/byo-adk/Dockerfile -t byo-adk-agent .
```

## Running the Docker Container

```bash
docker run -p 8080:8080 byo-adk-agent
```

The agent will start and listen on port 8080 for A2A (Agent-to-Agent) communication.

## Kubernetes Deployment

### Prerequisites
- Kubernetes cluster (minikube, kind, or cloud provider)
- kubectl configured to access your cluster
- Docker image built and available to your cluster

### Deploy to Kubernetes

1. **Build and load the image** (for local clusters like kind/minikube):
```bash
# Build the image
docker build -f examples/byo-adk/Dockerfile -t kagent.dev/byo-adk-agent:latest .

# For kind clusters
kind load docker-image kagent.dev/byo-adk-agent:latest
```

2. **Deploy the manifests**:
```bash
kubectl apply -f examples/byo-adk/k8s.yaml
```

3. **Check the deployment**:
```bash
# Check pods
kubectl get pods -l app=byo-adk-agent

# Check services
kubectl get svc -l app=byo-adk-agent

# View logs
kubectl logs -l app=byo-adk-agent
```

4. **Access the agent**:
```bash
# Using ClusterIP service (from within cluster)
kubectl port-forward svc/byo-adk-agent-service 8080:8080

# Using NodePort service (external access)
# The agent will be available on any node IP at port 30080
```

### Kubernetes Resources Created

The `k8s.yaml` manifest creates:
- **Deployment**: Runs the byo-adk-agent container with proper resource limits and health checks
- **ClusterIP Service**: For internal cluster communication on port 8080

### Cleanup

To remove the deployment:
```bash
kubectl delete -f examples/byo-adk/k8s.yaml
```

## Local Development

For local development, you can use uv to run the agent:

```bash
# From the byo-adk directory
uv sync
uv run python main.py
```

## Invoke

```bash
kubectl port-forward -n default byo-adk-agent-884486ff4-4b7hr 8080
cd go; go run cli/cmd/kagent/main.go invoke -a k8s-agent -t "How many pods in my cluster?" -n kagent --url http://localhost:8080/a2a/hello_world_agent -s "test"
```****