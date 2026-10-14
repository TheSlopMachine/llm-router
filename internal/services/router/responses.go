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
		err := errors.New("responses store is not wired")
		s.logError("Rejecting request without response store",
			"operation", "compat_record", "reason", "store_not_wired", "error", err)
		return err
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
	return models.ResponseObjectFromChat(id, request, response)
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
		if _, err := s.responseService.Get(ctx, request.PreviousResponseID, models.ResponseRecordResponse); err != nil {
			if errors.Is(err, apierrors.ErrNotFound) {
				s.debug("Loading stored response misses",
					"operation", "create_response", "detail", request.PreviousResponseID,
					"model", request.Model.String(), "token_id", tokenIdentifier(token), "error", err)
				return nil, compatNotFound("response", request.PreviousResponseID)
			}
			s.logError("Loading stored response fails",
				"operation", "create_response", "detail", request.PreviousResponseID,
				"model", request.Model.String(), "token_id", tokenIdentifier(token), "error", err)
			return nil, err
		}
	}
	response, err := s.complete(ctx, chatRequest, token, false)
	if err != nil {
		return nil, err
	}
	output := responseFromChat(models.NewResponseID(), request, response)
	if request.Store == nil || *request.Store {
		if err := s.responseService.Create(ctx, &models.ResponseRecord{
			ID:      output.ID,
			Kind:    models.ResponseRecordResponse,
			Model:   request.Model,
			Status:  output.Status,
			RefID:   request.PreviousResponseID,
			Input:   request.Input,
			Payload: mustPayload(output),
		}); err != nil {
			s.logError("Persisting response fails",
				"operation", "create_response", "detail", output.ID,
				"model", request.Model.String(), "token_id", tokenIdentifier(token), "error", err)
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
	record, err := s.responseService.Get(ctx, id, models.ResponseRecordResponse)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored response misses",
				"operation", "get_response", "detail", id,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("response", id)
		}
		s.logError("Loading stored response fails",
			"operation", "get_response", "detail", id,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
		return nil, deny
	}
	var output models.ResponseObject
	if err := decodePayload(record, &output); err != nil {
		s.logError("Decoding stored response fails",
			"operation", "get_response", "detail", id,
			"model", record.Model.String(), "error", err)
		return nil, err
	}
	return &output, nil
}

// CancelResponse marks a non-terminal response canceled. Synchronously
// executed responses are terminal, so cancel succeeds only on rows still
// open (e.g. requires_action runs surfaced as responses).
func (s *Service) CancelResponse(
	ctx context.Context,
	id string,
	token *models.RouterToken,
) (*models.ResponseObject, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	record, err := s.responseService.Get(ctx, id, models.ResponseRecordResponse)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored response misses",
				"operation", "cancel_response", "detail", id,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("response", id)
		}
		s.logError("Loading stored response fails",
			"operation", "cancel_response", "detail", id,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
		return nil, deny
	}
	if record.Status == "completed" || record.Status == "failed" || record.Status == "canceled" {
		return nil, invalidRequest(fmt.Sprintf("response %q is already %s", id, record.Status))
	}
	var output models.ResponseObject
	if err := decodePayload(record, &output); err != nil {
		s.logError("Decoding stored response fails",
			"operation", "cancel_response", "detail", id,
			"model", record.Model.String(), "error", err)
		return nil, err
	}
	output.Status = "canceled"
	record.Status = "canceled"
	record.Payload = mustPayload(&output)
	if err := s.responseService.Update(ctx, record); err != nil {
		s.logError("Updating stored response fails",
			"operation", "cancel_response", "detail", id,
			"model", record.Model.String(), "error", err)
		return nil, err
	}
	return &output, nil
}

