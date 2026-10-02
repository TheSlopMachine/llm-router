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
	"github.com/TheSlopMachine/llm-router/internal/services/exhausted"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/responses"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

func setupCompatRouter(t *testing.T, mock provider.GoAdapter) *Service {
	t.Helper()
	database := testutil.SetupTestDB(t)
	providerSvc := provider.NewService(database)
	if mock == nil {
		mock = testutil.NewMockAdapter("mock")
	}
	providerSvc.RegisterGoAdapter(mock)
	if _, err := providerSvc.Create(provider.CreateOptions{Name: "Mock", TypeKey: "mock"}); err != nil {
		t.Fatalf("create mock provider: %v", err)
	}
	credSvc := credential.New(database, providerSvc)
	if _, err := credSvc.Add(credential.AddOptions{
		ProviderID: "mock",
		Label:      "Cred 1",
		Data:       map[string]any{"api_key": "test-key"},
	}); err != nil {
		t.Fatalf("add credential: %v", err)
	}
	modelInfoSvc := modelinfo.New(database, providerSvc, credSvc, 1*time.Hour)
	return New(providerSvc, credSvc, modelInfoSvc, exhausted.New(database), videojobs.New(database),
		responses.New(database), batches.New(database), slog.Default())
}

func responseReq(model, input string) *models.ResponseRequest {
	return &models.ResponseRequest{
		Model: models.ModelId(model),
		Input: json.RawMessage(`"` + input + `"`),
	}
}

