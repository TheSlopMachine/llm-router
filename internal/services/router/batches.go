package router

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/batches"
)

// batchResult builds one JSONL-ready result line: succeeded carries the
// Anthropic message, errored carries the negotiated error shape.
func batchResult(customID string, message *models.AnthropicMessage) models.BatchResultLine {
	return models.BatchResultLine{
		CustomID: customID,
		Result: mustPayload(map[string]any{
			"type":    "succeeded",
			"message": message,
		}),
	}
}

func batchErrorResult(customID string, err error) models.BatchResultLine {
	re := apierrors.ToAPIError(err)
	body := map[string]any{"type": apierrors.AnthropicErrorType(re.Code), "message": err.Error()}
	if body["type"] == "api_error" {
		body["message"] = re.Code + ": " + err.Error()
	}
	return models.BatchResultLine{
		CustomID: customID,
		Result: mustPayload(map[string]any{
			"type":  "errored",
			"error": body,
		}),
	}
}

// mustBatchStore fails closed when the batch store stays unwired.
func (s *Service) mustBatchStore(operation string) error {
	if s.batchService == nil {
		err := errors.New("message batch store is not wired")
		s.logError("Rejecting request without batch store",
			"operation", operation, "reason", "store_not_wired", "error", err)
		return err
	}
	return nil
}

// CreateBatch validates the entries, executes each through the chat
// pipeline in order, and persists the ended batch. Execution is
// synchronous: the batch returns ended (or partially errored lines), never
// a deferred worker. Custom IDs must be unique within the batch.
func (s *Service) CreateBatch(
	ctx context.Context,
	request *models.BatchCreateRequest,
	token *models.RouterToken,
) (*models.BatchInfo, error) {
	if err := s.mustBatchStore("create_batch"); err != nil {
		return nil, err
	}
	if len(request.Requests) == 0 {
		return nil, invalidRequest("requests must not be empty")
	}
	seen := map[string]bool{}
	type parsed struct {
		entry   models.BatchEntry
		message *models.AnthropicMessageRequest
	}
	items := make([]parsed, 0, len(request.Requests))
	for _, entry := range request.Requests {
		if entry.CustomID == "" {
			return nil, invalidRequest("requests entry is missing custom_id")
		}
		if seen[entry.CustomID] {
			return nil, invalidRequest("duplicate custom_id in requests")
		}
		seen[entry.CustomID] = true
		var message models.AnthropicMessageRequest
		if err := json.Unmarshal(entry.Params, &message); err != nil {
			return nil, invalidRequest("requests entry params are not a message request")
		}
		if message.Model == "" {
			return nil, invalidRequest("requests entry is missing params.model")
		}
		if message.MaxTokens <= 0 {
			return nil, invalidRequest("requests entry is missing params.max_tokens")
		}
		if len(message.Messages) == 0 {
			return nil, invalidRequest("requests entry is missing params.messages")
		}
		items = append(items, parsed{entry: entry, message: &message})
	}
	record := &models.BatchRecord{
		ID:      models.NewBatchID(),
		Model:   models.ModelId(items[0].message.Model),
		Status:  batches.StatusInProgress,
		Entries: request.Requests,
	}
	for _, item := range items {
		chatRequest, err := item.message.ToChat()
		if err != nil {
			record.Results = append(record.Results, batchErrorResult(item.entry.CustomID, err))
			continue
		}
		response, err := s.complete(ctx, chatRequest, token, false)
		if err != nil {
			record.Results = append(record.Results, batchErrorResult(item.entry.CustomID, err))
			continue
		}
		record.Results = append(record.Results, batchResult(item.entry.CustomID, models.AnthropicMessageFromChat(response, item.message.Model)))
	}
	now := time.Now()
	record.Status = batches.StatusEnded
	record.EndedAt = &now
	if err := s.batchService.Create(ctx, record); err != nil {
		s.logError("Persisting message batch fails",
			"operation", "create_batch", "detail", record.ID,
			"model", record.Model.String(), "count", len(record.Results),
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	return record.ToBatchInfo("/v1/messages/batches/" + record.ID + "/results"), nil
}

// GetBatch returns one stored batch.
func (s *Service) GetBatch(
	ctx context.Context,
	id string,
	token *models.RouterToken,
) (*models.BatchInfo, error) {
	if err := s.mustBatchStore("get_batch"); err != nil {
		return nil, err
	}
	record, err := s.batchService.Get(ctx, id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored message batch misses",
				"operation", "get_batch", "detail", id,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("message batch", id)
		}
		s.logError("Loading stored message batch fails",
			"operation", "get_batch", "detail", id,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
		return nil, deny
	}
	return record.ToBatchInfo("/v1/messages/batches/" + record.ID + "/results"), nil
}

// ListBatches returns all stored batches, oldest first.
func (s *Service) ListBatches(ctx context.Context) ([]*models.BatchInfo, error) {
	if err := s.mustBatchStore("list_batches"); err != nil {
		return nil, err
	}
	records, err := s.batchService.List(ctx)
	if err != nil {
		s.logError("Listing stored message batches fails",
			"operation", "list_batches", "error", err)
		return nil, err
	}
	output := make([]*models.BatchInfo, 0, len(records))
	for _, record := range records {
		output = append(output, record.ToBatchInfo("/v1/messages/batches/"+record.ID+"/results"))
	}
	return output, nil
}

// CancelBatch marks a non-terminal batch canceled. Synchronously executed
// batches are terminal, so cancel succeeds only on rows still open.
func (s *Service) CancelBatch(
	ctx context.Context,
	id string,
	token *models.RouterToken,
) (*models.BatchInfo, error) {
	if err := s.mustBatchStore("cancel_batch"); err != nil {
		return nil, err
	}
	record, err := s.batchService.Get(ctx, id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored message batch misses",
				"operation", "cancel_batch", "detail", id,
				"token_id", tokenIdentifier(token), "error", err)
			return nil, compatNotFound("message batch", id)
		}
		s.logError("Loading stored message batch fails",
			"operation", "cancel_batch", "detail", id,
			"token_id", tokenIdentifier(token), "error", err)
		return nil, err
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
		return nil, deny
	}
	if record.Status == batches.StatusEnded || record.Status == batches.StatusCanceled {
		return nil, invalidRequest("message batch is already " + record.Status)
	}
	now := time.Now()
	record.Status = batches.StatusCanceled
	record.EndedAt = &now
	if err := s.batchService.Update(ctx, record); err != nil {
		s.logError("Updating stored message batch fails",
			"operation", "cancel_batch", "detail", id,
			"model", record.Model.String(), "error", err)
		return nil, err
	}
	return record.ToBatchInfo("/v1/messages/batches/" + record.ID + "/results"), nil
}

