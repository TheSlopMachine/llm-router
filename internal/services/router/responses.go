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
func invalidRequest(message string) error {
	return &models.ProviderError{StatusCode: 400, Code: "invalid_request_error", Message: message}
}

func compatNotFound(resource, id string) error {
	return &models.ProviderError{StatusCode: 404, Code: "not_found", Message: fmt.Sprintf("%s %q not found", resource, id)}
}

func (s *Service) mustResponseStore() error {
	if s.responseService == nil {
		return errors.New("responses store is not wired")
	}
	return nil
}

func mustPayload(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("{}")
	}
	return raw
}

func decodePayload(record *models.ResponseRecord, output any) error {
	if len(record.Payload) == 0 {
		return invalidRequest("stored record has no payload")
	}
	if err := json.Unmarshal(record.Payload, output); err != nil {
		return fmt.Errorf("stored record is corrupt: %w", err)
	}
	return nil
}

// responseFromChat maps one chat completion onto a ResponseObject.
func responseFromChat(id string, request *models.ResponseRequest, response *models.ChatCompletionResponse) *models.ResponseObject {
	output := &models.ResponseObject{
		ID:                 id,
		Object:             "response",
		CreatedAt:          time.Now().Unix(),
		Model:              request.Model.String(),
		Status:             "completed",
		Output:             []models.ResponseOutputItem{},
		Instructions:       request.Instructions,
		PreviousResponseID: request.PreviousResponseID,
		ConversationID:     request.ConversationID,
		Metadata:           request.Metadata,
	}
	if len(response.Choices) == 0 {
		return output
	}
	choice := response.Choices[0]
	message := choice.Message
	if message.Content != "" {
		output.Output = append(output.Output, models.ResponseOutputItem{
			Type:   "message",
			Text:   message.Content,
			Status: "completed",
		})
	}
	for _, toolCall := range message.ToolCalls {
		output.Output = append(output.Output, models.ResponseOutputItem{
			Type:   "function_call",
			ID:     toolCall.ID,
			Name:   toolCall.Function.Name,
			Status: "completed",
			Extra:  json.RawMessage(`{"arguments":` + quoteJSONString(toolCall.Function.Arguments) + `}`),
		})
	}
	output.Usage = &models.ResponseUsage{
		InputTokens:  response.Usage.PromptTokens,
		OutputTokens: response.Usage.CompletionTokens,
		TotalTokens:  response.Usage.TotalTokens,
	}
	return output
}

