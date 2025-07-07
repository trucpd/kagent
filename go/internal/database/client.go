package database

import (
	"fmt"
	"slices"

	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
)

type Client interface {
	CreateFeedback(feedback *Feedback) error
	CreateSession(session *Session) error
	CreateAgent(agent *Agent) error
	CreateToolServer(toolServer *ToolServer) (*ToolServer, error)

	UpsertAgent(agent *Agent) error

	RefreshToolsForServer(serverName string, tools []*autogen_client.NamedTool) error

	DeleteSession(sessionName string, userID string) error
	DeleteAgent(agentName string) error
	DeleteToolServer(serverName string) error

	UpdateSession(session *Session) error
	UpdateToolServer(server *ToolServer) error
	UpdateAgent(agent *Agent) error
	UpdateTask(task *Task) error

	GetSession(name string, userID string) (*Session, error)
	GetAgent(name string) (*Agent, error)
	GetTool(name string) (*Tool, error)
	GetToolServer(name string) (*ToolServer, error)

	ListTools() ([]Tool, error)
	ListFeedback(userID string) ([]Feedback, error)
	ListSessionTasks(sessionName string, userID string) ([]Task, error)
	ListSessions(userID string) ([]Session, error)
	ListAgents(userID string) ([]Agent, error)
	ListToolServers() ([]ToolServer, error)
	ListToolsForServer(serverName string) ([]Tool, error)
	ListMessagesForTask(taskID string) ([]Message, error)
}

type clientImpl struct {
	serviceWrapper *ServiceWrapper
}

func NewClient(serviceWrapper *ServiceWrapper) Client {
	return &clientImpl{
		serviceWrapper: serviceWrapper,
	}
}

// CreateFeedback creates a new feedback record
func (c *clientImpl) CreateFeedback(feedback *Feedback) error {
	return c.serviceWrapper.Feedback.Create(feedback)
}

// CreateSession creates a new session record
func (c *clientImpl) CreateSession(session *Session) error {
	return c.serviceWrapper.Session.Create(session)
}

// CreateAgent creates a new agent record
func (c *clientImpl) CreateAgent(agent *Agent) error {
	return c.serviceWrapper.Agent.Create(agent)
}

// UpsertAgent upserts an agent record
func (c *clientImpl) UpsertAgent(agent *Agent) error {
	return c.serviceWrapper.Agent.Update(agent)
}

// CreateToolServer creates a new tool server record
func (c *clientImpl) CreateToolServer(toolServer *ToolServer) (*ToolServer, error) {
	err := c.serviceWrapper.ToolServer.Create(toolServer)
	if err != nil {
		return nil, err
	}
	return toolServer, nil
}

// CreateTool creates a new tool record
func (c *clientImpl) CreateTool(tool *Tool) error {
	return c.serviceWrapper.Tool.Create(tool)
}

// DeleteSession deletes a session by name and user ID
func (c *clientImpl) DeleteSession(sessionName string, userID string) error {
	return c.serviceWrapper.Session.Delete(
		Clause{Key: "name", Value: sessionName},
		Clause{Key: "user_id", Value: userID},
	)
}

// DeleteAgent deletes an agent by name and user ID
func (c *clientImpl) DeleteAgent(agentName string) error {
	return c.serviceWrapper.Agent.Delete(Clause{Key: "name", Value: agentName})
}

// DeleteToolServer deletes a tool server by name and user ID
func (c *clientImpl) DeleteToolServer(serverName string) error {
	return c.serviceWrapper.ToolServer.Delete(Clause{Key: "name", Value: serverName})
}

// GetTaskMessages retrieves messages for a specific task
func (c *clientImpl) GetTaskMessages(taskID int) ([]Message, error) {
	messages, err := c.serviceWrapper.Message.List(Clause{Key: "task_id", Value: taskID})
	if err != nil {
		return nil, err
	}

	return messages, nil
}

// GetSession retrieves a session by name and user ID
func (c *clientImpl) GetSession(sessionLabel string, userID string) (*Session, error) {
	return c.serviceWrapper.Session.Get(
		Clause{Key: "name", Value: sessionLabel},
		Clause{Key: "user_id", Value: userID},
	)
}

// GetAgent retrieves an agent by name and user ID
func (c *clientImpl) GetAgent(agentLabel string) (*Agent, error) {
	return c.serviceWrapper.Agent.Get(Clause{Key: "name", Value: agentLabel})
}

// GetTool retrieves a tool by provider (name) and user ID
func (c *clientImpl) GetTool(provider string) (*Tool, error) {
	return c.serviceWrapper.Tool.Get(Clause{Key: "name", Value: provider})
}

