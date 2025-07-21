package database

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"gorm.io/gorm"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

type Client interface {
	// Store methods
	StoreFeedback(feedback *Feedback) error
	StoreSession(session *Session) error
	StoreAgent(agent *Agent) error
	StoreTask(task *protocol.Task) error
	StorePushNotification(config *protocol.TaskPushNotificationConfig) error
	StoreToolServer(toolServer *ToolServer) (*ToolServer, error)
	StoreMessages(messages ...*protocol.Message) error

	// Delete methods
	DeleteSession(sessionName string, userID string) error
	DeleteAgent(agentName string) error
	DeleteToolServer(serverName string) error
	DeleteTask(taskID string) error
	DeletePushNotification(taskID string) error

	// Get methods
	GetSession(name string, userID string) (*Session, error)
	GetAgent(name string) (*Agent, error)
	GetTask(id string) (*protocol.Task, error)
	GetTool(name string) (*Tool, error)
	GetToolServer(name string) (*ToolServer, error)
	GetMessage(messageID string) (*protocol.Message, error)
	GetPushNotification(taskID string, configID string) (*protocol.TaskPushNotificationConfig, error)

	// List methods
	ListTools() ([]Tool, error)
	ListFeedback(userID string) ([]Feedback, error)
	ListSessionTasks(sessionID string, userID string) ([]*protocol.Task, error)
	ListSessions(userID string) ([]Session, error)
	ListTasks(userID string) ([]*protocol.Task, error)
	ListSessionsForAgent(agentID uint, userID string) ([]Session, error)
	ListAgents() ([]Agent, error)
	ListToolServers() ([]ToolServer, error)
	ListToolsForServer(serverName string) ([]Tool, error)
	ListMessagesForTask(taskID, userID string) ([]*protocol.Message, error)
	ListMessagesForSession(sessionID, userID string) ([]*protocol.Message, error)
	ListPushNotifications(taskID string) ([]*protocol.TaskPushNotificationConfig, error)

	// Helper methods
	RefreshToolsForServer(serverName string, tools ...*v1alpha1.MCPTool) error
}

type clientImpl struct {
	db *gorm.DB
}

func NewClient(dbManager *Manager) Client {
	return &clientImpl{
		db: dbManager.db,
	}
}

// CreateFeedback creates a new feedback record
func (c *clientImpl) StoreFeedback(feedback *Feedback) error {
	return save(c.db, feedback)
}

// CreateSession creates a new session record
func (c *clientImpl) StoreSession(session *Session) error {
	return save(c.db, session)
}

// CreateAgent creates a new agent record
func (c *clientImpl) StoreAgent(agent *Agent) error {
	return save(c.db, agent)
}

// CreateTask creates a new task record
func (c *clientImpl) CreateTask(task *protocol.Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to serialize task: %w", err)
	}

	return save(c.db, &Task{
		ID:        task.ID,
		SessionID: task.ContextID,
		UserID:    utils.GetGlobalUserID(),
		Data:      string(data),
	})
}

func (c *clientImpl) CreatePushNotification(taskID string, config *protocol.TaskPushNotificationConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize push notification config: %w", err)
	}

	return save(c.db, &PushNotification{
		TaskID: taskID,
		Data:   string(data),
	})
}

// UpsertAgent upserts an agent record
func (c *clientImpl) UpsertAgent(agent *Agent) error {
	return save(c.db, agent)
}

// CreateToolServer creates a new tool server record
func (c *clientImpl) StoreToolServer(toolServer *ToolServer) (*ToolServer, error) {
	err := save(c.db, toolServer)
	if err != nil {
		return nil, err
	}
	return toolServer, nil
}

// CreateTool creates a new tool record
func (c *clientImpl) StoreTool(tool *Tool) error {
	return save(c.db, tool)
}

// DeleteTask deletes a task by ID
func (c *clientImpl) DeleteTask(taskID string) error {
	return delete[Task](c.db, Clause{Key: "id", Value: taskID})
}

// DeleteSession deletes a session by name and user ID
func (c *clientImpl) DeleteSession(sessionName string, userID string) error {
	return delete[Session](c.db,
		Clause{Key: "name", Value: sessionName},
		Clause{Key: "user_id", Value: userID})
}

// DeleteAgent deletes an agent by name and user ID
func (c *clientImpl) DeleteAgent(agentName string) error {
	return delete[Agent](c.db, Clause{Key: "name", Value: agentName})
}

// DeleteToolServer deletes a tool server by name and user ID
func (c *clientImpl) DeleteToolServer(serverName string) error {
	return delete[ToolServer](c.db, Clause{Key: "name", Value: serverName})
}

// GetTaskMessages retrieves messages for a specific task
func (c *clientImpl) GetTaskMessages(taskID int) ([]*protocol.Message, error) {
	messages, err := list[Message](c.db, Clause{Key: "task_id", Value: taskID})
	if err != nil {
		return nil, err
	}

	protocolMessages := make([]*protocol.Message, 0, len(messages))
	for _, message := range messages {
		var protocolMessage protocol.Message
		if err := json.Unmarshal([]byte(message.Data), &protocolMessage); err != nil {
			return nil, fmt.Errorf("failed to deserialize message: %w", err)
		}
		protocolMessages = append(protocolMessages, &protocolMessage)
	}

	return protocolMessages, nil
}

