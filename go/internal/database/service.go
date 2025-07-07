package database

import (
	"fmt"

	"gorm.io/gorm"
)

func NewServiceWrapper(manager *Manager) *ServiceWrapper {
	return &ServiceWrapper{
		Agent:            NewService[Agent](manager),
		Message:          NewService[Message](manager),
		Session:          NewService[Session](manager),
		Task:             NewService[Task](manager),
		PushNotification: NewService[PushNotification](manager),
		Feedback:         NewService[Feedback](manager),
		Tool:             NewService[Tool](manager),
		ToolServer:       NewService[ToolServer](manager),
		// EvalTask:         NewService[EvalTask](manager),
		// EvalCriteria:     NewService[EvalCriteria](manager),
		// EvalRun:          NewService[EvalRun](manager),
	}
}

type Model interface {
	TableName() string
}

type ServiceWrapper struct {
	Agent            *Service[Agent]
	Message          *Service[Message]
	Session          *Service[Session]
	Task             *Service[Task]
	PushNotification *Service[PushNotification]
	Feedback         *Service[Feedback]
	Tool             *Service[Tool]
	ToolServer       *Service[ToolServer]
	// EvalTask         *Service[EvalTask]
	// EvalCriteria     *Service[EvalCriteria]
	// EvalRun          *Service[EvalRun]
}

// Service provides high-level database operations
type Service[T Model] struct {
	db *gorm.DB
}

// NewService creates a new database service
func NewService[T Model](manager *Manager) *Service[T] {
	return &Service[T]{db: manager.db}
}

type Clause struct {
	Key   string
	Value interface{}
}

func (s *Service[T]) List(clauses ...Clause) ([]T, error) {
	var models []T
	query := s.db

	for _, clause := range clauses {
		query = query.Where(fmt.Sprintf("%s = ?", clause.Key), clause.Value)
	}

	err := query.Order("created_at DESC").Find(&models).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}
	return models, nil
}

func (s *Service[T]) Get(clauses ...Clause) (*T, error) {
	var model T
	query := s.db

	for _, clause := range clauses {
		query = query.Where(fmt.Sprintf("%s = ?", clause.Key), clause.Value)
	}

	err := query.First(&model).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get model: %w", err)
	}
	return &model, nil
}

func (s *Service[T]) Create(model *T) error {
	err := s.db.Create(model).Error
	if err != nil {
		return fmt.Errorf("failed to create model: %w", err)
	}
	return nil
}

func (s *Service[T]) Update(model *T) error {
	err := s.db.Save(model).Error
	if err != nil {
		return fmt.Errorf("failed to update model: %w", err)
	}
	return nil
}

func (s *Service[T]) Delete(clauses ...Clause) error {
	t := new(T)
	query := s.db

	for _, clause := range clauses {
		query = query.Where(fmt.Sprintf("%s = ?", clause.Key), clause.Value)
	}

	result := query.Delete(t)
	if result.Error != nil {
		return fmt.Errorf("failed to delete model: %w", result.Error)
	}
	return nil
}

// BuildWhereClause is deprecated, use individual Where clauses instead
func BuildWhereClause(clauses ...Clause) string {
	clausesStr := ""
	for idx, clause := range clauses {
		if idx > 0 {
			clausesStr += " AND "
		}
		clausesStr += fmt.Sprintf("%s = %v", clause.Key, clause.Value)
	}
	return clausesStr
}
