Setup kagent in kind:

```shell
make create-kind-cluster helm-install
echo KAGENT_URL "http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083
```

Update `.vscode/launch.json` to use the above KAGENT_URL for debugging static agents locally:

```json
        {
            "name": "Python Debugger: static agent",
            "type": "debugpy",
            "request": "launch",
            "module": "kagent.adk.cli",
            "python": "${workspaceFolder}/python/.venv/bin/python",
            "env": {
                "KAGENT_URL": "http://172.18.255.0:8083",
                "KAGENT_NAME": "kagent",
                "KAGENT_NAMESPACE": "kagent",
                "OPENAI_API_KEY": "testkey"
            },
            "args": [
                "static",
                "--filepath",
                "${workspaceFolder}"
            ],
            "console": "integratedTerminal",
            "justMyCode": false
        }
```

Start the mock mcp llm and mcp servers:
```shell
cd go; go run main.go --mock-file test/e2e/mocks/invoke_inline_agent.json
```

Make sure that the workspace folder contains the following two files (if you change the folder, update `--filepath` accordingly):

config.json:
```json
{
  "model": {
    "base_url": "http://127.0.0.1:8091/v1",
    "headers": null,
    "model": "gpt-4.1-mini",
    "type": "openai"
  },
  "description": "",
  "instruction": "You are a test agent. The system prompt doesn't matter because we're using a mock server.",
  "http_tools": [
    {
      "params": {
        "url": "http://127.0.0.1:8092/mcp",
        "headers": {},
        "timeout": 30,
        "sse_read_timeout": 300,
        "terminate_on_close": true
      },
      "tools": [
        "make_kebab"
      ]
    }
  ],
  "sse_tools": null,
  "remote_agents": null
}
```

agent-card.json:
```json
{
  "name": "test_agent",
  "description": "",
  "url": "http://test-agent.kagent:8080",
  "version": "",
  "capabilities": {
    "streaming": true,
    "pushNotifications": false,
    "stateTransitionHistory": true
  },
  "defaultInputModes": [
    "text"
  ],
  "defaultOutputModes": [
    "text"
  ],
  "skills": []
}
```

Finally, start the agent debug session. You can now call the agent directly!

```shell
cd go; go run cli/cmd/kagent/main.go invoke -a k8s-agent -t "Make a kebab" -n kagent http://127.0.0.1:8080 --header "x-user-id: foo@foo.com"
```