// DeleteBatch removes a batch row. Missing rows are not errors only when
// the row never existed? No: missing rows are 404, matching the catalog.
func (s *Service) DeleteBatch(ctx context.Context, id string) error {
	if err := s.mustBatchStore("delete_batch"); err != nil {
		return err
	}
	if _, err := s.batchService.Get(ctx, id); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored message batch misses",
				"operation", "delete_batch", "detail", id, "error", err)
			return compatNotFound("message batch", id)
		}
		s.logError("Loading stored message batch fails",
			"operation", "delete_batch", "detail", id, "error", err)
		return err
	}
	if err := s.batchService.Delete(ctx, id); err != nil {
		s.logError("Deleting stored message batch fails",
			"operation", "delete_batch", "detail", id, "error", err)
		return err
	}
	return nil
}

// GetBatchResults renders stored results as JSONL.
func (s *Service) GetBatchResults(
	ctx context.Context,
	id string,
	token *models.RouterToken,
) (string, error) {
	if err := s.mustBatchStore("get_batch_results"); err != nil {
		return "", err
	}
	record, err := s.batchService.Get(ctx, id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			s.debug("Loading stored message batch misses",
				"operation", "get_batch_results", "detail", id,
				"token_id", tokenIdentifier(token), "error", err)
			return "", compatNotFound("message batch", id)
		}
		s.logError("Loading stored message batch fails",
			"operation", "get_batch_results", "detail", id,
			"token_id", tokenIdentifier(token), "error", err)
		return "", err
	}
	if deny := s.tokenDenies(token, record.Model, ""); deny != nil {
		return "", deny
	}
	return record.BatchResults(), nil
}
