package manager

import (
<<<<<<< HEAD
	"fmt"

=======
>>>>>>> d9c75c64f3fabf139d6415ad1305f6c7e2955b6b
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
<<<<<<< HEAD
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
=======
	dbClient database.Client
}

func NewStorage(dbClient database.Client) Storage {
	return &storageImpl{
		dbClient: dbClient,
	}
}

func (s *storageImpl) GetTask(taskID string) (*MemoryCancellableTask, error) {
	task, err := s.dbClient.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	parsedTask, err := task.Parse()
	if err != nil {
		return nil, err
	}
	return NewCancellableTask(parsedTask), nil
}

func (s *storageImpl) TaskExists(taskID string) bool {
	_, err := s.dbClient.GetTask(taskID)
	return err == nil
}

func (s *storageImpl) StoreMessage(message protocol.Message) error {
	return s.dbClient.CreateMessages(&message)
}

func (s *storageImpl) GetMessage(messageID string) (protocol.Message, error) {
	message, err := s.dbClient.GetMessage(messageID)
	if err != nil {
		return protocol.Message{}, err
	}
	return message.Parse()
}

func (s *storageImpl) ListMessagesByContextID(contextID string, limit int) ([]protocol.Message, error) {
	messages, err := s.dbClient.ListMessagesForSession(contextID, utils.GetGlobalUserID())
	if err != nil {
		return nil, err
	}
	protocolMessages := make([]protocol.Message, len(messages))
	for i, message := range messages {
		parsedMessage, err := message.Parse()
		if err != nil {
			return nil, err
		}
		protocolMessages[i] = parsedMessage
>>>>>>> d9c75c64f3fabf139d6415ad1305f6c7e2955b6b
	}
	return protocolMessages, nil
}

func (s *storageImpl) StoreTask(taskID string, task *MemoryCancellableTask) error {
<<<<<<< HEAD
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
=======
	return s.dbClient.CreateTask(task.Task())
}

func (s *storageImpl) StorePushNotification(taskID string, config protocol.TaskPushNotificationConfig) error {
	return s.dbClient.CreatePushNotification(taskID, &config)
}

func (s *storageImpl) GetPushNotification(taskID string) (protocol.TaskPushNotificationConfig, error) {
	pushNotification, err := s.dbClient.GetPushNotification(taskID)
	if err != nil {
		return protocol.TaskPushNotificationConfig{}, err
	}
	return *pushNotification, nil
>>>>>>> d9c75c64f3fabf139d6415ad1305f6c7e2955b6b
}

// StorageOptions contains configuration options for storage implementations
type StorageOptions struct {
	MaxHistoryLength int
}
