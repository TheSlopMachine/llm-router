package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// Router-side validation and missing-record failures use ProviderError so
// the edge renders the correct 400/404 envelope through the shared
// classifier (video.go:79 precedent).
func invalidRequest(msg string) error {
	return &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: msg}
}

func compatNotFound(resource, id string) error {
	return &models.ProviderError{StatusCode: 404, Code: "not_found", Message: fmt.Sprintf("%s %q not found", resource, id)}
}

func (s *Service) mustResponseStore() error {
	if s.responseSvc == nil {
		return errors.New("responses store is not wired")
	}
	return nil
}

func mustPayload(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return raw
}

func decodePayload(rec *models.ResponseRecord, out any) error {
	if len(rec.Payload) == 0 {
		return invalidRequest("stored record has no payload")
	}
	if err := json.Unmarshal(rec.Payload, out); err != nil {
		return fmt.Errorf("stored record is corrupt: %w", err)
	}
	return nil
}

// responseFromChat maps one chat completion onto a ResponseObject.
func responseFromChat(id string, req *models.ResponseRequest, resp *models.ChatCompletionResponse) *models.ResponseObject {
	out := &models.ResponseObject{
		ID:                 id,
		Object:             "response",
		CreatedAt:          time.Now().Unix(),
		Model:              req.Model.String(),
		Status:             "completed",
		Output:             []models.ResponseOutputItem{},
		Instructions:       req.Instructions,
		PreviousResponseID: req.PreviousResponseID,
		ConversationID:     req.ConversationID,
		Metadata:           req.Metadata,
	}
	if len(resp.Choices) == 0 {
		return out
	}
	choice := resp.Choices[0]
	msg := choice.Message
	if msg.Content != "" {
		out.Output = append(out.Output, models.ResponseOutputItem{
			Type:   "message",
			Text:   msg.Content,
			Status: "completed",
		})
	}
	for _, tc := range msg.ToolCalls {
		out.Output = append(out.Output, models.ResponseOutputItem{
			Type:   "function_call",
			ID:     tc.ID,
			Name:   tc.Function.Name,
			Status: "completed",
			Extra:  json.RawMessage(`{"arguments":` + quoteJSONString(tc.Function.Arguments) + `}`),
		})
	}
	out.Usage = &models.ResponseUsage{
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
		TotalTokens:  resp.Usage.TotalTokens,
	}
	return out
}

func quoteJSONString(s string) string {
	raw, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(raw)
}

// ─────────────────────────────────────────────
// Responses
// ─────────────────────────────────────────────