func quoteJSONString(value string) string {
	raw, err := json.Marshal(value)
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
	request *models.ResponseRequest,
	token *models.RouterToken,
) (*models.ResponseObject, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	chatRequest := request.ToChat()
	if len(chatRequest.Messages) == 0 {
		return nil, invalidRequest("input is required: provide a string or input items")
	}
	if request.PreviousResponseID != "" {
		if _, err := s.responseService.Get(request.PreviousResponseID, models.ResponseRecordResponse); err != nil {
			if errors.Is(err, apierrors.ErrNotFound) {
				return nil, compatNotFound("response", request.PreviousResponseID)
			}
			return nil, err
		}
	}
	response, err := s.complete(ctx, chatRequest, token, false)
	if err != nil {
		return nil, err
	}
	output := responseFromChat(models.NewResponseID(), request, response)
	if request.Store == nil || *request.Store {
		if err := s.responseService.Create(&models.ResponseRecord{
			ID:      output.ID,
			Kind:    models.ResponseRecordResponse,
			Model:   request.Model,
			Status:  output.Status,
			RefID:   request.PreviousResponseID,
			Input:   request.Input,
			Payload: mustPayload(output),
		}); err != nil {
			return nil, err
		}
	}
	return output, nil
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
	record, err := s.responseService.Get(id, models.ResponseRecordResponse)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("response", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return nil, deny
	}
	var output models.ResponseObject
	if err := decodePayload(record, &output); err != nil {
		return nil, err
	}
	return &output, nil
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
	record, err := s.responseService.Get(id, models.ResponseRecordResponse)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("response", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return nil, deny
	}
	if record.Status == "completed" || record.Status == "failed" || record.Status == "canceled" {
		return nil, invalidRequest(fmt.Sprintf("response %q is already %s", id, record.Status))
	}
	var output models.ResponseObject
	if err := decodePayload(record, &output); err != nil {
		return nil, err
	}
	output.Status = "canceled"
	record.Status = "canceled"
	record.Payload = mustPayload(&output)
	if err := s.responseService.Update(record); err != nil {
		return nil, err
	}
	return &output, nil
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
	record, err := s.responseService.Get(id, models.ResponseRecordResponse)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("response", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return nil, deny
	}
	return responseInputItems(record.Input), nil
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
func (s *Service) CountResponseInputTokens(request *models.ResponseRequest) int {
	texts := request.InputTexts()
	if request.Instructions != "" {
		texts = append(texts, request.Instructions)
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
	output := &models.ConversationObject{
		ID:        models.NewConversationID(),
		Object:    "conversation",
		CreatedAt: time.Now().Unix(),
		Metadata:  metadata,
	}
	if items == nil {
		items = []models.ResponseInputItem{}
	}
	if err := s.responseService.Create(&models.ResponseRecord{
		ID:      output.ID,
		Kind:    models.ResponseRecordConversation,
		Model:   model,
		Status:  "active",
		Input:   mustPayload(items),
		Payload: mustPayload(output),
	}); err != nil {
		return nil, err
	}
	return output, nil
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
	record, err := s.responseService.Get(id, models.ResponseRecordConversation)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("conversation", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return nil, deny
	}
	var output models.ConversationObject
	if err := decodePayload(record, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

// DeleteConversation removes a conversation container. Items stored as
// responses with the conversation id stay addressable by response id.
func (s *Service) DeleteConversation(_ context.Context, id string) error {
	if err := s.mustResponseStore(); err != nil {
		return err
	}
	if _, err := s.responseService.Get(id, models.ResponseRecordConversation); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return compatNotFound("conversation", id)
		}
		return err
	}
	return s.responseService.Delete(id)
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
	record, err := s.responseService.Get(id, models.ResponseRecordConversation)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("conversation", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return nil, deny
	}
	return responseInputItems(record.Input), nil
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
	record, err := s.responseService.Get(id, models.ResponseRecordConversation)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("conversation", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return nil, deny
	}
	current := responseInputItems(record.Input)
	current = append(current, items...)
	record.Input = mustPayload(current)
	if err := s.responseService.Update(record); err != nil {
		return nil, err
	}
	return current, nil
}

// ─────────────────────────────────────────────
// Assistants
// ─────────────────────────────────────────────

// CreateAssistant persists an assistant preset. Model is required: presets
// execute runs through the chat pipeline.
func (s *Service) CreateAssistant(
	_ context.Context,
	request *models.AssistantRequest,
) (*models.Assistant, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	if request.Model == "" {
		return nil, invalidRequest("model is required")
	}
	if _, _, err := request.Model.Parse(); err != nil {
		return nil, invalidRequest(fmt.Sprintf("invalid model id: %s", err))
	}
	output := &models.Assistant{
		ID:             models.NewAssistantID(),
		Object:         "assistant",
		CreatedAt:      time.Now().Unix(),
		Model:          request.Model.String(),
		Name:           request.Name,
		Description:    request.Description,
		Instructions:   request.Instructions,
		Tools:          request.Tools,
		ResponseFormat: request.ResponseFormat,
		Metadata:       request.Metadata,
	}
	if err := s.responseService.Create(&models.ResponseRecord{
		ID:      output.ID,
		Kind:    models.ResponseRecordAssistant,
		Model:   request.Model,
		Status:  "active",
		Payload: mustPayload(output),
	}); err != nil {
		return nil, err
	}
	return output, nil
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
	record, err := s.responseService.Get(id, models.ResponseRecordAssistant)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("assistant", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return nil, deny
	}
	var output models.Assistant
	if err := decodePayload(record, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

// UpdateAssistant patches name, description, instructions, tools,
// response_format and metadata. Model changes re-parse the id.
func (s *Service) UpdateAssistant(
	_ context.Context,
	id string,
	token *models.RouterToken,
	request *models.AssistantRequest,
) (*models.Assistant, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	record, err := s.responseService.Get(id, models.ResponseRecordAssistant)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("assistant", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return nil, deny
	}
	var output models.Assistant
	if err := decodePayload(record, &output); err != nil {
		return nil, err
	}
	if request.Model != "" {
		if _, _, err := request.Model.Parse(); err != nil {
			return nil, invalidRequest(fmt.Sprintf("invalid model id: %s", err))
		}
		output.Model = request.Model.String()
		record.Model = request.Model
	}
	if request.Name != "" {
		output.Name = request.Name
	}
	if request.Description != "" {
		output.Description = request.Description
	}
	if request.Instructions != "" {
		output.Instructions = request.Instructions
	}
	if request.Tools != nil {
		output.Tools = request.Tools
	}
	if len(request.ResponseFormat) > 0 {
		output.ResponseFormat = request.ResponseFormat
	}
	if request.Metadata != nil {
		output.Metadata = request.Metadata
	}
	record.Payload = mustPayload(&output)
	if err := s.responseService.Update(record); err != nil {
		return nil, err
	}
	return &output, nil
}

// DeleteAssistant removes an assistant preset. Runs referencing it stay
// addressable by run id.
func (s *Service) DeleteAssistant(_ context.Context, id string) error {
	if err := s.mustResponseStore(); err != nil {
		return err
	}
	if _, err := s.responseService.Get(id, models.ResponseRecordAssistant); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return compatNotFound("assistant", id)
		}
		return err
	}
	return s.responseService.Delete(id)
}

// ListAssistants returns all stored assistants, oldest first.
func (s *Service) ListAssistants(_ context.Context) ([]*models.Assistant, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	records, err := s.responseService.List([]models.ResponseRecordKind{models.ResponseRecordAssistant}, "")
	if err != nil {
		return nil, err
	}
	output := make([]*models.Assistant, 0, len(records))
	for _, record := range records {
		var assistant models.Assistant
		if err := decodePayload(record, &assistant); err != nil {
			return nil, err
		}
		output = append(output, &assistant)
	}
	return output, nil
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
	output := &models.Thread{
		ID:        models.NewThreadID(),
		Object:    "thread",
		CreatedAt: time.Now().Unix(),
		Metadata:  metadata,
	}
	if err := s.responseService.Create(&models.ResponseRecord{
		ID:      output.ID,
		Kind:    models.ResponseRecordThread,
		Status:  "active",
		Payload: mustPayload(output),
	}); err != nil {
		return nil, err
	}
	return output, nil
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
	record, err := s.responseService.Get(id, models.ResponseRecordThread)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("thread", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return nil, deny
	}
	var output models.Thread
	if err := decodePayload(record, &output); err != nil {
		return nil, err
	}
	return &output, nil
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
	record, err := s.responseService.Get(id, models.ResponseRecordThread)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("thread", id)
		}
		return nil, err
	}
	var output models.Thread
	if err := decodePayload(record, &output); err != nil {
		return nil, err
	}
	if metadata != nil {
		output.Metadata = metadata
	}
	record.Payload = mustPayload(&output)
	if err := s.responseService.Update(record); err != nil {
		return nil, err
	}
	return &output, nil
}