// ListResponseInputItems returns the stored input of a response as items: a
// stored string becomes one message item, stored items return as-is.
func (s *Service) ListResponseInputItems(
	ctx context.Context,
	id string,
	token *models.RouterToken,
) ([]models.ResponseInputItem, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	record, err := s.responseService.Get(ctx, id, models.ResponseRecordResponse)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored response misses",
				"operation", "list_response_input_items", "detail", id,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("response", id)
		}
		s.logError("Loading stored response fails",
			"operation", "list_response_input_items", "detail", id,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
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
	ctx context.Context,
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
	if err := s.responseService.Create(ctx, &models.ResponseRecord{
		ID:      output.ID,
		Kind:    models.ResponseRecordConversation,
		Model:   model,
		Status:  "active",
		Input:   mustPayload(items),
		Payload: mustPayload(output),
	}); err != nil {
		s.logError("Persisting conversation fails",
			"operation", "create_conversation", "detail", output.ID,
			"model", model.String(), "error", err)
		return nil, err
	}
	return output, nil
}

// GetConversation returns one stored conversation.
func (s *Service) GetConversation(
	ctx context.Context,
	id string,
	token *models.RouterToken,
) (*models.ConversationObject, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	record, err := s.responseService.Get(ctx, id, models.ResponseRecordConversation)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored conversation misses",
				"operation", "get_conversation", "detail", id,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("conversation", id)
		}
		s.logError("Loading stored conversation fails",
			"operation", "get_conversation", "detail", id,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
		return nil, deny
	}
	var output models.ConversationObject
	if err := decodePayload(record, &output); err != nil {
		s.logError("Decoding stored conversation fails",
			"operation", "get_conversation", "detail", id,
			"model", record.Model.String(), "error", err)
		return nil, err
	}
	return &output, nil
}

// DeleteConversation removes a conversation container. Items stored as
// responses with the conversation id stay addressable by response id.
func (s *Service) DeleteConversation(ctx context.Context, id string) error {
	if err := s.mustResponseStore(); err != nil {
		return err
	}
	if _, err := s.responseService.Get(ctx, id, models.ResponseRecordConversation); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored conversation misses",
				"operation", "delete_conversation", "detail", id, "error", err)
			return compatNotFound("conversation", id)
		}
		s.logError("Loading stored conversation fails",
			"operation", "delete_conversation", "detail", id, "error", err)
		return err
	}
	if err := s.responseService.Delete(ctx, id); err != nil {
		s.logError("Deleting stored conversation fails",
			"operation", "delete_conversation", "detail", id, "error", err)
		return err
	}
	return nil
}

// ListConversationItems returns the items appended to a conversation.
func (s *Service) ListConversationItems(
	ctx context.Context,
	id string,
	token *models.RouterToken,
) ([]models.ResponseInputItem, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	record, err := s.responseService.Get(ctx, id, models.ResponseRecordConversation)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored conversation misses",
				"operation", "list_conversation_items", "detail", id,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("conversation", id)
		}
		s.logError("Loading stored conversation fails",
			"operation", "list_conversation_items", "detail", id,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
		return nil, deny
	}
	return responseInputItems(record.Input), nil
}

// AppendConversationItems appends items to a conversation.
func (s *Service) AppendConversationItems(
	ctx context.Context,
	id string,
	token *models.RouterToken,
	items []models.ResponseInputItem,
) ([]models.ResponseInputItem, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	record, err := s.responseService.Get(ctx, id, models.ResponseRecordConversation)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored conversation misses",
				"operation", "append_conversation_items", "detail", id,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("conversation", id)
		}
		s.logError("Loading stored conversation fails",
			"operation", "append_conversation_items", "detail", id,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
		return nil, deny
	}
	current := responseInputItems(record.Input)
	current = append(current, items...)
	record.Input = mustPayload(current)
	if err := s.responseService.Update(ctx, record); err != nil {
		s.logError("Updating stored conversation fails",
			"operation", "append_conversation_items", "detail", id,
			"model", record.Model.String(), "error", err)
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
	ctx context.Context,
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
	if err := s.responseService.Create(ctx, &models.ResponseRecord{
		ID:      output.ID,
		Kind:    models.ResponseRecordAssistant,
		Model:   request.Model,
		Status:  "active",
		Payload: mustPayload(output),
	}); err != nil {
		s.logError("Persisting assistant fails",
			"operation", "create_assistant", "detail", output.ID,
			"model", request.Model.String(), "error", err)
		return nil, err
	}
	return output, nil
}

