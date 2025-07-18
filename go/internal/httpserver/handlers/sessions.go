package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/internal/a2a/manager"
	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	"golang.org/x/net/websocket"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// SessionsHandler handles session-related requests
type SessionsHandler struct {
	*Base
}

// NewSessionsHandler creates a new SessionsHandler
func NewSessionsHandler(base *Base) *SessionsHandler {
	return &SessionsHandler{Base: base}
}

// RunRequest represents a run creation request
type RunRequest struct {
	Task string `json:"task"`
}

func (h *SessionsHandler) HandleGetSessionsForAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "get-sessions-for-agent")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get agent ref from path", err))
		return
	}
	log = log.WithValues("namespace", namespace)

	agentName, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get agent namespace from path", err))
		return
	}
	log = log.WithValues("agentName", agentName)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	// Get agent ID from agent ref
	agent, err := h.DatabaseService.GetAgent(namespace + "/" + agentName)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Agent not found", err))
		return
	}

	log.V(1).Info("Getting sessions for agent from database")
	sessions, err := h.DatabaseService.ListSessionsForAgent(agent.ID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get sessions for agent", err))
		return
	}

	log.Info("Successfully listed sessions", "count", len(sessions))
	data := api.NewResponse(sessions, "Successfully listed sessions", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleListSessions handles GET /api/sessions requests using database
func (h *SessionsHandler) HandleListSessions(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "list-db")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Listing sessions from database")
	sessions, err := h.DatabaseService.ListSessions(userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list sessions", err))
		return
	}

	log.Info("Successfully listed sessions", "count", len(sessions))
	data := api.NewResponse(sessions, "Successfully listed sessions", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleCreateSession handles POST /api/sessions requests using database
func (h *SessionsHandler) HandleCreateSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "create-db")

	var sessionRequest api.SessionRequest
	if err := DecodeJSONBody(r, &sessionRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if sessionRequest.UserID == "" {
		w.RespondWithError(errors.NewBadRequestError("user_id is required", nil))
		return
	}
	log = log.WithValues("userID", sessionRequest.UserID)

	if sessionRequest.AgentRef == nil {
		w.RespondWithError(errors.NewBadRequestError("agent_ref is required", nil))
		return
	}
	log = log.WithValues("agentRef", *sessionRequest.AgentRef)

	id := uuid.New().String()
	name := id
	if sessionRequest.Name != nil {
		name = *sessionRequest.Name
	}

	agent, err := h.DatabaseService.GetAgent(*sessionRequest.AgentRef)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Agent not found", err))
		return
	}

	session := &database.Session{
		ID:      id,
		Name:    name,
		UserID:  sessionRequest.UserID,
		AgentID: &agent.ID,
		Url:     agent.Url,
	}

	log.V(1).Info("Creating session in database",
		"agentRef", sessionRequest.AgentRef,
		"name", sessionRequest.Name)

	if err := h.DatabaseService.CreateSession(session); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create session", err))
		return
	}

	log.Info("Successfully created session", "sessionID", session.ID)
	data := api.NewResponse(session, "Successfully created session", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleGetSession handles GET /api/sessions/{session_id} requests using database
func (h *SessionsHandler) HandleGetSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "get-db")

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session name from path", err))
		return
	}
	log = log.WithValues("session_id", sessionID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Getting session from database")
	session, err := h.DatabaseService.GetSession(sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", err))
		return
	}

	log.Info("Successfully retrieved session")
	data := api.NewResponse(session, "Successfully retrieved session", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleUpdateSession handles PUT /api/sessions requests using database
func (h *SessionsHandler) HandleUpdateSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "update-db")

	var sessionRequest api.SessionRequest
	if err := DecodeJSONBody(r, &sessionRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if sessionRequest.Name == nil {
		w.RespondWithError(errors.NewBadRequestError("session name is required", nil))
		return
	}

	if sessionRequest.AgentRef == nil {
		w.RespondWithError(errors.NewBadRequestError("agent_ref is required", nil))
		return
	}
	log = log.WithValues("agentRef", *sessionRequest.AgentRef)

	// Get existing session
	session, err := h.DatabaseService.GetSession(*sessionRequest.Name, sessionRequest.UserID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", err))
		return
	}

	agent, err := h.DatabaseService.GetAgent(*sessionRequest.AgentRef)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Agent not found", err))
		return
	}

	// Update fields
	session.AgentID = &agent.ID

	if err := h.DatabaseService.UpdateSession(session); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to update session", err))
		return
	}

	log.Info("Successfully updated session")
	data := api.NewResponse(session, "Successfully updated session", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleDeleteSession handles DELETE /api/sessions/{session_id} requests using database
func (h *SessionsHandler) HandleDeleteSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "delete-db")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("session_id", sessionID)

	if err := h.DatabaseService.DeleteSession(sessionID, userID); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete session", err))
		return
	}

	log.Info("Successfully deleted session")
	data := api.NewResponse(struct{}{}, "Session deleted successfully", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleListSessionRuns handles GET /api/sessions/{session_id}/tasks requests using database
func (h *SessionsHandler) HandleListSessionTasks(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "list-tasks-db")

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("session_id", sessionID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Getting session tasks from database")
	tasks, err := h.DatabaseService.ListSessionTasks(sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get session runs", err))
		return
	}

	log.Info("Successfully retrieved session tasks", "count", len(tasks))
	data := api.NewResponse(tasks, "Successfully retrieved session tasks", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// EventRequest represents an event/message append request
type EventRequest struct {
	Event map[string]interface{} `json:"event"`
}

func (h *SessionsHandler) HandleAddEventToSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "add-event")
	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("session_id", sessionID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	var eventRequest EventRequest
	if err := DecodeJSONBody(r, &eventRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	// Get session to verify it exists
	session, err := h.DatabaseService.GetSession(sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", err))
		return
	}

	// Convert event to JSON string for storage
	eventData, err := json.Marshal(eventRequest.Event)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to serialize event", err))
		return
	}

	// Create a message entry for the event
	messageID := uuid.New().String()
	message := &database.Message{
		ID:        messageID,
		UserID:    userID,
		Data:      string(eventData),
		SessionID: &session.ID,
	}

	if err := h.DatabaseService.(manager.Storage).StoreMessage(protocol.Message{
		MessageID: messageID,
		ContextID: &session.ID,
		Metadata: map[string]interface{}{
			"event_type": "session_event",
		},
		Parts: []protocol.Part{
			protocol.DataPart{Kind: protocol.KindData, Data: eventData},
		},
	}); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to store event", err))
		return
	}

	log.Info("Successfully added event to session")
	data := api.NewResponse(message, "Event added to session successfully", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

func (h *SessionsHandler) HandleInvokeSession(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "invoke-session")

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	var req autogen_client.InvokeTaskRequest
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	session, err := h.DatabaseService.GetSession(sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", err))
		return
	}

	messages, err := h.DatabaseService.ListMessagesForSession(session.ID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get messages for session", err))
		return
	}

	parsedMessages, err := database.ParseMessages(messages)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to parse messages", err))
		return
	}

	autogenEvents, err := utils.ConvertMessagesToAutogenEvents(parsedMessages)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to convert messages to autogen events", err))
		return
	}
	req.Messages = autogenEvents

	var messagesToClient []autogen_client.Event
	var messageToSave []*protocol.Message
	if session.Url != "" {
		u, err := url.Parse(session.Url)
		if err != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to parse session URL", err))
			return
		}
		u.Scheme = "ws"

		cfg, err := websocket.NewConfig(u.String(), "")
		if err != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to create websocket config", err))
			return
		}
		conn, err := cfg.DialContext(r.Context()) //.NewRequest("POST", session.Url, b)
		if err != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to invoke session", err))
			return
		}
		defer conn.Close()
		var b bytes.Buffer
		if err := json.NewEncoder(&b).Encode(req); err != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to encode request", err))
			return
		}
		// write to buffer first to make sure it goes in one websocket msg
		buf := b.Bytes()
		n, err := conn.Write(buf)
		if err != nil || n != len(buf) {
			w.RespondWithError(errors.NewInternalServerError("Failed to write request to websocket", err))
			return
		}

		//	result, err := http.DefaultClient.Do(req)
		//	if err != nil {
		//		w.RespondWithError(errors.NewInternalServerError("Failed to invoke session", err))
		//		return
		//	}

		var msg = make([]byte, 512*1024)
		n, err = conn.Read(msg)
		if err != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to read response from websocket", err))
			return

		}
		msg = msg[:n] // trim to actual read size

		// defer result.Body.Close()
		// if result.StatusCode != http.StatusOK {
		// 	w.RespondWithError(errors.NewInternalServerError("Failed to invoke session", nil))
		// 	return
		// }
		d := json.NewDecoder(bytes.NewBuffer(msg))
		if err := d.Decode(&messageToSave); err != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to decode response", err))
			return
		}
		for _, msg := range messageToSave {
			m := &autogen_client.BaseEvent{
				Type: *msg.ContextID,
			}
			messagesToClient = append(messagesToClient, m)
		}

	} else {
		result, err := h.AutogenClient.InvokeTask(r.Context(), &req)
		if err != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to invoke session", err))
			return
		}
		messagesToClient = result.TaskResult.Messages
		messageToSave = utils.ConvertAutogenEventsToMessages(nil, &sessionID, result.TaskResult.Messages...)
	}
	if err := h.DatabaseService.CreateMessages(messageToSave...); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create messages", err))
		return
	}

	data := api.NewResponse(messagesToClient, "Successfully invoked session", false)
	RespondWithJSON(w, http.StatusOK, data)
}