// DeleteThread removes a thread handle. Messages and runs referencing it
// stay addressable by id.
func (s *Service) DeleteThread(_ context.Context, id string) error {
	if err := s.mustResponseStore(); err != nil {
		return err
	}
	if _, err := s.responseService.Get(id, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return compatNotFound("thread", id)
		}
		return err
	}
	return s.responseService.Delete(id)
}

// CreateThreadMessage appends a user or assistant message to a thread.
func (s *Service) CreateThreadMessage(
	_ context.Context,
	threadID string,
	request *models.ThreadMessageRequest,
) (*models.ThreadMessage, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	if _, err := s.responseService.Get(threadID, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("thread", threadID)
		}
		return nil, err
	}
	if request.Role != "user" && request.Role != "assistant" {
		return nil, invalidRequest(`role must be "user" or "assistant"`)
	}
	texts := request.ThreadMessageTexts()
	if len(texts) == 0 {
		return nil, invalidRequest("content is required: provide a string or text blocks")
	}
	output := &models.ThreadMessage{
		ID:        models.NewThreadMessageID(),
		Object:    "thread.message",
		CreatedAt: time.Now().Unix(),
		ThreadID:  threadID,
		Role:      request.Role,
		Metadata:  request.Metadata,
	}
	for _, text := range texts {
		output.Content = append(output.Content, models.ThreadContent{Type: "text", Text: text})
	}
	if err := s.responseService.Create(&models.ResponseRecord{
		ID:       output.ID,
		Kind:     models.ResponseRecordThreadMessage,
		Status:   "completed",
		ParentID: threadID,
		Payload:  mustPayload(output),
	}); err != nil {
		return nil, err
	}
	return output, nil
}

