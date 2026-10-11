package router

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/batches"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/responses"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func setupCompatRouter(t *testing.T, mock provider.GoAdapter) *Service {
	t.Helper()
	database := testutil.SetupTestDB(t)
	providerService := provider.NewService(database)
	if mock == nil {
		mock = testutil.NewMockAdapter("mock")
	}
	providerService.RegisterGoAdapter(mock)
	if _, err := providerService.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credentialService := credential.New(database, providerService)
	if _, err := credentialService.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "test-key"},
	}); err != nil {
		t.Fatalf("add credential: %v", err)
	}
	modelInfoService := modelinfo.New(database, providerService, credentialService, 1*time.Hour)
	return New(providerService, credentialService, modelInfoService, videojobs.New(database),
		responses.New(database), batches.New(database), slog.Default())
}

func responseRequest(model, input string) *models.ResponseRequest {
	return &models.ResponseRequest{
		Model: models.ModelId(model),
		Input: json.RawMessage(`"` + input + `"`),
	}
}

func TestRouter_CreateGetResponse(t *testing.T) {
	service := setupCompatRouter(t, nil)
	ctx := context.Background()
	output, err := service.CreateResponse(ctx, responseRequest("mock/m", "hi"), nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(output.ID, "resp_") || output.Status != "completed" {
		t.Fatalf("response: %+v", output)
	}
	if len(output.Output) != 1 || output.Output[0].Text != "mock response" {
		t.Fatalf("output: %+v", output.Output)
	}
	if output.Usage == nil || output.Usage.TotalTokens != 15 {
		t.Fatalf("usage: %+v", output.Usage)
	}
	got, err := service.GetResponse(ctx, output.ID, nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != output.ID {
		t.Fatalf("id: %q", got.ID)
	}
	items, err := service.ListResponseInputItems(ctx, output.ID, nil)
	if err != nil || len(items) != 1 {
		t.Fatalf("input items: %+v %v", items, err)
	}
	if _, err := service.GetResponse(ctx, "resp_missing", nil); apierrors.ToAPIError(err).Status != 404 {
		t.Fatalf("missing must 404, got %v", err)
	}
	if _, err := service.CancelResponse(ctx, output.ID, nil); err == nil {
		t.Fatal("cancel of completed must fail")
	}
	compacted, err := service.CompactResponse(ctx, output.ID, nil)
	if err != nil || compacted.ID != output.ID {
		t.Fatalf("compact: %+v %v", compacted, err)
	}
	if n := service.CountResponseInputTokens(responseRequest("mock/m", "hi")); n < 1 {
		t.Fatalf("tokens: %d", n)
	}
}

func TestRouter_AssistantThreadRun(t *testing.T) {
	service := setupCompatRouter(t, nil)
	ctx := context.Background()
	assistant, err := service.CreateAssistant(ctx, &models.AssistantRequest{Model: "mock/m", Instructions: "be nice"})
	if err != nil {
		t.Fatalf("create assistant: %v", err)
	}
	if !strings.HasPrefix(assistant.ID, "asst_") {
		t.Fatalf("assistant: %+v", assistant)
	}
	if _, err := service.CreateAssistant(ctx, &models.AssistantRequest{}); err == nil {
		t.Fatal("assistant without model must fail")
	}
	thread, err := service.CreateThread(ctx, nil)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := service.CreateThreadMessage(ctx, thread.ID, &models.ThreadMessageRequest{Role: "user", Content: json.RawMessage(`"hello"`)}); err != nil {
		t.Fatalf("create message: %v", err)
	}
	if _, err := service.CreateThreadMessage(ctx, thread.ID, &models.ThreadMessageRequest{Role: "system", Content: json.RawMessage(`"x"`)}); err == nil {
		t.Fatal("system role must fail")
	}
	run, err := service.CreateRun(ctx, thread.ID, &models.RunRequest{AssistantID: assistant.ID}, nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("run: %+v", run)
	}
	messages, err := service.ListThreadMessages(ctx, thread.ID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages: %+v %v", messages, err)
	}
	if _, err := service.GetThreadMessage(ctx, thread.ID, messages[0].ID); err != nil {
		t.Fatalf("get message: %v", err)
	}
	if _, err := service.SubmitToolOutputs(ctx, thread.ID, run.ID, nil, []models.ToolOutput{{ToolCallID: "x", Output: "y"}}); err == nil {
		t.Fatal("submit on completed run must fail")
	}
	if _, err := service.CancelRun(ctx, thread.ID, run.ID, nil); err == nil {
		t.Fatal("cancel of completed run must fail")
	}
	runs, err := service.ListRuns(ctx, thread.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs: %+v %v", runs, err)
	}
}

func TestRouter_RunRequiresAction(t *testing.T) {
	mock := testutil.NewMockAdapter("mock")
	calls := 0
	mock.WithCompleteFunc(func(ctx context.Context, creds []*models.Credential, request *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		calls++
		if calls == 1 {
			return &models.ChatCompletionResponse{
				ID: "r1", Object: "chat.completion", Model: string(request.Model),
				Choices: []models.ChatCompletionChoice{{
					Message: models.ChatMessage{
						Role: "assistant",
						ToolCalls: []models.ChatToolCall{{
							ID:       "call_1",
							Type:     "function",
							Function: models.ChatToolFunction{Name: "get_weather", Arguments: `{"city":"Oslo"}`},
						}},
					},
					FinishReason: "tool_calls",
				}},
			}, nil
		}
		return &models.ChatCompletionResponse{
			ID: "r2", Object: "chat.completion", Model: string(request.Model),
			Choices: []models.ChatCompletionChoice{{
				Message:      models.ChatMessage{Role: "assistant", Content: "sunny"},
				FinishReason: "stop",
			}},
		}, nil
	})
	service := setupCompatRouter(t, mock)
	ctx := context.Background()
	assistant, err := service.CreateAssistant(ctx, &models.AssistantRequest{Model: "mock/m"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := service.CreateThread(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateThreadMessage(ctx, thread.ID, &models.ThreadMessageRequest{Role: "user", Content: json.RawMessage(`"weather?"`)}); err != nil {
		t.Fatal(err)
	}
	run, err := service.CreateRun(ctx, thread.ID, &models.RunRequest{AssistantID: assistant.ID}, nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if run.Status != "requires_action" || run.RequiredAction == nil {
		t.Fatalf("run: %+v", run)
	}
	done, err := service.SubmitToolOutputs(ctx, thread.ID, run.ID, nil, []models.ToolOutput{{ToolCallID: "call_1", Output: "sunny"}})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if done.Status != "completed" {
		t.Fatalf("run: %+v", done)
	}
	messages, _ := service.ListThreadMessages(ctx, thread.ID)
	if len(messages) != 2 {
		t.Fatalf("messages: %+v", messages)
	}
	found := false
	for _, m := range messages {
		if m.Role == "assistant" && len(m.Content) > 0 && m.Content[0].Text == "sunny" {
			found = true
		}
	}
	if !found {
		t.Fatalf("assistant turn missing: %+v", messages)
	}
}

// moderateMockAdapter serves the Moderator capability for success-path tests.
type moderateMockAdapter struct {
	*testutil.MockAdapter
}

func (m *moderateMockAdapter) Moderate(_ context.Context, _ []*models.Credential, request *models.ModerationRequest, _ map[string]any) (*models.ModerationResponse, error) {
	results := make([]models.ModerationResult, 0, len(request.Input))
	for range request.Input {
		results = append(results, models.ModerationResult{
			Categories:     map[string]bool{"violence": false},
			CategoryScores: map[string]float64{"violence": 0.01},
		})
	}
	return &models.ModerationResponse{Results: results}, nil
}

func TestRouter_Moderate(t *testing.T) {
	service := setupCompatRouter(t, nil)
	_, err := service.Moderate(context.Background(), &models.ModerationRequest{Model: "mock/m", Input: []string{"hi"}}, nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("plain mock must not serve moderations, got %v", err)
	}
	service2 := setupCompatRouter(t, &moderateMockAdapter{MockAdapter: testutil.NewMockAdapter("mock")})
	response, err := service2.Moderate(context.Background(), &models.ModerationRequest{Model: "mock/m", Input: []string{"a", "b"}}, nil)
	if err != nil {
		t.Fatalf("moderate: %v", err)
	}
	if len(response.Results) != 2 || response.Results[0].Flagged {
		t.Fatalf("results: %+v", response.Results)
	}
}

func TestRouter_Batch(t *testing.T) {
	service := setupCompatRouter(t, nil)
	ctx := context.Background()
	if _, err := service.CreateBatch(ctx, &models.BatchCreateRequest{}, nil); err == nil {
		t.Fatal("empty batch must fail")
	}
	batchInfo, err := service.CreateBatch(ctx, &models.BatchCreateRequest{Requests: []models.BatchEntry{
		{CustomID: "one", Params: json.RawMessage(`{"model":"mock/m","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)},
		{CustomID: "two", Params: json.RawMessage(`{"model":"mock/m","max_tokens":8,"messages":[{"role":"user","content":[]}]}`)},
	}}, nil)
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if !strings.HasPrefix(batchInfo.ID, "batch_") || batchInfo.ProcessingStatus != "ended" {
		t.Fatalf("batch: %+v", batchInfo)
	}
	if batchInfo.RequestCounts.Succeeded != 1 || batchInfo.RequestCounts.Errored != 1 {
		t.Fatalf("counts: %+v", batchInfo.RequestCounts)
	}
	got, err := service.GetBatch(ctx, batchInfo.ID, nil)
	if err != nil || got.ID != batchInfo.ID {
		t.Fatalf("get: %+v %v", got, err)
	}
	lines, err := service.GetBatchResults(ctx, batchInfo.ID, nil)
	if err != nil || strings.Count(lines, "\n") != 2 {
		t.Fatalf("results: %q %v", lines, err)
	}
	if _, err := service.CancelBatch(ctx, batchInfo.ID, nil); err == nil {
		t.Fatal("cancel of ended batch must fail")
	}
	list, err := service.ListBatches(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := service.DeleteBatch(ctx, batchInfo.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := service.GetBatch(ctx, batchInfo.ID, nil); apierrors.ToAPIError(err).Status != 404 {
		t.Fatalf("deleted must 404, got %v", err)
	}
}

func TestRouter_Conversations(t *testing.T) {
	service := setupCompatRouter(t, nil)
	ctx := context.Background()
	conversation, err := service.CreateConversation(ctx, "", map[string]string{"k": "v"}, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(conversation.ID, "conv_") {
		t.Fatalf("conv: %+v", conversation)
	}
	items, err := service.AppendConversationItems(ctx, conversation.ID, nil, []models.ResponseInputItem{{Type: "message", Role: "user"}})
	if err != nil || len(items) != 1 {
		t.Fatalf("append: %+v %v", items, err)
	}
	got, err := service.GetConversation(ctx, conversation.ID, nil)
	if err != nil || got.ID != conversation.ID {
		t.Fatalf("get: %+v %v", got, err)
	}
	if err := service.DeleteConversation(ctx, conversation.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := service.GetConversation(ctx, conversation.ID, nil); apierrors.ToAPIError(err).Status != 404 {
		t.Fatalf("deleted must 404, got %v", err)
	}
}