func (h *SessionsHandler) HandleInvokeSessionStream(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "invoke-session")

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	var req autogen_client.InvokeTaskRequest
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	session, err := h.DatabaseService.GetSession(sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", err))
		return
	}

	messages, err := h.DatabaseService.ListMessagesForSession(session.ID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get messages for session", err))
		return
	}

	parsedMessages, err := database.ParseMessages(messages)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to parse messages", err))
		return
	}
	if session.Url != "" {
		//		h.handleInvokeSessionStreamWebSocket(w, r, session, req, parsedMessages)
		h.handleInvokeSessionStreamPost(w, r, session, req, parsedMessages)
	} else {
		h.handleInvokeSessionStreamAutogen(w, r, session, req, parsedMessages)
	}
}

type TextPart struct {
	// Text is the text content.
	Text string `json:"text"`
}

type Msg struct {
	Parts []TextPart `json:"parts"`
}

func convert(req autogen_client.InvokeTaskRequest) Msg {
	m := Msg{
		//		MessageID: req.Task,
		//		ContextID: nil,
		//		Metadata:  map[string]interface{}{},
	}

	m.Parts = make([]TextPart, 0, len(req.Messages)+1)
	m.Parts = append(m.Parts, TextPart{Text: req.Task})

	for _, part := range req.Messages {
		switch part := part.(type) {
		case *autogen_client.TextMessage:
			m.Parts = append(m.Parts, TextPart{Text: part.Content})
		}
	}
	return m
}