// GetSession retrieves a session by name and user ID
func (c *clientImpl) GetSession(sessionName string, userID string) (*Session, error) {
	return get[Session](c.db,
		Clause{Key: "id", Value: sessionName},
		Clause{Key: "user_id", Value: userID})
}

// GetAgent retrieves an agent by name and user ID
func (c *clientImpl) GetAgent(agentName string) (*Agent, error) {
	return get[Agent](c.db, Clause{Key: "name", Value: agentName})
}

// GetTool retrieves a tool by provider (name) and user ID
func (c *clientImpl) GetTool(provider string) (*Tool, error) {
	return get[Tool](c.db, Clause{Key: "name", Value: provider})
}

// GetToolServer retrieves a tool server by name and user ID
func (c *clientImpl) GetToolServer(serverName string) (*ToolServer, error) {
	return get[ToolServer](c.db, Clause{Key: "name", Value: serverName})
}

// ListFeedback lists all feedback for a user
func (c *clientImpl) ListFeedback(userID string) ([]Feedback, error) {
	feedback, err := list[Feedback](c.db, Clause{Key: "user_id", Value: userID})
	if err != nil {
		return nil, err
	}

	return feedback, nil
}

func (c *clientImpl) StoreMessages(messages ...*protocol.Message) error {
	for _, message := range messages {
		if message == nil {
			continue
		}
		data, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("failed to serialize message: %w", err)
		}

		dbMessage := Message{
			ID:        message.MessageID,
			Data:      string(data),
			SessionID: message.ContextID,
			TaskID:    message.TaskID,
			UserID:    utils.GetGlobalUserID(),
		}
		err = save(c.db, &dbMessage)
		if err != nil {
			return fmt.Errorf("failed to create message: %w", err)
		}
	}
	return nil
}

// ListRuns lists all runs for a user
func (c *clientImpl) ListTasks(userID string) ([]*protocol.Task, error) {
	tasks, err := list[Task](c.db, Clause{Key: "user_id", Value: userID})
	if err != nil {
		return nil, err
	}
	return ParseTasks(tasks)
}

// ListSessionRuns lists all runs for a specific session
func (c *clientImpl) ListSessionTasks(sessionID string, userID string) ([]*protocol.Task, error) {
	tasks, err := list[Task](c.db,
		Clause{Key: "session_id", Value: sessionID},
		Clause{Key: "user_id", Value: userID})
	if err != nil {
		return nil, err
	}

	return ParseTasks(tasks)
}

func (c *clientImpl) ListSessionsForAgent(agentID uint, userID string) ([]Session, error) {
	return list[Session](c.db,
		Clause{Key: "agent_id", Value: agentID},
		Clause{Key: "user_id", Value: userID})
}

// ListSessions lists all sessions for a user
func (c *clientImpl) ListSessions(userID string) ([]Session, error) {
	return list[Session](c.db, Clause{Key: "user_id", Value: userID})
}

// ListAgents lists all agents
func (c *clientImpl) ListAgents() ([]Agent, error) {
	return list[Agent](c.db)
}

// ListToolServers lists all tool servers for a user
func (c *clientImpl) ListToolServers() ([]ToolServer, error) {
	return list[ToolServer](c.db)
}

// ListTools lists all tools for a user
func (c *clientImpl) ListTools() ([]Tool, error) {
	return list[Tool](c.db)
}

// ListToolsForServer lists all tools for a specific server
func (c *clientImpl) ListToolsForServer(serverName string) ([]Tool, error) {
	return list[Tool](c.db, Clause{Key: "server_name", Value: serverName})
}

// RefreshToolsForServer refreshes a tool server
// TODO: Use a transaction to ensure atomicity
func (c *clientImpl) RefreshToolsForServer(serverName string, tools ...*v1alpha1.MCPTool) error {
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
			existingTool.ServerName = serverName
			existingTool.Description = tool.Description
			err = save(c.db, &existingTool)
			if err != nil {
				return err
			}
		} else {
			err = save(c.db, &Tool{
				Name:        tool.Name,
				ServerName:  serverName,
				Description: tool.Description,
			})
			if err != nil {
				return fmt.Errorf("failed to create tool %s: %v", tool.Name, err)
			}
		}
	}

	// Delete any tools that are in the existing tools but not in the new tools
	for _, existingTool := range existingTools {
		if !slices.ContainsFunc(tools, func(t *v1alpha1.MCPTool) bool {
			return t.Name == existingTool.Name
		}) {
			err = delete[Tool](c.db, Clause{Key: "name", Value: existingTool.Name})
			if err != nil {
				return fmt.Errorf("failed to delete tool %s: %v", existingTool.Name, err)
			}
		}
	}
	return nil
}

