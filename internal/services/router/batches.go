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
func batchResult(customID string, msg *models.AnthropicMessage) models.BatchResultLine {
	return models.BatchResultLine{
		CustomID: customID,
		Result: mustPayload(map[string]any{
			"type":    "succeeded",
			"message": msg,
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
	req *models.BatchCreateRequest,
	token *models.RouterToken,
) (*models.BatchInfo, error) {
	if s.batchSvc == nil {
		return nil, errors.New("message batch store is not wired")
	}
	if len(req.Requests) == 0 {
		return nil, invalidRequest("requests must not be empty")
	}
	seen := map[string]bool{}
	type parsed struct {
		entry models.BatchEntry
		msg   *models.AnthropicMessageRequest
	}
	items := make([]parsed, 0, len(req.Requests))
	for _, e := range req.Requests {
		if e.CustomID == "" {
			return nil, invalidRequest("requests entry is missing custom_id")
		}
		if seen[e.CustomID] {
			return nil, invalidRequest("duplicate custom_id in requests")
		}
		seen[e.CustomID] = true
		var msg models.AnthropicMessageRequest
		if err := json.Unmarshal(e.Params, &msg); err != nil {
			return nil, invalidRequest("requests entry params are not a message request")
		}
		if msg.Model == "" {
			return nil, invalidRequest("requests entry is missing params.model")
		}
		if msg.MaxTokens <= 0 {
			return nil, invalidRequest("requests entry is missing params.max_tokens")
		}
		if len(msg.Messages) == 0 {
			return nil, invalidRequest("requests entry is missing params.messages")
		}
		items = append(items, parsed{entry: e, msg: &msg})
	}
	rec := &models.BatchRecord{
		ID:      models.NewBatchID(),
		Model:   models.ModelId(items[0].msg.Model),
		Status:  batches.StatusInProgress,
		Entries: req.Requests,
	}
	for _, it := range items {
		chatReq, err := it.msg.ToChat()
		if err != nil {
			rec.Results = append(rec.Results, batchErrorResult(it.entry.CustomID, err))
			continue
		}
		resp, err := s.complete(ctx, chatReq, token, false)
		if err != nil {
			rec.Results = append(rec.Results, batchErrorResult(it.entry.CustomID, err))
			continue
		}
		rec.Results = append(rec.Results, batchResult(it.entry.CustomID, models.AnthropicMessageFromChat(resp, it.msg.Model)))
	}
	now := time.Now()
	rec.Status = batches.StatusEnded
	rec.EndedAt = &now
	if err := s.batchSvc.Create(rec); err != nil {
		return nil, err
	}
	return rec.ToBatchInfo("/v1/messages/batches/" + rec.ID + "/results"), nil
}

// GetBatch returns one stored batch.
func (s *Service) GetBatch(
	_ context.Context,
	id string,
	token *models.RouterToken,
) (*models.BatchInfo, error) {
	if s.batchSvc == nil {
		return nil, errors.New("message batch store is not wired")
	}
	rec, err := s.batchSvc.Get(id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("message batch", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return nil, deny
	}
	return rec.ToBatchInfo("/v1/messages/batches/" + rec.ID + "/results"), nil
}

// ListBatches returns all stored batches, oldest first.
func (s *Service) ListBatches(_ context.Context) ([]*models.BatchInfo, error) {
	if s.batchSvc == nil {
		return nil, errors.New("message batch store is not wired")
	}
	recs, err := s.batchSvc.List()
	if err != nil {
		return nil, err
	}
	out := make([]*models.BatchInfo, 0, len(recs))
	for _, rec := range recs {
		out = append(out, rec.ToBatchInfo("/v1/messages/batches/"+rec.ID+"/results"))
	}
	return out, nil
}

// CancelBatch marks a non-terminal batch canceled. Synchronously executed
// batches are terminal, so cancel succeeds only on rows still open.
func (s *Service) CancelBatch(
	_ context.Context,
	id string,
	token *models.RouterToken,
) (*models.BatchInfo, error) {
	if s.batchSvc == nil {
		return nil, errors.New("message batch store is not wired")
	}
	rec, err := s.batchSvc.Get(id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil, compatNotFound("message batch", id)
		}
		return nil, err
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return nil, deny
	}
	if rec.Status == batches.StatusEnded || rec.Status == batches.StatusCanceled {
		return nil, invalidRequest("message batch is already " + rec.Status)
	}
	now := time.Now()
	rec.Status = batches.StatusCanceled
	rec.EndedAt = &now
	if err := s.batchSvc.Update(rec); err != nil {
		return nil, err
	}
	return rec.ToBatchInfo("/v1/messages/batches/" + rec.ID + "/results"), nil
}

// DeleteBatch removes a batch row. Missing rows are not errors only when
// the row never existed? No: missing rows are 404, matching the catalog.
func (s *Service) DeleteBatch(_ context.Context, id string) error {
	if s.batchSvc == nil {
		return errors.New("message batch store is not wired")
	}
	if _, err := s.batchSvc.Get(id); err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return compatNotFound("message batch", id)
		}
		return err
	}
	return s.batchSvc.Delete(id)
}

// GetBatchResults renders stored results as JSONL.
func (s *Service) GetBatchResults(
	_ context.Context,
	id string,
	token *models.RouterToken,
) (string, error) {
	if s.batchSvc == nil {
		return "", errors.New("message batch store is not wired")
	}
	rec, err := s.batchSvc.Get(id)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return "", compatNotFound("message batch", id)
		}
		return "", err
	}
	if deny := tokenDenies(token, rec.Model); deny != nil {
		return "", deny
	}
	return rec.BatchResults(), nil
}
