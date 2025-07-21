package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

type PassthroughManager struct {
	client *client.A2AClient
}

func NewPassthroughManager(client *client.A2AClient) taskmanager.TaskManager {
	return &PassthroughManager{
		client: client,
	}
}

func (m *PassthroughManager) OnSendMessage(ctx context.Context, request protocol.SendMessageParams) (*protocol.MessageResult, error) {
	byt, _ := json.Marshal(request)
	if request.Message.MessageID == "" {
		request.Message.MessageID = protocol.GenerateMessageID()
	}
	if request.Message.Kind == "" {
		request.Message.Kind = protocol.KindMessage
	}
	slog.Info("OnSendMessage", "request", string(byt))
	return m.client.SendMessage(ctx, request)
}

func (m *PassthroughManager) OnSendMessageStream(ctx context.Context, request protocol.SendMessageParams) (<-chan protocol.StreamingMessageEvent, error) {
	byt, _ := json.Marshal(request)
	if request.Message.MessageID == "" {
		request.Message.MessageID = protocol.GenerateMessageID()
	}
	if request.Message.Kind == "" {
		request.Message.Kind = protocol.KindMessage
	}
	slog.Info("OnSendMessageStream", "request", string(byt))
	return m.client.StreamMessage(ctx, request)
}

func (m *PassthroughManager) OnGetTask(ctx context.Context, params protocol.TaskQueryParams) (*protocol.Task, error) {
	return m.client.GetTasks(ctx, params)
}

func (m *PassthroughManager) OnCancelTask(ctx context.Context, params protocol.TaskIDParams) (*protocol.Task, error) {
	return m.client.CancelTasks(ctx, params)
}

func (m *PassthroughManager) OnPushNotificationSet(ctx context.Context, params protocol.TaskPushNotificationConfig) (*protocol.TaskPushNotificationConfig, error) {
	return m.client.SetPushNotification(ctx, params)
}

func (m *PassthroughManager) OnPushNotificationGet(ctx context.Context, params protocol.TaskIDParams) (*protocol.TaskPushNotificationConfig, error) {
	return m.client.GetPushNotification(ctx, params)
}

func (m *PassthroughManager) OnResubscribe(ctx context.Context, params protocol.TaskIDParams) (<-chan protocol.StreamingMessageEvent, error) {
	// TODO: Implement
	return nil, nil
}

// Deprecated: OnSendTask is deprecated and will be removed in the future.
func (m *PassthroughManager) OnSendTask(ctx context.Context, request protocol.SendTaskParams) (*protocol.Task, error) {
	return nil, errors.New("OnSendTask is deprecated and will be removed in the future")
}

// Deprecated: OnSendTaskSubscribe is deprecated and will be removed in the future.
func (m *PassthroughManager) OnSendTaskSubscribe(ctx context.Context, request protocol.SendTaskParams) (<-chan protocol.TaskEvent, error) {
	return nil, errors.New("OnSendTaskSubscribe is deprecated and will be removed in the future")
}
