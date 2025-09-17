package a2a

import (
	"context"

	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	"gorm.io/gorm"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

// RecordingManager is similar to the PassthroughManager, but it also stores the task and session information in the database.
// It is used for remote agents, which do not have a connection to kagent, so we use this middleware to handle that.
// Note: We expect the remote agent to handle its task history + token information which we store and parse from.
type RecordingManager struct {
	client   *client.A2AClient
	dbClient database.Client
	agentRef string
}

func NewRecordingManager(client *client.A2AClient, dbClient database.Client, agentRef string) taskmanager.TaskManager {
	return &RecordingManager{
		client:   client,
		dbClient: dbClient,
		agentRef: agentRef,
	}
}

// getUserID extracts user ID from context.
// similar to _get_user_id in kagent-adk's request_converter.py
func (m *RecordingManager) getUserID(ctx context.Context, contextID string) string {
	if session, ok := auth.AuthSessionFrom(ctx); ok {
		if session.Principal().User.ID != "" {
			return session.Principal().User.ID
		}
	}

	return "A2A_USER_" + contextID
}

func (m *RecordingManager) OnSendMessage(ctx context.Context, request protocol.SendMessageParams) (*protocol.MessageResult, error) {
	if request.Message.MessageID == "" {
		request.Message.MessageID = protocol.GenerateMessageID()
	}
	if request.Message.Kind == "" {
		request.Message.Kind = protocol.KindMessage
	}

	result, err := m.client.SendMessage(ctx, request)
	if err != nil {
		return nil, err
	}

	if task, ok := result.Result.(*protocol.Task); ok {
		logger := ctrllog.FromContext(ctx).WithName("recording-manager")

		userID := m.getUserID(ctx, task.ContextID)

		// Get agent ID from the agent ref
		var agentID *string
		if m.agentRef != "" {
			agentIDStr := utils.ConvertToPythonIdentifier(m.agentRef)
			agentID = &agentIDStr
		}

		// Check if session already exists
		session, err := m.dbClient.GetSession(task.ContextID, userID)
		if err != nil {
			if err != gorm.ErrRecordNotFound {
				logger.Error(err, "Failed to get session", "contextID", task.ContextID)
				return nil, err
			}

			// Session doesn't exist, create a new one
			session = &database.Session{
				ID:      task.ContextID,
				UserID:  userID,
				AgentID: agentID,
			}

			if err := m.dbClient.StoreSession(session); err != nil {
				logger.Error(err, "Failed to create session", "contextID", task.ContextID, "sessionID", session.ID, "agentID", session.AgentID)
				return nil, err
			}
		}

		// Store Task
		if err := m.dbClient.StoreTask(task); err != nil {
			logger.Error(err, "Failed to store task", "taskID", task.ID, "contextID", task.ContextID)
			return nil, err
		}
	}

	return result, nil
}

func (m *RecordingManager) OnSendMessageStream(ctx context.Context, request protocol.SendMessageParams) (<-chan protocol.StreamingMessageEvent, error) {
	if request.Message.MessageID == "" {
		request.Message.MessageID = protocol.GenerateMessageID()
	}
	if request.Message.Kind == "" {
		request.Message.Kind = protocol.KindMessage
	}

	upstream, err := m.client.StreamMessage(ctx, request)
	if err != nil {
		return nil, err
	}

	out := make(chan protocol.StreamingMessageEvent)

	// TODO: We shouldn't _expect_ a Task to be sent during streaming.
	// If not, this means we'd need to have an internal Task. This way we can store it in the database for future reference.
	go func() {
		defer close(out)
		logger := ctrllog.FromContext(ctx).WithName("recording-manager")

		for ev := range upstream {
			// Forward to client
			out <- ev

			switch v := ev.Result.(type) {
			case *protocol.Message:
				if v != nil {
					logger.V(1).Info("Remote agent message",
						"messageID", v.MessageID,
						"contextID", v.ContextID,
					)
				}
			case *protocol.TaskArtifactUpdateEvent:
				if v != nil {
					logger.V(1).Info("Remote agent artifact update",
						"taskID", v.TaskID,
						"contextID", v.ContextID,
						"artifactParts", len(v.Artifact.Parts),
						"metadata", v.Artifact.Metadata)
				}
			case *protocol.TaskStatusUpdateEvent:
				if v != nil {
					logger.V(1).Info("Remote agent status update",
						"taskID", v.TaskID,
						"contextID", v.ContextID,
						"state", v.Status.State,
						"final", v.Final,
						"metadata", v.Metadata)
				}
			case *protocol.Task:
				if v != nil {
					// Store session
					userID := m.getUserID(ctx, v.ContextID)
					var agentID *string
					if m.agentRef != "" {
						agentIDStr := utils.ConvertToPythonIdentifier(m.agentRef)
						agentID = &agentIDStr
					}

					// Check if session already exists (if it does not exist, create a new one)
					session, err := m.dbClient.GetSession(v.ContextID, userID)
					if err != nil {
						// In this case, a non-NotFound error occurred, so do not store the task or any session information
						if err != gorm.ErrRecordNotFound {
							logger.Error(err, "Failed to get session", "contextID", v.ContextID)
							continue
						}

						// Create a new session
						session = &database.Session{
							ID:      v.ContextID,
							UserID:  userID,
							AgentID: agentID,
						}
						if err := m.dbClient.StoreSession(session); err != nil {
							logger.Error(err, "Failed to create session", "contextID", v.ContextID)
							continue
						}
					}

					// Store task (should only happen if a session exists)
					if err := m.dbClient.StoreTask(v); err != nil {
						logger.Error(err, "Failed to store streaming task", "taskID", v.ID, "contextID", v.ContextID)
					}
				}
			}
		}
	}()

	return out, nil
}

func (m *RecordingManager) OnGetTask(ctx context.Context, params protocol.TaskQueryParams) (*protocol.Task, error) {
	return m.client.GetTasks(ctx, params)
}

func (m *RecordingManager) OnCancelTask(ctx context.Context, params protocol.TaskIDParams) (*protocol.Task, error) {
	return m.client.CancelTasks(ctx, params)
}

func (m *RecordingManager) OnPushNotificationSet(ctx context.Context, params protocol.TaskPushNotificationConfig) (*protocol.TaskPushNotificationConfig, error) {
	return m.client.SetPushNotification(ctx, params)
}

func (m *RecordingManager) OnPushNotificationGet(ctx context.Context, params protocol.TaskIDParams) (*protocol.TaskPushNotificationConfig, error) {
	return m.client.GetPushNotification(ctx, params)
}

func (m *RecordingManager) OnResubscribe(ctx context.Context, params protocol.TaskIDParams) (<-chan protocol.StreamingMessageEvent, error) {
	return m.client.ResubscribeTask(ctx, params)
}
