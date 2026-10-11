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

// CreateBatch validates the entries, executes each through the chat
// pipeline in order, and persists the ended batch. Execution is
// synchronous: the batch returns ended (or partially errored lines), never
// a deferred worker. Custom IDs must be unique within the batch.
func (s *Service) CreateBatch(
	ctx context.Context,
	request *models.BatchCreateRequest,
	token *models.RouterToken,
) (*models.BatchInfo, error) {
	if s.batchService == nil {
		return nil, errors.New("message batch store is not wired")
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
	if s.batchService == nil {
		return nil, errors.New("message batch store is not wired")
	}
	record, err := s.batchService.Get(ctx, id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("message batch", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return nil, deny
	}
	return record.ToBatchInfo("/v1/messages/batches/" + record.ID + "/results"), nil
}

// ListBatches returns all stored batches, oldest first.
func (s *Service) ListBatches(ctx context.Context) ([]*models.BatchInfo, error) {
	if s.batchService == nil {
		return nil, errors.New("message batch store is not wired")
	}
	records, err := s.batchService.List(ctx)
	if err != nil {
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
	if s.batchService == nil {
		return nil, errors.New("message batch store is not wired")
	}
	record, err := s.batchService.Get(ctx, id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("message batch", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return nil, deny
	}
	if record.Status == batches.StatusEnded || record.Status == batches.StatusCanceled {
		return nil, invalidRequest("message batch is already " + record.Status)
	}
	now := time.Now()
	record.Status = batches.StatusCanceled
	record.EndedAt = &now
	if err := s.batchService.Update(ctx, record); err != nil {
		return nil, err
	}
	return record.ToBatchInfo("/v1/messages/batches/" + record.ID + "/results"), nil
}

// DeleteBatch removes a batch row. Missing rows are not errors only when
// the row never existed? No: missing rows are 404, matching the catalog.
func (s *Service) DeleteBatch(ctx context.Context, id string) error {
	if s.batchService == nil {
		return errors.New("message batch store is not wired")
	}
	if _, err := s.batchService.Get(ctx, id); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return compatNotFound("message batch", id)
		}
		return err
	}
	return s.batchService.Delete(ctx, id)
}

// GetBatchResults renders stored results as JSONL.
func (s *Service) GetBatchResults(
	ctx context.Context,
	id string,
	token *models.RouterToken,
) (string, error) {
	if s.batchService == nil {
		return "", errors.New("message batch store is not wired")
	}
	record, err := s.batchService.Get(ctx, id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return "", compatNotFound("message batch", id)
		}
		return "", err
	}
	if deny := tokenDenies(token, record.Model); deny != nil {
		return "", deny
	}
	return record.BatchResults(), nil
}