// GetThreadMessage returns one thread message, asserting thread membership.
func (s *Service) GetThreadMessage(
	_ context.Context,
	threadID, messageID string,
) (*models.ThreadMessage, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	record, err := s.responseService.Get(messageID, models.ResponseRecordThreadMessage)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("message", messageID)
		}
		return nil, err
	}
	if record.ParentID != threadID {
		return nil, compatNotFound("message", messageID)
	}
	var output models.ThreadMessage
	if err := decodePayload(record, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

// ListThreadMessages returns all messages of a thread, oldest first.
func (s *Service) ListThreadMessages(
	_ context.Context,
	threadID string,
) ([]*models.ThreadMessage, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	if _, err := s.responseService.Get(threadID, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("thread", threadID)
		}
		return nil, err
	}
	records, err := s.responseService.List([]models.ResponseRecordKind{models.ResponseRecordThreadMessage}, threadID)
	if err != nil {
		return nil, err
	}
	output := make([]*models.ThreadMessage, 0, len(records))
	for _, record := range records {
		var message models.ThreadMessage
		if err := decodePayload(record, &message); err != nil {
			return nil, err
		}
		output = append(output, &message)
	}
	return output, nil
}

// ─────────────────────────────────────────────
// Runs
// ─────────────────────────────────────────────

func threadChatMessages(messages []*models.ThreadMessage) []models.ChatMessage {
	output := make([]models.ChatMessage, 0, len(messages))
	for _, message := range messages {
		var texts []string
		for _, content := range message.Content {
			if content.Text != "" {
				texts = append(texts, content.Text)
			}
		}
		if len(texts) == 0 {
			continue
		}
		role := message.Role
		if role != "user" && role != "assistant" {
			role = "user"
		}
		joined := ""
		for i, text := range texts {
			if i > 0 {
				joined += "\n"
			}
			joined += text
		}
		output = append(output, models.ChatMessage{Role: role, Content: joined})
	}
	return output
}

