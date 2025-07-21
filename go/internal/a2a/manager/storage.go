package manager

import (
	"fmt"

	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// Storage defines the interface for persisting task manager data
type Storage interface {
	// Message operations
	StoreMessage(message protocol.Message) error
	GetMessage(messageID string) (protocol.Message, error)
	// List messages by context ID, if limit is -1, return all messages
	ListMessagesByContextID(contextID string, limit int) ([]protocol.Message, error)

	// Task operations
	StoreTask(taskID string, task *MemoryCancellableTask) error
	GetTask(taskID string) (*MemoryCancellableTask, error)
	TaskExists(taskID string) bool

	// Push notification operations
	StorePushNotification(taskID string, config protocol.TaskPushNotificationConfig) error
	GetPushNotification(taskID string) (protocol.TaskPushNotificationConfig, error)
}

type storageImpl struct {
	db database.Client
}

func NewStorage(db database.Client) Storage {
	return &storageImpl{
		db: db,
	}
}

func (s *storageImpl) StoreMessage(message protocol.Message) error {
	return s.db.StoreMessages(&message)
}

func (s *storageImpl) GetMessage(messageID string) (protocol.Message, error) {
	message, err := s.db.GetMessage(messageID)
	if err != nil {
		return protocol.Message{}, err
	}
	return *message, nil
}

func (s *storageImpl) ListMessagesByContextID(contextID string, limit int) ([]protocol.Message, error) {
	messages, err := s.db.ListMessagesForSession(contextID, utils.GetGlobalUserID())
	if err != nil {
		return nil, err
	}
	protocolMessages := make([]protocol.Message, 0, len(messages))
	for _, message := range messages {
		protocolMessages = append(protocolMessages, *message)
	}
	return protocolMessages, nil
}

func (s *storageImpl) StoreTask(taskID string, task *MemoryCancellableTask) error {
	return s.db.StoreTask(task.Task())
}

func (s *storageImpl) GetTask(taskID string) (*MemoryCancellableTask, error) {
	task, err := s.db.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	return NewCancellableTask(*task), nil
}

func (s *storageImpl) TaskExists(taskID string) bool {
	task, err := s.db.GetTask(taskID)
	if err != nil {
		return false
	}
	return task != nil
}

func (s *storageImpl) StorePushNotification(taskID string, config protocol.TaskPushNotificationConfig) error {
	return s.db.StorePushNotification(&config)
}

func (s *storageImpl) GetPushNotification(taskID string) (protocol.TaskPushNotificationConfig, error) {
	configs, err := s.db.ListPushNotifications(taskID)
	if err != nil {
		return protocol.TaskPushNotificationConfig{}, err
	}
	if len(configs) == 0 {
		return protocol.TaskPushNotificationConfig{}, fmt.Errorf("no push notification config found for task %s", taskID)
	}
	return *configs[0], nil
}

// StorageOptions contains configuration options for storage implementations
type StorageOptions struct {
	MaxHistoryLength int
}