// CreateResponse runs the request through the chat pipeline and persists
// the response object, unless Store is explicitly false. Background is
// accepted and executed inline: the router has no deferred worker, so the
// object always returns completed or failed.
func (s *Service) CreateResponse(
	ctx context.Context,
	req *models.ResponseRequest,
	token *models.RouterToken,
) (*models.ResponseObject, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	chatReq := req.ToChat()
	if len(chatReq.Messages) == 0 {
		return nil, invalidRequest("input is required: provide a string or input items")
	}
	if req.PreviousResponseID != "" {
		if _, err := s.responseSvc.Get(req.PreviousResponseID, models.ResponseRecordResponse); err != nil {
			if errors.Is(err, apierrors.ErrNotFound) {
				return nil, compatNotFound("response", req.PreviousResponseID)
			}
			return nil, err
		}
	}
	resp, err := s.complete(ctx, chatReq, token, false)
	if err != nil {
		return nil, err
	}
	out := responseFromChat(models.NewResponseID(), req, resp)
	if req.Store == nil || *req.Store {
		if err := s.responseSvc.Create(&models.ResponseRecord{
			ID:      out.ID,
			Kind:    models.ResponseRecordResponse,
			Model:   req.Model,
			Status:  out.Status,
			RefID:   req.PreviousResponseID,
			Input:   req.Input,
			Payload: mustPayload(out),
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// GetResponse returns one stored response. Token rules apply to the stored
// request model, exactly like video poll/content authorization.
func (s *Service) GetResponse(
	ctx context.Context,
	id string,
	token *models.RouterToken,
) (*models.ResponseObject, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	rec, err := s.responseSvc.Get(id, models.ResponseRecordResponse)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("response", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return nil, deny
	}
	var out models.ResponseObject
	if err := decodePayload(rec, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelResponse marks a non-terminal response canceled. Synchronously
// executed responses are terminal, so cancel succeeds only on rows still
// open (e.g. requires_action runs surfaced as responses).
func (s *Service) CancelResponse(
	_ context.Context,
	id string,
	token *models.RouterToken,
) (*models.ResponseObject, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	rec, err := s.responseSvc.Get(id, models.ResponseRecordResponse)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("response", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return nil, deny
	}
	if rec.Status == "completed" || rec.Status == "failed" || rec.Status == "canceled" {
		return nil, invalidRequest(fmt.Sprintf("response %q is already %s", id, rec.Status))
	}
	var out models.ResponseObject
	if err := decodePayload(rec, &out); err != nil {
		return nil, err
	}
	out.Status = "canceled"
	rec.Status = "canceled"
	rec.Payload = mustPayload(&out)
	if err := s.responseSvc.Update(rec); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListResponseInputItems returns the stored input of a response as items: a
// stored string becomes one message item, stored items return as-is.
func (s *Service) ListResponseInputItems(
	_ context.Context,
	id string,
	token *models.RouterToken,
) ([]models.ResponseInputItem, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	rec, err := s.responseSvc.Get(id, models.ResponseRecordResponse)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("response", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return nil, deny
	}
	return responseInputItems(rec.Input), nil
}

func responseInputItems(raw json.RawMessage) []models.ResponseInputItem {
	if len(raw) == 0 {
		return []models.ResponseInputItem{}
	}
	var items []models.ResponseInputItem
	if err := json.Unmarshal(raw, &items); err == nil {
		return items
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []models.ResponseInputItem{{Type: "message", Role: "user", Content: raw}}
	}
	return []models.ResponseInputItem{}
}

// CompactResponse collapses a response input chain. The router stores flat
// inputs, so compaction returns the stored object unchanged.
func (s *Service) CompactResponse(
	ctx context.Context,
	id string,
	token *models.RouterToken,
) (*models.ResponseObject, error) {
	return s.GetResponse(ctx, id, token)
}

// CountResponseInputTokens estimates input tokens for a response request
// with the documented 4-chars-per-token heuristic.
func (s *Service) CountResponseInputTokens(req *models.ResponseRequest) int {
	texts := req.InputTexts()
	if req.Instructions != "" {
		texts = append(texts, req.Instructions)
	}
	n := 0
	for _, t := range texts {
		n += len(t)
	}
	if n == 0 {
		return 1
	}
	return models.EstimateInputTokens(texts)
}

// ─────────────────────────────────────────────
// Conversations
// ─────────────────────────────────────────────

// CreateConversation persists a conversation container. Model is optional:
// containers carry no model until items referencing one arrive.
func (s *Service) CreateConversation(
	_ context.Context,
	model models.ModelId,
	metadata map[string]string,
	items []models.ResponseInputItem,
) (*models.ConversationObject, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	out := &models.ConversationObject{
		ID:        models.NewConversationID(),
		Object:    "conversation",
		CreatedAt: time.Now().Unix(),
		Metadata:  metadata,
	}
	if items == nil {
		items = []models.ResponseInputItem{}
	}
	if err := s.responseSvc.Create(&models.ResponseRecord{
		ID:      out.ID,
		Kind:    models.ResponseRecordConversation,
		Model:   model,
		Status:  "active",
		Input:   mustPayload(items),
		Payload: mustPayload(out),
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// GetConversation returns one stored conversation.
func (s *Service) GetConversation(
	_ context.Context,
	id string,
	token *models.RouterToken,
) (*models.ConversationObject, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	rec, err := s.responseSvc.Get(id, models.ResponseRecordConversation)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("conversation", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return nil, deny
	}
	var out models.ConversationObject
	if err := decodePayload(rec, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteConversation removes a conversation container. Items stored as
// responses with the conversation id stay addressable by response id.
func (s *Service) DeleteConversation(_ context.Context, id string) error {
	if err := s.mustResponseStore(); err != nil {
		return err
	}
	if _, err := s.responseSvc.Get(id, models.ResponseRecordConversation); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return compatNotFound("conversation", id)
		}
		return err
	}
	return s.responseSvc.Delete(id)
}

// ListConversationItems returns the items appended to a conversation.
func (s *Service) ListConversationItems(
	_ context.Context,
	id string,
	token *models.RouterToken,
) ([]models.ResponseInputItem, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	rec, err := s.responseSvc.Get(id, models.ResponseRecordConversation)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("conversation", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return nil, deny
	}
	return responseInputItems(rec.Input), nil
}

// AppendConversationItems appends items to a conversation.
func (s *Service) AppendConversationItems(
	_ context.Context,
	id string,
	token *models.RouterToken,
	items []models.ResponseInputItem,
) ([]models.ResponseInputItem, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	rec, err := s.responseSvc.Get(id, models.ResponseRecordConversation)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("conversation", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return nil, deny
	}
	cur := responseInputItems(rec.Input)
	cur = append(cur, items...)
	rec.Input = mustPayload(cur)
	if err := s.responseSvc.Update(rec); err != nil {
		return nil, err
	}
	return cur, nil
}

// ─────────────────────────────────────────────
// Assistants
// ─────────────────────────────────────────────

// CreateAssistant persists an assistant preset. Model is required: presets
// execute runs through the chat pipeline.
func (s *Service) CreateAssistant(
	_ context.Context,
	req *models.AssistantRequest,
) (*models.Assistant, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	if req.Model == "" {
		return nil, invalidRequest("model is required")
	}
	if _, _, err := req.Model.Parse(); err != nil {
		return nil, invalidRequest(fmt.Sprintf("invalid model id: %s", err))
	}
	out := &models.Assistant{
		ID:             models.NewAssistantID(),
		Object:         "assistant",
		CreatedAt:      time.Now().Unix(),
		Model:          req.Model.String(),
		Name:           req.Name,
		Description:    req.Description,
		Instructions:   req.Instructions,
		Tools:          req.Tools,
		ResponseFormat: req.ResponseFormat,
		Metadata:       req.Metadata,
	}
	if err := s.responseSvc.Create(&models.ResponseRecord{
		ID:      out.ID,
		Kind:    models.ResponseRecordAssistant,
		Model:   req.Model,
		Status:  "active",
		Payload: mustPayload(out),
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAssistant returns one stored assistant.
func (s *Service) GetAssistant(
	_ context.Context,
	id string,
	token *models.RouterToken,
) (*models.Assistant, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	rec, err := s.responseSvc.Get(id, models.ResponseRecordAssistant)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("assistant", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return nil, deny
	}
	var out models.Assistant
	if err := decodePayload(rec, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateAssistant patches name, description, instructions, tools,
// response_format and metadata. Model changes re-parse the id.
func (s *Service) UpdateAssistant(
	_ context.Context,
	id string,
	token *models.RouterToken,
	req *models.AssistantRequest,
) (*models.Assistant, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	rec, err := s.responseSvc.Get(id, models.ResponseRecordAssistant)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("assistant", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return nil, deny
	}
	var out models.Assistant
	if err := decodePayload(rec, &out); err != nil {
		return nil, err
	}
	if req.Model != "" {
		if _, _, err := req.Model.Parse(); err != nil {
			return nil, invalidRequest(fmt.Sprintf("invalid model id: %s", err))
		}
		out.Model = req.Model.String()
		rec.Model = req.Model
	}
	if req.Name != "" {
		out.Name = req.Name
	}
	if req.Description != "" {
		out.Description = req.Description
	}
	if req.Instructions != "" {
		out.Instructions = req.Instructions
	}
	if req.Tools != nil {
		out.Tools = req.Tools
	}
	if len(req.ResponseFormat) > 0 {
		out.ResponseFormat = req.ResponseFormat
	}
	if req.Metadata != nil {
		out.Metadata = req.Metadata
	}
	rec.Payload = mustPayload(&out)
	if err := s.responseSvc.Update(rec); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAssistant removes an assistant preset. Runs referencing it stay
// addressable by run id.
func (s *Service) DeleteAssistant(_ context.Context, id string) error {
	if err := s.mustResponseStore(); err != nil {
		return err
	}
	if _, err := s.responseSvc.Get(id, models.ResponseRecordAssistant); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return compatNotFound("assistant", id)
		}
		return err
	}
	return s.responseSvc.Delete(id)
}

// ListAssistants returns all stored assistants, oldest first.
func (s *Service) ListAssistants(_ context.Context) ([]*models.Assistant, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	recs, err := s.responseSvc.List([]models.ResponseRecordKind{models.ResponseRecordAssistant}, "")
	if err != nil {
		return nil, err
	}
	out := make([]*models.Assistant, 0, len(recs))
	for _, rec := range recs {
		var a models.Assistant
		if err := decodePayload(rec, &a); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, nil
}

// ─────────────────────────────────────────────
// Threads and thread messages
// ─────────────────────────────────────────────

// CreateThread persists a thread handle.
func (s *Service) CreateThread(
	_ context.Context,
	metadata map[string]string,
) (*models.Thread, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	out := &models.Thread{
		ID:        models.NewThreadID(),
		Object:    "thread",
		CreatedAt: time.Now().Unix(),
		Metadata:  metadata,
	}
	if err := s.responseSvc.Create(&models.ResponseRecord{
		ID:      out.ID,
		Kind:    models.ResponseRecordThread,
		Status:  "active",
		Payload: mustPayload(out),
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// GetThread returns one stored thread.
func (s *Service) GetThread(
	_ context.Context,
	id string,
	token *models.RouterToken,
) (*models.Thread, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	rec, err := s.responseSvc.Get(id, models.ResponseRecordThread)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("thread", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return nil, deny
	}
	var out models.Thread
	if err := decodePayload(rec, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateThread patches thread metadata.
func (s *Service) UpdateThread(
	_ context.Context,
	id string,
	metadata map[string]string,
) (*models.Thread, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	rec, err := s.responseSvc.Get(id, models.ResponseRecordThread)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("thread", id)
		}
		return nil, err
	}
	var out models.Thread
	if err := decodePayload(rec, &out); err != nil {
		return nil, err
	}
	if metadata != nil {
		out.Metadata = metadata
	}
	rec.Payload = mustPayload(&out)
	if err := s.responseSvc.Update(rec); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteThread removes a thread handle. Messages and runs referencing it
// stay addressable by id.
func (s *Service) DeleteThread(_ context.Context, id string) error {
	if err := s.mustResponseStore(); err != nil {
		return err
	}
	if _, err := s.responseSvc.Get(id, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return compatNotFound("thread", id)
		}
		return err
	}
	return s.responseSvc.Delete(id)
}

// CreateThreadMessage appends a user or assistant message to a thread.
func (s *Service) CreateThreadMessage(
	_ context.Context,
	threadID string,
	req *models.ThreadMessageRequest,
) (*models.ThreadMessage, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	if _, err := s.responseSvc.Get(threadID, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("thread", threadID)
		}
		return nil, err
	}
	if req.Role != "user" && req.Role != "assistant" {
		return nil, invalidRequest(`role must be "user" or "assistant"`)
	}
	texts := req.ThreadMessageTexts()
	if len(texts) == 0 {
		return nil, invalidRequest("content is required: provide a string or text blocks")
	}
	out := &models.ThreadMessage{
		ID:        models.NewThreadMessageID(),
		Object:    "thread.message",
		CreatedAt: time.Now().Unix(),
		ThreadID:  threadID,
		Role:      req.Role,
		Metadata:  req.Metadata,
	}
	for _, t := range texts {
		out.Content = append(out.Content, models.ThreadContent{Type: "text", Text: t})
	}
	if err := s.responseSvc.Create(&models.ResponseRecord{
		ID:       out.ID,
		Kind:     models.ResponseRecordThreadMessage,
		Status:   "completed",
		ParentID: threadID,
		Payload:  mustPayload(out),
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// GetThreadMessage returns one thread message, asserting thread membership.
func (s *Service) GetThreadMessage(
	_ context.Context,
	threadID, messageID string,
) (*models.ThreadMessage, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	rec, err := s.responseSvc.Get(messageID, models.ResponseRecordThreadMessage)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("message", messageID)
		}
		return nil, err
	}
	if rec.ParentID != threadID {
		return nil, compatNotFound("message", messageID)
	}
	var out models.ThreadMessage
	if err := decodePayload(rec, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListThreadMessages returns all messages of a thread, oldest first.
func (s *Service) ListThreadMessages(
	_ context.Context,
	threadID string,
) ([]*models.ThreadMessage, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	if _, err := s.responseSvc.Get(threadID, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("thread", threadID)
		}
		return nil, err
	}
	recs, err := s.responseSvc.List([]models.ResponseRecordKind{models.ResponseRecordThreadMessage}, threadID)
	if err != nil {
		return nil, err
	}
	out := make([]*models.ThreadMessage, 0, len(recs))
	for _, rec := range recs {
		var m models.ThreadMessage
		if err := decodePayload(rec, &m); err != nil {
			return nil, err
		}
		out = append(out, &m)
	}
	return out, nil
}

// ─────────────────────────────────────────────
// Runs
// ─────────────────────────────────────────────

func threadChatMessages(msgs []*models.ThreadMessage) []models.ChatMessage {
	out := make([]models.ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		var texts []string
		for _, c := range m.Content {
			if c.Text != "" {
				texts = append(texts, c.Text)
			}
		}
		if len(texts) == 0 {
			continue
		}
		role := m.Role
		if role != "user" && role != "assistant" {
			role = "user"
		}
		joined := ""
		for i, t := range texts {
			if i > 0 {
				joined += "\n"
			}
			joined += t
		}
		out = append(out, models.ChatMessage{Role: role, Content: joined})
	}
	return out
}

// runFromChat maps a chat completion onto a run: tool calls park the run in
// requires_action with the pending turns stashed for submit_tool_outputs.
func runFromChat(run *models.Run, resp *models.ChatCompletionResponse) ([]models.ChatMessage, bool) {
	if len(resp.Choices) == 0 {
		run.Status = "completed"
		return nil, false
	}
	choice := resp.Choices[0]
	if len(choice.Message.ToolCalls) == 0 {
		run.Status = "completed"
		return nil, false
	}
	run.Status = "requires_action"
	action := &models.RunRequiredAction{
		Type:              "submit_tool_outputs",
		SubmitToolOutputs: &models.SubmitToolOutputs{},
	}
	pending := []models.ChatMessage{choice.Message}
	for _, tc := range choice.Message.ToolCalls {
		action.SubmitToolOutputs.ToolCalls = append(action.SubmitToolOutputs.ToolCalls, models.RunToolCall{
			ID:   tc.ID,
			Type: "function",
		})
		action.SubmitToolOutputs.ToolCalls[len(action.SubmitToolOutputs.ToolCalls)-1].Function.Name = tc.Function.Name
		action.SubmitToolOutputs.ToolCalls[len(action.SubmitToolOutputs.ToolCalls)-1].Function.Arguments = tc.Function.Arguments
	}
	run.RequiredAction = action
	return pending, true
}

// CreateRun executes an assistant over a thread through the chat pipeline.
// Runs with tool calls park in requires_action; completed runs append the
// assistant turn to the thread, mirroring OpenAI run semantics.
func (s *Service) CreateRun(
	ctx context.Context,
	threadID string,
	req *models.RunRequest,
	token *models.RouterToken,
) (*models.Run, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	if _, err := s.responseSvc.Get(threadID, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("thread", threadID)
		}
		return nil, err
	}
	if req.AssistantID == "" {
		return nil, invalidRequest("assistant_id is required")
	}
	assistantRec, err := s.responseSvc.Get(req.AssistantID, models.ResponseRecordAssistant)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("assistant", req.AssistantID)
		}
		return nil, err
	}
	var assistant models.Assistant
	if err := decodePayload(assistantRec, &assistant); err != nil {
		return nil, err
	}
	model := req.Model
	if model == "" {
		model = models.ModelId(assistant.Model)
	}
	if model == "" {
		return nil, invalidRequest("model is required: set it on the run or the assistant")
	}
	instructions := req.Instructions
	if instructions == "" {
		instructions = assistant.Instructions
	}
	msgs, err := s.ListThreadMessages(ctx, threadID)
	if err != nil {
		return nil, err
	}
	chatReq := &models.ChatCompletionRequest{Model: model}
	if instructions != "" {
		chatReq.Messages = append(chatReq.Messages, models.ChatMessage{Role: "system", Content: instructions})
	}
	history := threadChatMessages(msgs)
	chatReq.Messages = append(chatReq.Messages, history...)
	if len(chatReq.Messages) == 0 {
		return nil, invalidRequest("thread has no messages to run")
	}
	for _, raw := range assistant.Tools {
		var fn struct {
			Type        string         `json:"type"`
			Name        string         `json:"name"`
			Description string         `json:"description,omitempty"`
			Parameters  map[string]any `json:"parameters,omitempty"`
		}
		if err := json.Unmarshal(raw, &fn); err != nil || fn.Name == "" {
			continue
		}
		if fn.Type != "" && fn.Type != "function" {
			continue
		}
		chatReq.Tools = append(chatReq.Tools, models.ChatTool{
			Type:     "function",
			Function: &models.ChatToolFunction{Name: fn.Name, Description: fn.Description, Parameters: fn.Parameters},
		})
	}
	run := &models.Run{
		ID:          models.NewRunID(),
		Object:      "thread.run",
		CreatedAt:   time.Now().Unix(),
		ThreadID:    threadID,
		AssistantID: req.AssistantID,
		Model:       model.String(),
		Status:      "in_progress",
		Metadata:    req.Metadata,
	}
	if instructions != "" {
		run.Instructions = instructions
	}
	resp, err := s.complete(ctx, chatReq, token, false)
	if err != nil {
		run.Status = "failed"
		if rerr := s.responseSvc.Create(&models.ResponseRecord{
			ID:       run.ID,
			Kind:     models.ResponseRecordRun,
			Model:    model,
			Status:   run.Status,
			ParentID: threadID,
			RefID:    req.AssistantID,
			Payload:  mustPayload(run),
		}); rerr != nil {
			return nil, rerr
		}
		return nil, err
	}
	pending, waiting := runFromChat(run, resp)
	rec := &models.ResponseRecord{
		ID:       run.ID,
		Kind:     models.ResponseRecordRun,
		Model:    model,
		Status:   run.Status,
		ParentID: threadID,
		RefID:    req.AssistantID,
		Payload:  mustPayload(run),
	}
	if waiting {
		turns := append(append([]models.ChatMessage{}, chatReq.Messages...), pending...)
		rec.Input = mustPayload(turns)
	}
	if err := s.responseSvc.Create(rec); err != nil {
		return nil, err
	}
	if !waiting && len(resp.Choices) > 0 && resp.Choices[0].Message.Content != "" {
		_, _ = s.CreateThreadMessage(ctx, threadID, &models.ThreadMessageRequest{
			Role:    "assistant",
			Content: json.RawMessage(quoteJSONString(resp.Choices[0].Message.Content)),
		})
	}
	return run, nil
}

// GetRun returns one run, asserting thread membership.
func (s *Service) GetRun(
	_ context.Context,
	threadID, runID string,
	token *models.RouterToken,
) (*models.Run, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	rec, err := s.responseSvc.Get(runID, models.ResponseRecordRun)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("run", runID)
		}
		return nil, err
	}
	if rec.ParentID != threadID {
		return nil, compatNotFound("run", runID)
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return nil, deny
	}
	var out models.Run
	if err := decodePayload(rec, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRuns returns all runs of a thread, oldest first.
func (s *Service) ListRuns(
	_ context.Context,
	threadID string,
) ([]*models.Run, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	if _, err := s.responseSvc.Get(threadID, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("thread", threadID)
		}
		return nil, err
	}
	recs, err := s.responseSvc.List([]models.ResponseRecordKind{models.ResponseRecordRun}, threadID)
	if err != nil {
		return nil, err
	}
	out := make([]*models.Run, 0, len(recs))
	for _, rec := range recs {
		var r models.Run
		if err := decodePayload(rec, &r); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, nil
}

// CancelRun marks a non-terminal run canceled.
func (s *Service) CancelRun(
	_ context.Context,
	threadID, runID string,
	token *models.RouterToken,
) (*models.Run, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	rec, err := s.responseSvc.Get(runID, models.ResponseRecordRun)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("run", runID)
		}
		return nil, err
	}
	if rec.ParentID != threadID {
		return nil, compatNotFound("run", runID)
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return nil, deny
	}
	var out models.Run
	if err := decodePayload(rec, &out); err != nil {
		return nil, err
	}
	switch out.Status {
	case "completed", "failed", "canceled":
		return nil, invalidRequest(fmt.Sprintf("run %q is already %s", runID, out.Status))
	}
	out.Status = "canceled"
	out.RequiredAction = nil
	rec.Status = "canceled"
	rec.Payload = mustPayload(&out)
	if err := s.responseSvc.Update(rec); err != nil {
		return nil, err
	}
	return &out, nil
}

// SubmitToolOutputs continues a requires_action run: tool outputs join the
// stashed turns, the pipeline runs again, and completion appends the
// assistant turn to the thread.
func (s *Service) SubmitToolOutputs(
	ctx context.Context,
	threadID, runID string,
	token *models.RouterToken,
	outputs []models.ToolOutput,
) (*models.Run, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	rec, err := s.responseSvc.Get(runID, models.ResponseRecordRun)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("run", runID)
		}
		return nil, err
	}
	if rec.ParentID != threadID {
		return nil, compatNotFound("run", runID)
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return nil, deny
	}
	var out models.Run
	if err := decodePayload(rec, &out); err != nil {
		return nil, err
	}
	if out.Status != "requires_action" {
		return nil, invalidRequest(fmt.Sprintf("run %q is %s, not requires_action", runID, out.Status))
	}
	var turns []models.ChatMessage
	if len(rec.Input) > 0 {
		if err := json.Unmarshal(rec.Input, &turns); err != nil {
			return nil, invalidRequest("stored run turns are corrupt")
		}
	}
	byID := map[string]string{}
	for _, o := range outputs {
		byID[o.ToolCallID] = o.Output
	}
	if out.RequiredAction == nil || out.RequiredAction.SubmitToolOutputs == nil {
		return nil, invalidRequest("run has no pending tool calls")
	}
	for _, tc := range out.RequiredAction.SubmitToolOutputs.ToolCalls {
		text, ok := byID[tc.ID]
		if !ok {
			return nil, invalidRequest(fmt.Sprintf("missing output for tool call %q", tc.ID))
		}
		turns = append(turns, models.ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: text})
	}
	chatReq := &models.ChatCompletionRequest{Model: rec.Model, Messages: turns}
	resp, err := s.complete(ctx, chatReq, token, false)
	if err != nil {
		return nil, err
	}
	pending, waiting := runFromChat(&out, resp)
	if !waiting {
		out.RequiredAction = nil
	}
	rec.Status = out.Status
	rec.Payload = mustPayload(&out)
	if waiting {
		rec.Input = mustPayload(append(turns, pending...))
	} else {
		rec.Input = mustPayload(turns)
	}
	if err := s.responseSvc.Update(rec); err != nil {
		return nil, err
	}
	if !waiting && len(resp.Choices) > 0 && resp.Choices[0].Message.Content != "" {
		_, _ = s.CreateThreadMessage(ctx, threadID, &models.ThreadMessageRequest{
			Role:    "assistant",
			Content: json.RawMessage(quoteJSONString(resp.Choices[0].Message.Content)),
		})
	}
	return &out, nil
}