// runFromChat maps a chat completion onto a run: tool calls park the run in
// requires_action with the pending turns stashed for submit_tool_outputs.
func runFromChat(run *models.Run, response *models.ChatCompletionResponse) ([]models.ChatMessage, bool) {
	if len(response.Choices) == 0 {
		run.Status = "completed"
		return nil, false
	}
	choice := response.Choices[0]
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
	for _, toolCall := range choice.Message.ToolCalls {
		action.SubmitToolOutputs.ToolCalls = append(action.SubmitToolOutputs.ToolCalls, models.RunToolCall{
			ID:   toolCall.ID,
			Type: "function",
		})
		action.SubmitToolOutputs.ToolCalls[len(action.SubmitToolOutputs.ToolCalls)-1].Function.Name = toolCall.Function.Name
		action.SubmitToolOutputs.ToolCalls[len(action.SubmitToolOutputs.ToolCalls)-1].Function.Arguments = toolCall.Function.Arguments
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
	request *models.RunRequest,
	token *models.RouterToken,
) (*models.Run, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	if _, err := s.responseService.Get(threadID, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("thread", threadID)
		}
		return nil, err
	}
	if request.AssistantID == "" {
		return nil, invalidRequest("assistant_id is required")
	}
	assistantRecord, err := s.responseService.Get(request.AssistantID, models.ResponseRecordAssistant)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("assistant", request.AssistantID)
		}
		return nil, err
	}
	var assistant models.Assistant
	if err := decodePayload(assistantRecord, &assistant); err != nil {
		return nil, err
	}
	model := request.Model
	if model == "" {
		model = models.ModelId(assistant.Model)
	}
	if model == "" {
		return nil, invalidRequest("model is required: set it on the run or the assistant")
	}
	instructions := request.Instructions
	if instructions == "" {
		instructions = assistant.Instructions
	}
	messages, err := s.ListThreadMessages(ctx, threadID)
	if err != nil {
		return nil, err
	}
	chatRequest := &models.ChatCompletionRequest{Model: model}
	if instructions != "" {
		chatRequest.Messages = append(chatRequest.Messages, models.ChatMessage{Role: "system", Content: instructions})
	}
	history := threadChatMessages(messages)
	chatRequest.Messages = append(chatRequest.Messages, history...)
	if len(chatRequest.Messages) == 0 {
		return nil, invalidRequest("thread has no messages to run")
	}
	for _, raw := range assistant.Tools {
		var function struct {
			Type        string         `json:"type"`
			Name        string         `json:"name"`
			Description string         `json:"description,omitempty"`
			Parameters  map[string]any `json:"parameters,omitempty"`
		}
		if err := json.Unmarshal(raw, &function); err != nil || function.Name == "" {
			continue
		}
		if function.Type != "" && function.Type != "function" {
			continue
		}
		chatRequest.Tools = append(chatRequest.Tools, models.ChatTool{
			Type:     "function",
			Function: &models.ChatToolFunction{Name: function.Name, Description: function.Description, Parameters: function.Parameters},
		})
	}
	run := &models.Run{
		ID:          models.NewRunID(),
		Object:      "thread.run",
		CreatedAt:   time.Now().Unix(),
		ThreadID:    threadID,
		AssistantID: request.AssistantID,
		Model:       model.String(),
		Status:      "in_progress",
		Metadata:    request.Metadata,
	}
	if instructions != "" {
		run.Instructions = instructions
	}
	response, err := s.complete(ctx, chatRequest, token, false)
	if err != nil {
		run.Status = "failed"
		if createError := s.responseService.Create(&models.ResponseRecord{
			ID:       run.ID,
			Kind:     models.ResponseRecordRun,
			Model:    model,
			Status:   run.Status,
			ParentID: threadID,
			RefID:    request.AssistantID,
			Payload:  mustPayload(run),
		}); createError != nil {
			return nil, createError
		}
		return nil, err
	}
	pending, waiting := runFromChat(run, response)
	record := &models.ResponseRecord{
		ID:       run.ID,
		Kind:     models.ResponseRecordRun,
		Model:    model,
		Status:   run.Status,
		ParentID: threadID,
		RefID:    request.AssistantID,
		Payload:  mustPayload(run),
	}
	if waiting {
		turns := append(append([]models.ChatMessage{}, chatRequest.Messages...), pending...)
		record.Input = mustPayload(turns)
	}
	if err := s.responseService.Create(record); err != nil {
		return nil, err
	}
	if !waiting && len(response.Choices) > 0 && response.Choices[0].Message.Content != "" {
		_, _ = s.CreateThreadMessage(ctx, threadID, &models.ThreadMessageRequest{
			Role:    "assistant",
			Content: json.RawMessage(quoteJSONString(response.Choices[0].Message.Content)),
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
	record, err := s.responseService.Get(runID, models.ResponseRecordRun)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("run", runID)
		}
		return nil, err
	}
	if record.ParentID != threadID {
		return nil, compatNotFound("run", runID)
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return nil, deny
	}
	var output models.Run
	if err := decodePayload(record, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

// ListRuns returns all runs of a thread, oldest first.
func (s *Service) ListRuns(
	_ context.Context,
	threadID string,
) ([]*models.Run, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	if _, err := s.responseService.Get(threadID, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("thread", threadID)
		}
		return nil, err
	}
	records, err := s.responseService.List([]models.ResponseRecordKind{models.ResponseRecordRun}, threadID)
	if err != nil {
		return nil, err
	}
	output := make([]*models.Run, 0, len(records))
	for _, record := range records {
		var run models.Run
		if err := decodePayload(record, &run); err != nil {
			return nil, err
		}
		output = append(output, &run)
	}
	return output, nil
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
	record, err := s.responseService.Get(runID, models.ResponseRecordRun)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("run", runID)
		}
		return nil, err
	}
	if record.ParentID != threadID {
		return nil, compatNotFound("run", runID)
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return nil, deny
	}
	var output models.Run
	if err := decodePayload(record, &output); err != nil {
		return nil, err
	}
	switch output.Status {
	case "completed", "failed", "canceled":
		return nil, invalidRequest(fmt.Sprintf("run %q is already %s", runID, output.Status))
	}
	output.Status = "canceled"
	output.RequiredAction = nil
	record.Status = "canceled"
	record.Payload = mustPayload(&output)
	if err := s.responseService.Update(record); err != nil {
		return nil, err
	}
	return &output, nil
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
	record, err := s.responseService.Get(runID, models.ResponseRecordRun)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("run", runID)
		}
		return nil, err
	}
	if record.ParentID != threadID {
		return nil, compatNotFound("run", runID)
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return nil, deny
	}
	var output models.Run
	if err := decodePayload(record, &output); err != nil {
		return nil, err
	}
	if output.Status != "requires_action" {
		return nil, invalidRequest(fmt.Sprintf("run %q is %s, not requires_action", runID, output.Status))
	}
	var turns []models.ChatMessage
	if len(record.Input) > 0 {
		if err := json.Unmarshal(record.Input, &turns); err != nil {
			return nil, invalidRequest("stored run turns are corrupt")
		}
	}
	byID := map[string]string{}
	for _, toolOutput := range outputs {
		byID[toolOutput.ToolCallID] = toolOutput.Output
	}
	if output.RequiredAction == nil || output.RequiredAction.SubmitToolOutputs == nil {
		return nil, invalidRequest("run has no pending tool calls")
	}
	for _, toolCall := range output.RequiredAction.SubmitToolOutputs.ToolCalls {
		text, ok := byID[toolCall.ID]
		if !ok {
			return nil, invalidRequest(fmt.Sprintf("missing output for tool call %q", toolCall.ID))
		}
		turns = append(turns, models.ChatMessage{Role: "tool", ToolCallID: toolCall.ID, Content: text})
	}
	chatRequest := &models.ChatCompletionRequest{Model: record.Model, Messages: turns}
	response, err := s.complete(ctx, chatRequest, token, false)
	if err != nil {
		return nil, err
	}
	pending, waiting := runFromChat(&output, response)
	if !waiting {
		output.RequiredAction = nil
	}
	record.Status = output.Status
	record.Payload = mustPayload(&output)
	if waiting {
		record.Input = mustPayload(append(turns, pending...))
	} else {
		record.Input = mustPayload(turns)
	}
	if err := s.responseService.Update(record); err != nil {
		return nil, err
	}
	if !waiting && len(response.Choices) > 0 && response.Choices[0].Message.Content != "" {
		_, _ = s.CreateThreadMessage(ctx, threadID, &models.ThreadMessageRequest{
			Role:    "assistant",
			Content: json.RawMessage(quoteJSONString(response.Choices[0].Message.Content)),
		})
	}
	return &output, nil
}