// ListMessagesForRun retrieves messages for a specific run (helper method)
func (c *clientImpl) ListMessagesForTask(taskID, userID string) ([]*protocol.Message, error) {
	messages, err := list[Message](c.db,
		Clause{Key: "task_id", Value: taskID},
		Clause{Key: "user_id", Value: userID})
	if err != nil {
		return nil, err
	}

	return ParseMessages(messages)
}

func (c *clientImpl) ListMessagesForSession(sessionID, userID string) ([]*protocol.Message, error) {
	messages, err := list[Message](c.db,
		Clause{Key: "session_id", Value: sessionID},
		Clause{Key: "user_id", Value: userID})
	if err != nil {
		return nil, err
	}

	return ParseMessages(messages)
}

// GetMessage retrieves a protocol message from the database
func (c *clientImpl) GetMessage(messageID string) (*protocol.Message, error) {
	dbMessage, err := get[Message](c.db, Clause{Key: "id", Value: messageID})
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	var message protocol.Message
	if err := json.Unmarshal([]byte(dbMessage.Data), &message); err != nil {
		return nil, fmt.Errorf("failed to deserialize message: %w", err)
	}

	return &message, nil
}

// DeleteMessage deletes a protocol message from the database
func (c *clientImpl) DeleteMessage(messageID string) error {
	return delete[Message](c.db, Clause{Key: "id", Value: messageID})
}

// ListMessagesByContextID retrieves messages by context ID with optional limit
func (c *clientImpl) ListMessagesByContextID(contextID string, limit int) ([]protocol.Message, error) {
	var dbMessages []Message
	query := c.db.Where("session_id = ?", contextID).Order("created_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&dbMessages).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}

	protocolMessages := make([]protocol.Message, 0, len(dbMessages))
	for _, dbMessage := range dbMessages {
		var protocolMessage protocol.Message
		if err := json.Unmarshal([]byte(dbMessage.Data), &protocolMessage); err != nil {
			return nil, fmt.Errorf("failed to deserialize message: %w", err)
		}
		protocolMessages = append(protocolMessages, protocolMessage)
	}

	return protocolMessages, nil
}

// StoreTask stores a MemoryCancellableTask in the database
func (c *clientImpl) StoreTask(task *protocol.Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to serialize task: %w", err)
	}

	dbTask := Task{
		ID:        task.ID,
		Data:      string(data),
		SessionID: task.ContextID,
	}

	return save(c.db, &dbTask)
}

// GetTask retrieves a MemoryCancellableTask from the database
func (c *clientImpl) GetTask(taskID string) (*protocol.Task, error) {
	dbTask, err := get[Task](c.db, Clause{Key: "id", Value: taskID})
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	var task protocol.Task
	if err := json.Unmarshal([]byte(dbTask.Data), &task); err != nil {
		return nil, fmt.Errorf("failed to deserialize task: %w", err)
	}

	return &task, nil
}

// TaskExists checks if a task exists in the database
func (c *clientImpl) TaskExists(taskID string) bool {
	var count int64
	c.db.Model(&Task{}).Where("id = ?", taskID).Count(&count)
	return count > 0
}

// StorePushNotification stores a push notification configuration in the database
func (c *clientImpl) StorePushNotification(config *protocol.TaskPushNotificationConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize push notification config: %w", err)
	}

	dbPushNotification := PushNotification{
		ID:     config.PushNotificationConfig.ID,
		TaskID: config.TaskID,
		Data:   string(data),
	}

	return save(c.db, &dbPushNotification)
}

// GetPushNotification retrieves a push notification configuration from the database
func (c *clientImpl) GetPushNotification(taskID string, configID string) (*protocol.TaskPushNotificationConfig, error) {
	dbPushNotification, err := get[PushNotification](c.db,
		Clause{Key: "task_id", Value: taskID},
		Clause{Key: "id", Value: configID})
	if err != nil {
		return nil, fmt.Errorf("failed to get push notification config: %w", err)
	}

	var config protocol.TaskPushNotificationConfig
	if err := json.Unmarshal([]byte(dbPushNotification.Data), &config); err != nil {
		return nil, fmt.Errorf("failed to deserialize push notification config: %w", err)
	}

	return &config, nil
}

func (c *clientImpl) ListPushNotifications(taskID string) ([]*protocol.TaskPushNotificationConfig, error) {
	pushNotifications, err := list[PushNotification](c.db, Clause{Key: "task_id", Value: taskID})
	if err != nil {
		return nil, err
	}

	protocolPushNotifications := make([]*protocol.TaskPushNotificationConfig, 0, len(pushNotifications))
	for _, pushNotification := range pushNotifications {
		var protocolPushNotification protocol.TaskPushNotificationConfig
		if err := json.Unmarshal([]byte(pushNotification.Data), &protocolPushNotification); err != nil {
			return nil, fmt.Errorf("failed to deserialize push notification config: %w", err)
		}
		protocolPushNotifications = append(protocolPushNotifications, &protocolPushNotification)
	}

	return protocolPushNotifications, nil
}

// DeletePushNotification deletes a push notification configuration from the database
func (c *clientImpl) DeletePushNotification(taskID string) error {
	return delete[PushNotification](c.db, Clause{Key: "task_id", Value: taskID})
}