func (h *SessionsHandler) handleInvokeSessionStreamPost(w ErrorResponseWriter, r *http.Request, session *database.Session, req autogen_client.InvokeTaskRequest, parsedMessages []protocol.Message) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "invoke-session")

	msg := struct {
		SessionID  string `json:"session_id"`
		UserID     string `json:"user_id"`
		AgentID    string `json:"app_name"`
		NewMessage Msg    `json:"new_message"`
	}{
		SessionID:  session.ID,
		UserID:     session.UserID,
		AgentID:    "todo", //session.AgentID,
		NewMessage: convert(req),
	}

	b := bytes.NewBuffer(nil)
	if err := json.NewEncoder(b).Encode(msg); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to encode request", err))
		return
	}

	hreq, err := http.NewRequest("POST", session.Url, b)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to invoke session", err))
		return
	}

	hreq.Header.Set("Content-Type", "application/json")
	result, err := http.DefaultClient.Do(hreq)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to invoke session", err))
		return
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(result.Body)
		strBody := string(body)
		log.Error(fmt.Errorf("failed to invoke session: %s", strBody), "Failed to invoke session", "status", result.Status)
		w.RespondWithError(errors.NewInternalServerError("Failed to invoke session", nil))
		return
	}
	var messageToSave []struct {
		Conent *Msg `json:"content"`
	}

	var messagesReader io.Reader = result.Body
	var debugBuf *bytes.Buffer
	if true {
		debugBuf = bytes.NewBuffer(nil)
		messagesReader = io.TeeReader(result.Body, debugBuf)
	}

	d := json.NewDecoder(messagesReader)
	if err := d.Decode(&messageToSave); err != nil {
		if debugBuf != nil {
			body := debugBuf.String()
			log.Error(err, "Failed to decode response", "body", body)
		} else {
			log.Error(err, "Failed to decode response")
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to decode response", err))
		return
	}
	log.Info("Saving messages", "count", len(messageToSave))
	msgs := make([]*protocol.Message, 0, len(messageToSave))
	for _, m := range messageToSave {
		if m.Conent == nil {
			log.Error(fmt.Errorf("message content is nil"), "Invalid message content")
			continue
		}
		newmsg := &protocol.Message{

			ContextID: &session.ID,
			MessageID: uuid.New().String(),
			Metadata: map[string]interface{}{
				"event_type": "session_event",
			},
		}
		for i := range m.Conent.Parts {
			part := m.Conent.Parts[i]
			newmsg.Parts = append(newmsg.Parts, protocol.TextPart{
				Kind: protocol.KindText,
				Text: part.Text,
			})
		}
		msgs = append(msgs, newmsg)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	w.Flush()

	/* TODO: handle task result
	taskResult := autogen_client.InvokeTaskResult{}
	for event := range messageToSave {
		log.Info(event.String())
		w.Write([]byte(event.String()))
		w.Flush()

		if event.Event == "task_result" {
			if err := json.Unmarshal(event.Data, &taskResult); err != nil {
				log.Error(err, "Failed to unmarshal task result")
				continue
			}
		}
	}
	*/

	if err := h.DatabaseService.CreateMessages(msgs...); err != nil {
		if debugBuf != nil {
			body := debugBuf.String()
			log.Error(err, "Failed to create messages", "body", body)
		} else {
			log.Error(err, "Failed to create messages")
		}
	}
}

func (h *SessionsHandler) handleInvokeSessionStreamWebSocket(w ErrorResponseWriter, r *http.Request, session *database.Session, req autogen_client.InvokeTaskRequest, parsedMessages []protocol.Message) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "invoke-session")

	u, err := url.Parse(session.Url)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to parse session URL", err))
		return
	}
	u.Scheme = "ws"
	u.RawQuery = url.Values{
		"app_name":   []string{fmt.Sprintf("todo-%d", session.AgentID)},
		"user_id":    []string{session.UserID},
		"session_id": []string{session.ID},
	}.Encode()

	cfg, err := websocket.NewConfig(u.String(), "http://localhost:8000")
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create websocket config", err))
		return
	}

	conn, err := cfg.DialContext(r.Context()) //.NewRequest("POST", session.Url, b)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to invoke session", err))
		return
	}
	defer conn.Close()
	var b bytes.Buffer
	if err := json.NewEncoder(&b).Encode(req); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to encode request", err))
		return
	}
	// write to buffer first to make sure it goes in one websocket msg
	buf := b.Bytes()
	n, err := conn.Write(buf)
	if err != nil || n != len(buf) {
		w.RespondWithError(errors.NewInternalServerError("Failed to write request to websocket", err))
		return
	}

	var messageToSave []*protocol.Message
	var msg = make([]byte, 512*1024)
	n, err = conn.Read(msg)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to read response from websocket", err))
		return

	}
	msg = msg[:n] // trim to actual read size
	d := json.NewDecoder(bytes.NewBuffer(msg))
	if err := d.Decode(&messageToSave); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to decode response", err))
		return
	}
	log.Info("Saving messages", "count", len(messageToSave))
	if err := h.DatabaseService.CreateMessages(messageToSave...); err != nil {
		log.Error(err, "Failed to create messages")
	}
}