// GetAssistant returns one stored assistant.
func (s *Service) GetAssistant(
	ctx context.Context,
	id string,
	token *models.RouterToken,
) (*models.Assistant, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	record, err := s.responseService.Get(ctx, id, models.ResponseRecordAssistant)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored assistant misses",
				"operation", "get_assistant", "detail", id,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("assistant", id)
		}
		s.logError("Loading stored assistant fails",
			"operation", "get_assistant", "detail", id,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
		return nil, deny
	}
	var output models.Assistant
	if err := decodePayload(record, &output); err != nil {
		s.logError("Decoding stored assistant fails",
			"operation", "get_assistant", "detail", id,
			"model", record.Model.String(), "error", err)
		return nil, err
	}
	return &output, nil
}

// UpdateAssistant patches name, description, instructions, tools,
// response_format and metadata. Model changes re-parse the id.
func (s *Service) UpdateAssistant(
	ctx context.Context,
	id string,
	token *models.RouterToken,
	request *models.AssistantRequest,
) (*models.Assistant, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	record, err := s.responseService.Get(ctx, id, models.ResponseRecordAssistant)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored assistant misses",
				"operation", "update_assistant", "detail", id,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("assistant", id)
		}
		s.logError("Loading stored assistant fails",
			"operation", "update_assistant", "detail", id,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
		return nil, deny
	}
	var output models.Assistant
	if err := decodePayload(record, &output); err != nil {
		s.logError("Decoding stored assistant fails",
			"operation", "update_assistant", "detail", id,
			"model", record.Model.String(), "error", err)
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
	if err := s.responseService.Update(ctx, record); err != nil {
		s.logError("Updating stored assistant fails",
			"operation", "update_assistant", "detail", id,
			"model", record.Model.String(), "error", err)
		return nil, err
	}
	return &output, nil
}

// DeleteAssistant removes an assistant preset. Runs referencing it stay
// addressable by run id.
func (s *Service) DeleteAssistant(ctx context.Context, id string) error {
	if err := s.mustResponseStore(); err != nil {
		return err
	}
	if _, err := s.responseService.Get(ctx, id, models.ResponseRecordAssistant); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored assistant misses",
				"operation", "delete_assistant", "detail", id, "error", err)
			return compatNotFound("assistant", id)
		}
		s.logError("Loading stored assistant fails",
			"operation", "delete_assistant", "detail", id, "error", err)
		return err
	}
	if err := s.responseService.Delete(ctx, id); err != nil {
		s.logError("Deleting stored assistant fails",
			"operation", "delete_assistant", "detail", id, "error", err)
		return err
	}
	return nil
}