// GetToolServer retrieves a tool server by name and user ID
func (c *clientImpl) GetToolServer(serverName string) (*ToolServer, error) {
	return c.serviceWrapper.ToolServer.Get(Clause{Key: "name", Value: serverName})
}

// ListFeedback lists all feedback for a user
func (c *clientImpl) ListFeedback(userID string) ([]Feedback, error) {
	feedback, err := c.serviceWrapper.Feedback.List(Clause{Key: "user_id", Value: userID})
	if err != nil {
		return nil, err
	}

	return feedback, nil
}

// ListRuns lists all runs for a user
func (c *clientImpl) ListTasks(userID string) ([]Task, error) {
	tasks, err := c.serviceWrapper.Task.List(Clause{Key: "user_id", Value: userID})
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

// ListSessionRuns lists all runs for a specific session
func (c *clientImpl) ListSessionTasks(sessionName string, userID string) ([]Task, error) {
	return c.serviceWrapper.Task.List(
		Clause{Key: "session_id", Value: sessionName},
		Clause{Key: "user_id", Value: userID},
	)
}

// ListSessions lists all sessions for a user
func (c *clientImpl) ListSessions(userID string) ([]Session, error) {
	return c.serviceWrapper.Session.List(Clause{Key: "user_id", Value: userID})
}

// ListAgents lists all agents for a user
func (c *clientImpl) ListAgents(userID string) ([]Agent, error) {
	return c.serviceWrapper.Agent.List(Clause{Key: "user_id", Value: userID})
}

// ListToolServers lists all tool servers for a user
func (c *clientImpl) ListToolServers() ([]ToolServer, error) {
	return c.serviceWrapper.ToolServer.List()
}

// ListTools lists all tools for a user
func (c *clientImpl) ListTools() ([]Tool, error) {
	return c.serviceWrapper.Tool.List()
}

// ListToolsForServer lists all tools for a specific server
func (c *clientImpl) ListToolsForServer(serverName string) ([]Tool, error) {
	return c.serviceWrapper.Tool.List(Clause{Key: "server_name", Value: serverName})
}

// RefreshToolsForServer refreshes a tool server
func (c *clientImpl) RefreshToolsForServer(serverName string, tools []*autogen_client.NamedTool) error {
	existingTools, err := c.ListToolsForServer(serverName)
	if err != nil {
		return err
	}

	// Check if the tool exists in the existing tools
	// If it does, update it
	// If it doesn't, create it
	// If it's in the existing tools but not in the new tools, delete it
	for _, tool := range tools {
		existingToolIndex := slices.IndexFunc(existingTools, func(t Tool) bool {
			return t.Name == tool.Name
		})
		if existingToolIndex != -1 {
			existingTool := existingTools[existingToolIndex]
			existingTool.Component = *tool.Component
			existingTool.ServerName = serverName
			err = c.serviceWrapper.Tool.Update(&existingTool)
			if err != nil {
				return err
			}
		} else {
			err = c.serviceWrapper.Tool.Create(&Tool{
				Name:       tool.Name,
				Component:  *tool.Component,
				ServerName: serverName,
			})
			if err != nil {
				return fmt.Errorf("failed to create tool %s: %v", tool.Name, err)
			}
		}
	}

	// Delete any tools that are in the existing tools but not in the new tools
	for _, existingTool := range existingTools {
		if !slices.ContainsFunc(tools, func(t *autogen_client.NamedTool) bool {
			return t.Name == existingTool.Name
		}) {
			err = c.serviceWrapper.Tool.Delete(Clause{Key: "name", Value: existingTool.Name})
			if err != nil {
				return fmt.Errorf("failed to delete tool %s: %v", existingTool.Name, err)
			}
		}
	}
	return nil
}

// UpdateSession updates a session
func (c *clientImpl) UpdateSession(session *Session) error {
	return c.serviceWrapper.Session.Update(session)
}

// UpdateToolServer updates a tool server
func (c *clientImpl) UpdateToolServer(server *ToolServer) error {
	return c.serviceWrapper.ToolServer.Update(server)
}

// UpdateTask updates a task record
func (c *clientImpl) UpdateTask(task *Task) error {
	return c.serviceWrapper.Task.Update(task)
}

// UpdateAgent updates an agent record
func (c *clientImpl) UpdateAgent(agent *Agent) error {
	return c.serviceWrapper.Agent.Update(agent)
}

// ListMessagesForRun retrieves messages for a specific run (helper method)
func (c *clientImpl) ListMessagesForTask(taskID string) ([]Message, error) {
	return c.serviceWrapper.Message.List(Clause{Key: "task_id", Value: taskID})
}