func TestRouter_CreateGetResponse(t *testing.T) {
	svc := setupCompatRouter(t, nil)
	ctx := context.Background()
	out, err := svc.CreateResponse(ctx, responseReq("mock/m", "hi"), nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(out.ID, "resp_") || out.Status != "completed" {
		t.Fatalf("response: %+v", out)
	}
	if len(out.Output) != 1 || out.Output[0].Text != "mock response" {
		t.Fatalf("output: %+v", out.Output)
	}
	if out.Usage == nil || out.Usage.TotalTokens != 15 {
		t.Fatalf("usage: %+v", out.Usage)
	}
	got, err := svc.GetResponse(ctx, out.ID, nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != out.ID {
		t.Fatalf("id: %q", got.ID)
	}
	items, err := svc.ListResponseInputItems(ctx, out.ID, nil)
	if err != nil || len(items) != 1 {
		t.Fatalf("input items: %+v %v", items, err)
	}
	if _, err := svc.GetResponse(ctx, "resp_missing", nil); apierrors.ToAPIError(err).Status != 404 {
		t.Fatalf("missing must 404, got %v", err)
	}
	if _, err := svc.CancelResponse(ctx, out.ID, nil); err == nil {
		t.Fatal("cancel of completed must fail")
	}
	compacted, err := svc.CompactResponse(ctx, out.ID, nil)
	if err != nil || compacted.ID != out.ID {
		t.Fatalf("compact: %+v %v", compacted, err)
	}
	if n := svc.CountResponseInputTokens(responseReq("mock/m", "hi")); n < 1 {
		t.Fatalf("tokens: %d", n)
	}
}

func TestRouter_AssistantThreadRun(t *testing.T) {
	svc := setupCompatRouter(t, nil)
	ctx := context.Background()
	assistant, err := svc.CreateAssistant(ctx, &models.AssistantRequest{Model: "mock/m", Instructions: "be nice"})
	if err != nil {
		t.Fatalf("create assistant: %v", err)
	}
	if !strings.HasPrefix(assistant.ID, "asst_") {
		t.Fatalf("assistant: %+v", assistant)
	}
	if _, err := svc.CreateAssistant(ctx, &models.AssistantRequest{}); err == nil {
		t.Fatal("assistant without model must fail")
	}
	thread, err := svc.CreateThread(ctx, nil)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := svc.CreateThreadMessage(ctx, thread.ID, &models.ThreadMessageRequest{Role: "user", Content: json.RawMessage(`"hello"`)}); err != nil {
		t.Fatalf("create message: %v", err)
	}
	if _, err := svc.CreateThreadMessage(ctx, thread.ID, &models.ThreadMessageRequest{Role: "system", Content: json.RawMessage(`"x"`)}); err == nil {
		t.Fatal("system role must fail")
	}
	run, err := svc.CreateRun(ctx, thread.ID, &models.RunRequest{AssistantID: assistant.ID}, nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("run: %+v", run)
	}
	msgs, err := svc.ListThreadMessages(ctx, thread.ID)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("messages: %+v %v", msgs, err)
	}
	if _, err := svc.GetThreadMessage(ctx, thread.ID, msgs[0].ID); err != nil {
		t.Fatalf("get message: %v", err)
	}
	if _, err := svc.SubmitToolOutputs(ctx, thread.ID, run.ID, nil, []models.ToolOutput{{ToolCallID: "x", Output: "y"}}); err == nil {
		t.Fatal("submit on completed run must fail")
	}
	if _, err := svc.CancelRun(ctx, thread.ID, run.ID, nil); err == nil {
		t.Fatal("cancel of completed run must fail")
	}
	runs, err := svc.ListRuns(ctx, thread.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs: %+v %v", runs, err)
	}
}

func TestRouter_RunRequiresAction(t *testing.T) {
	mock := testutil.NewMockAdapter("mock")
	calls := 0
	mock.WithCompleteFunc(func(ctx context.Context, creds []*models.Credential, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
		calls++
		if calls == 1 {
			return &models.ChatCompletionResponse{
				ID: "r1", Object: "chat.completion", Model: string(req.Model),
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
			ID: "r2", Object: "chat.completion", Model: string(req.Model),
			Choices: []models.ChatCompletionChoice{{
				Message:      models.ChatMessage{Role: "assistant", Content: "sunny"},
				FinishReason: "stop",
			}},
		}, nil
	})
	svc := setupCompatRouter(t, mock)
	ctx := context.Background()
	assistant, err := svc.CreateAssistant(ctx, &models.AssistantRequest{Model: "mock/m"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.CreateThread(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateThreadMessage(ctx, thread.ID, &models.ThreadMessageRequest{Role: "user", Content: json.RawMessage(`"weather?"`)}); err != nil {
		t.Fatal(err)
	}
	run, err := svc.CreateRun(ctx, thread.ID, &models.RunRequest{AssistantID: assistant.ID}, nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if run.Status != "requires_action" || run.RequiredAction == nil {
		t.Fatalf("run: %+v", run)
	}
	done, err := svc.SubmitToolOutputs(ctx, thread.ID, run.ID, nil, []models.ToolOutput{{ToolCallID: "call_1", Output: "sunny"}})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if done.Status != "completed" {
		t.Fatalf("run: %+v", done)
	}
	msgs, _ := svc.ListThreadMessages(ctx, thread.ID)
	if len(msgs) != 2 {
		t.Fatalf("messages: %+v", msgs)
	}
	found := false
	for _, m := range msgs {
		if m.Role == "assistant" && len(m.Content) > 0 && m.Content[0].Text == "sunny" {
			found = true
		}
	}
	if !found {
		t.Fatalf("assistant turn missing: %+v", msgs)
	}
}

// moderateMockAdapter serves the Moderator capability for success-path tests.
type moderateMockAdapter struct {
	*testutil.MockAdapter
}

func (m *moderateMockAdapter) Moderate(_ context.Context, _ []*models.Credential, req *models.ModerationRequest, _ map[string]any) (*models.ModerationResponse, error) {
	results := make([]models.ModerationResult, 0, len(req.Input))
	for range req.Input {
		results = append(results, models.ModerationResult{
			Categories:     map[string]bool{"violence": false},
			CategoryScores: map[string]float64{"violence": 0.01},
		})
	}
	return &models.ModerationResponse{Results: results}, nil
}

func TestRouter_Moderate(t *testing.T) {
	svc := setupCompatRouter(t, nil)
	_, err := svc.Moderate(context.Background(), &models.ModerationRequest{Model: "mock/m", Input: []string{"hi"}}, nil)
	if !errors.Is(err, apierrors.ErrEndpointNotSupported) {
		t.Fatalf("plain mock must not serve moderations, got %v", err)
	}
	svc2 := setupCompatRouter(t, &moderateMockAdapter{MockAdapter: testutil.NewMockAdapter("mock")})
	resp, err := svc2.Moderate(context.Background(), &models.ModerationRequest{Model: "mock/m", Input: []string{"a", "b"}}, nil)
	if err != nil {
		t.Fatalf("moderate: %v", err)
	}
	if len(resp.Results) != 2 || resp.Results[0].Flagged {
		t.Fatalf("results: %+v", resp.Results)
	}
}

func TestRouter_Batch(t *testing.T) {
	svc := setupCompatRouter(t, nil)
	ctx := context.Background()
	if _, err := svc.CreateBatch(ctx, &models.BatchCreateRequest{}, nil); err == nil {
		t.Fatal("empty batch must fail")
	}
	info, err := svc.CreateBatch(ctx, &models.BatchCreateRequest{Requests: []models.BatchEntry{
		{CustomID: "one", Params: json.RawMessage(`{"model":"mock/m","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)},
		{CustomID: "two", Params: json.RawMessage(`{"model":"mock/m","max_tokens":8,"messages":[{"role":"user","content":[]}]}`)},
	}}, nil)
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if !strings.HasPrefix(info.ID, "batch_") || info.ProcessingStatus != "ended" {
		t.Fatalf("batch: %+v", info)
	}
	if info.RequestCounts.Succeeded != 1 || info.RequestCounts.Errored != 1 {
		t.Fatalf("counts: %+v", info.RequestCounts)
	}
	got, err := svc.GetBatch(ctx, info.ID, nil)
	if err != nil || got.ID != info.ID {
		t.Fatalf("get: %+v %v", got, err)
	}
	lines, err := svc.GetBatchResults(ctx, info.ID, nil)
	if err != nil || strings.Count(lines, "\n") != 2 {
		t.Fatalf("results: %q %v", lines, err)
	}
	if _, err := svc.CancelBatch(ctx, info.ID, nil); err == nil {
		t.Fatal("cancel of ended batch must fail")
	}
	list, err := svc.ListBatches(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := svc.DeleteBatch(ctx, info.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.GetBatch(ctx, info.ID, nil); apierrors.ToAPIError(err).Status != 404 {
		t.Fatalf("deleted must 404, got %v", err)
	}
}

func TestRouter_Conversations(t *testing.T) {
	svc := setupCompatRouter(t, nil)
	ctx := context.Background()
	conv, err := svc.CreateConversation(ctx, "", map[string]string{"k": "v"}, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(conv.ID, "conv_") {
		t.Fatalf("conv: %+v", conv)
	}
	items, err := svc.AppendConversationItems(ctx, conv.ID, nil, []models.ResponseInputItem{{Type: "message", Role: "user"}})
	if err != nil || len(items) != 1 {
		t.Fatalf("append: %+v %v", items, err)
	}
	got, err := svc.GetConversation(ctx, conv.ID, nil)
	if err != nil || got.ID != conv.ID {
		t.Fatalf("get: %+v %v", got, err)
	}
	if err := svc.DeleteConversation(ctx, conv.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.GetConversation(ctx, conv.ID, nil); apierrors.ToAPIError(err).Status != 404 {
		t.Fatalf("deleted must 404, got %v", err)
	}
}