// ListAssistants returns all stored assistants, oldest first.
func (s *Service) ListAssistants(ctx context.Context) ([]*models.Assistant, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	records, err := s.responseService.List(ctx, []models.ResponseRecordKind{models.ResponseRecordAssistant}, "")
	if err != nil {
		s.logError("Listing stored assistants fails",
			"operation", "list_assistants", "error", err)
		return nil, err
	}
	output := make([]*models.Assistant, 0, len(records))
	for _, record := range records {
		var assistant models.Assistant
		if err := decodePayload(record, &assistant); err != nil {
			s.logError("Decoding stored assistant fails",
				"operation", "list_assistants", "detail", record.ID,
				"model", record.Model.String(), "error", err)
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
	ctx context.Context,
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
	if err := s.responseService.Create(ctx, &models.ResponseRecord{
		ID:      output.ID,
		Kind:    models.ResponseRecordThread,
		Status:  "active",
		Payload: mustPayload(output),
	}); err != nil {
		s.logError("Persisting thread fails",
			"operation", "create_thread", "detail", output.ID, "error", err)
		return nil, err
	}
	return output, nil
}

// GetThread returns one stored thread.
func (s *Service) GetThread(
	ctx context.Context,
	id string,
	token *models.RouterToken,
) (*models.Thread, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	record, err := s.responseService.Get(ctx, id, models.ResponseRecordThread)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored thread misses",
				"operation", "get_thread", "detail", id,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("thread", id)
		}
		s.logError("Loading stored thread fails",
			"operation", "get_thread", "detail", id,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
		return nil, deny
	}
	var output models.Thread
	if err := decodePayload(record, &output); err != nil {
		s.logError("Decoding stored thread fails",
			"operation", "get_thread", "detail", id, "error", err)
		return nil, err
	}
	return &output, nil
}

// UpdateThread patches thread metadata.
func (s *Service) UpdateThread(
	ctx context.Context,
	id string,
	metadata map[string]string,
) (*models.Thread, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	record, err := s.responseService.Get(ctx, id, models.ResponseRecordThread)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored thread misses",
				"operation", "update_thread", "detail", id, "error", err)
			return nil, compatNotFound("thread", id)
		}
		s.logError("Loading stored thread fails",
			"operation", "update_thread", "detail", id, "error", err)
		return nil, err
	}
	var output models.Thread
	if err := decodePayload(record, &output); err != nil {
		s.logError("Decoding stored thread fails",
			"operation", "update_thread", "detail", id, "error", err)
		return nil, err
	}
	if metadata != nil {
		output.Metadata = metadata
	}
	record.Payload = mustPayload(&output)
	if err := s.responseService.Update(ctx, record); err != nil {
		s.logError("Updating stored thread fails",
			"operation", "update_thread", "detail", id, "error", err)
		return nil, err
	}
	return &output, nil
}

// DeleteThread removes a thread handle. Messages and runs referencing it
// stay addressable by id.
func (s *Service) DeleteThread(ctx context.Context, id string) error {
	if err := s.mustResponseStore(); err != nil {
		return err
	}
	if _, err := s.responseService.Get(ctx, id, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored thread misses",
				"operation", "delete_thread", "detail", id, "error", err)
			return compatNotFound("thread", id)
		}
		s.logError("Loading stored thread fails",
			"operation", "delete_thread", "detail", id, "error", err)
		return err
	}
	if err := s.responseService.Delete(ctx, id); err != nil {
		s.logError("Deleting stored thread fails",
			"operation", "delete_thread", "detail", id, "error", err)
		return err
	}
	return nil
}

// CreateThreadMessage appends a user or assistant message to a thread.
func (s *Service) CreateThreadMessage(
	ctx context.Context,
	threadID string,
	request *models.ThreadMessageRequest,
) (*models.ThreadMessage, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	if _, err := s.responseService.Get(ctx, threadID, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored thread misses",
				"operation", "create_thread_message", "detail", threadID, "error", err)
			return nil, compatNotFound("thread", threadID)
		}
		s.logError("Loading stored thread fails",
			"operation", "create_thread_message", "detail", threadID, "error", err)
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
	if err := s.responseService.Create(ctx, &models.ResponseRecord{
		ID:       output.ID,
		Kind:     models.ResponseRecordThreadMessage,
		Status:   "completed",
		ParentID: threadID,
		Payload:  mustPayload(output),
	}); err != nil {
		s.logError("Persisting thread message fails",
			"operation", "create_thread_message", "detail", output.ID, "error", err)
		return nil, err
	}
	return output, nil
}