func (h *SessionsHandler) handleInvokeSessionStreamAutogen(w ErrorResponseWriter, r *http.Request, session *database.Session, req autogen_client.InvokeTaskRequest, parsedMessages []protocol.Message) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "invoke-session")

	autogenEvents, err := utils.ConvertMessagesToAutogenEvents(parsedMessages)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to convert messages to autogen events", err))
		return
	}
	req.Messages = autogenEvents

	ch, err := h.AutogenClient.InvokeTaskStream(r.Context(), &req)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to invoke session", err))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	w.Flush()

	taskResult := autogen_client.InvokeTaskResult{}

	for event := range ch {
		log.Info(event.String())
		w.Write([]byte(event.String()))
		w.Flush()

		if event.Event == "task_result" {
			if err := json.Unmarshal(event.Data, &taskResult); err != nil {
				log.Error(err, "Failed to unmarshal task result")
				continue
			}
		}

	}

	messageToSave := utils.ConvertAutogenEventsToMessages(nil, &session.ID, taskResult.TaskResult.Messages...)
	log.Info("Saving messages", "count", len(messageToSave))
	if err := h.DatabaseService.CreateMessages(messageToSave...); err != nil {
		log.Error(err, "Failed to create messages")
	}
}

func (h *SessionsHandler) HandleListSessionMessages(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "list-messages-db")

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("session_id", sessionID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	messages, err := h.DatabaseService.ListMessagesForSession(sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get messages for session", err))
		return
	}

	parsedMessages, err := database.ParseMessages(messages)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to parse messages", err))
		return
	}

	autogenEvents, err := utils.ConvertMessagesToAutogenEvents(parsedMessages)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to convert messages to autogen events", err))
		return
	}

	data := api.NewResponse(autogenEvents, "Successfully retrieved session messages", false)
	RespondWithJSON(w, http.StatusOK, data)
}