// GetThreadMessage returns one thread message, asserting thread membership.
func (s *Service) GetThreadMessage(
	ctx context.Context,
	threadID, messageID string,
) (*models.ThreadMessage, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	record, err := s.responseService.Get(ctx, messageID, models.ResponseRecordThreadMessage)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored thread message misses",
				"operation", "get_thread_message", "detail", messageID, "error", err)
			return nil, compatNotFound("message", messageID)
		}
		s.logError("Loading stored thread message fails",
			"operation", "get_thread_message", "detail", messageID, "error", err)
		return nil, err
	}
	if record.ParentID != threadID {
		s.debug("Loading stored thread message misses",
			"operation", "get_thread_message", "detail", messageID,
			"reason", "thread_mismatch")
		return nil, compatNotFound("message", messageID)
	}
	var output models.ThreadMessage
	if err := decodePayload(record, &output); err != nil {
		s.logError("Decoding stored thread message fails",
			"operation", "get_thread_message", "detail", messageID, "error", err)
		return nil, err
	}
	return &output, nil
}

// ListThreadMessages returns all messages of a thread, oldest first.
func (s *Service) ListThreadMessages(
	ctx context.Context,
	threadID string,
) ([]*models.ThreadMessage, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	if _, err := s.responseService.Get(ctx, threadID, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored thread misses",
				"operation", "list_thread_messages", "detail", threadID, "error", err)
			return nil, compatNotFound("thread", threadID)
		}
		s.logError("Loading stored thread fails",
			"operation", "list_thread_messages", "detail", threadID, "error", err)
		return nil, err
	}
	records, err := s.responseService.List(ctx, []models.ResponseRecordKind{models.ResponseRecordThreadMessage}, threadID)
	if err != nil {
		s.logError("Listing stored thread messages fails",
			"operation", "list_thread_messages", "detail", threadID, "error", err)
		return nil, err
	}
	output := make([]*models.ThreadMessage, 0, len(records))
	for _, record := range records {
		var message models.ThreadMessage
		if err := decodePayload(record, &message); err != nil {
			s.logError("Decoding stored thread message fails",
				"operation", "list_thread_messages", "detail", record.ID, "error", err)
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
	if _, err := s.responseService.Get(ctx, threadID, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored thread misses",
				"operation", "create_run", "detail", threadID,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("thread", threadID)
		}
		s.logError("Loading stored thread fails",
			"operation", "create_run", "detail", threadID,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if request.AssistantID == "" {
		return nil, invalidRequest("assistant_id is required")
	}
	assistantRecord, err := s.responseService.Get(ctx, request.AssistantID, models.ResponseRecordAssistant)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored assistant misses",
				"operation", "create_run", "detail", request.AssistantID,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("assistant", request.AssistantID)
		}
		s.logError("Loading stored assistant fails",
			"operation", "create_run", "detail", request.AssistantID,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	var assistant models.Assistant
	if err := decodePayload(assistantRecord, &assistant); err != nil {
		s.logError("Decoding stored assistant fails",
			"operation", "create_run", "detail", request.AssistantID,
			"token_id", tokenIdentifier(token), "error", err)
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
		if createError := s.responseService.Create(ctx, &models.ResponseRecord{
			ID:       run.ID,
			Kind:     models.ResponseRecordRun,
			Model:    model,
			Status:   run.Status,
			ParentID: threadID,
			RefID:    request.AssistantID,
			Payload:  mustPayload(run),
		}); createError != nil {
			s.logError("Persisting failed run fails",
				"operation", "create_run", "detail", run.ID,
				"model", model.String(), "token_id", tokenIdentifier(token),
				"reason", err.Error(), "error", createError)
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
	if err := s.responseService.Create(ctx, record); err != nil {
		s.logError("Persisting run fails",
			"operation", "create_run", "detail", run.ID,
			"model", model.String(), "token_id", tokenIdentifier(token), "error", err)
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
	ctx context.Context,
	threadID, runID string,
	token *models.RouterToken,
) (*models.Run, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	record, err := s.responseService.Get(ctx, runID, models.ResponseRecordRun)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored run misses",
				"operation", "get_run", "detail", runID,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("run", runID)
		}
		s.logError("Loading stored run fails",
			"operation", "get_run", "detail", runID,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if record.ParentID != threadID {
		s.debug("Loading stored run misses",
			"operation", "get_run", "detail", runID, "reason", "thread_mismatch")
		return nil, compatNotFound("run", runID)
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
		return nil, deny
	}
	var output models.Run
	if err := decodePayload(record, &output); err != nil {
		s.logError("Decoding stored run fails",
			"operation", "get_run", "detail", runID,
			"model", record.Model.String(), "error", err)
		return nil, err
	}
	return &output, nil
}

// ListRuns returns all runs of a thread, oldest first.
func (s *Service) ListRuns(
	ctx context.Context,
	threadID string,
) ([]*models.Run, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	if _, err := s.responseService.Get(ctx, threadID, models.ResponseRecordThread); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored thread misses",
				"operation", "list_runs", "detail", threadID, "error", err)
			return nil, compatNotFound("thread", threadID)
		}
		s.logError("Loading stored thread fails",
			"operation", "list_runs", "detail", threadID, "error", err)
		return nil, err
	}
	records, err := s.responseService.List(ctx, []models.ResponseRecordKind{models.ResponseRecordRun}, threadID)
	if err != nil {
		s.logError("Listing stored runs fails",
			"operation", "list_runs", "detail", threadID, "error", err)
		return nil, err
	}
	output := make([]*models.Run, 0, len(records))
	for _, record := range records {
		var run models.Run
		if err := decodePayload(record, &run); err != nil {
			s.logError("Decoding stored run fails",
				"operation", "list_runs", "detail", record.ID,
				"model", record.Model.String(), "error", err)
			return nil, err
		}
		output = append(output, &run)
	}
	return output, nil
}

// CancelRun marks a non-terminal run canceled.
func (s *Service) CancelRun(
	ctx context.Context,
	threadID, runID string,
	token *models.RouterToken,
) (*models.Run, error) {
	if err := s.mustResponseStore(); err != nil {
		return nil, err
	}
	record, err := s.responseService.Get(ctx, runID, models.ResponseRecordRun)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored run misses",
				"operation", "cancel_run", "detail", runID,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("run", runID)
		}
		s.logError("Loading stored run fails",
			"operation", "cancel_run", "detail", runID,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if record.ParentID != threadID {
		s.debug("Loading stored run misses",
			"operation", "cancel_run", "detail", runID, "reason", "thread_mismatch")
		return nil, compatNotFound("run", runID)
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
		return nil, deny
	}
	var output models.Run
	if err := decodePayload(record, &output); err != nil {
		s.logError("Decoding stored run fails",
			"operation", "cancel_run", "detail", runID,
			"model", record.Model.String(), "error", err)
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
	if err := s.responseService.Update(ctx, record); err != nil {
		s.logError("Updating stored run fails",
			"operation", "cancel_run", "detail", runID,
			"model", record.Model.String(), "error", err)
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
	record, err := s.responseService.Get(ctx, runID, models.ResponseRecordRun)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored run misses",
				"operation", "submit_tool_outputs", "detail", runID,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("run", runID)
		}
		s.logError("Loading stored run fails",
			"operation", "submit_tool_outputs", "detail", runID,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if record.ParentID != threadID {
		s.debug("Loading stored run misses",
			"operation", "submit_tool_outputs", "detail", runID, "reason", "thread_mismatch")
		return nil, compatNotFound("run", runID)
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
		return nil, deny
	}
	var output models.Run
	if err := decodePayload(record, &output); err != nil {
		s.logError("Decoding stored run fails",
			"operation", "submit_tool_outputs", "detail", runID,
			"model", record.Model.String(), "error", err)
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
	if err := s.responseService.Update(ctx, record); err != nil {
		s.logError("Updating stored run fails",
			"operation", "submit_tool_outputs", "detail", runID,
			"model", record.Model.String(), "token_id", tokenIdentifier(token), "error", err)
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
