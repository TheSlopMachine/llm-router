package luaplugin

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin/statepool"
)

func resetSharedPool(t *testing.T) {
	t.Helper()
	statepool.InvalidateAll()
	statepool.SetBucketSize(statepool.DefaultBucketSize)
	t.Cleanup(func() {
		statepool.InvalidateAll()
		statepool.SetBucketSize(statepool.DefaultBucketSize)
	})
}

func qualifiedBucketKey(record *PluginRecord, lookupKey string) string {
	return record.ID + "\x00" + lookupKey
}

func completeWithHello(t *testing.T, service *Service, typeKey string) string {
	t.Helper()
	response, err := service.Complete(context.Background(), testMetadata(typeKey,
		&models.Credential{ID: "c1", Data: map[string]any{"api_key": "k"}}, models.ModelId(typeKey+"/model-a"), nil),
		&models.ChatCompletionRequest{Model: models.ModelId(typeKey + "/model-a"), Messages: []models.ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(response.Choices) != 1 {
		t.Fatalf("choices: %+v", response.Choices)
	}
	return response.Choices[0].Message.Content
}

func TestPooledCompleteReusesBucket(t *testing.T) {
	resetSharedPool(t)
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	bucketKey := qualifiedBucketKey(record, "test-type")
	if content := completeWithHello(t, service, "test-type"); content != "hello" {
		t.Fatalf("first content = %q, want hello", content)
	}
	if content := completeWithHello(t, service, "test-type"); content != "hello" {
		t.Fatalf("second content = %q, want hello", content)
	}
	if got := statepool.Length(bucketKey); got != 1 {
		t.Fatalf("bucket length = %d, want 1", got)
	}
}

func TestPooledTopLevelResetsAcrossCalls(t *testing.T) {
	resetSharedPool(t)
	service := setupService(t)
	source := `--- @plugin Counter Plugin
--- @author tester
--- @version 1.0.0
--- @plugin_api 1.0
--- @description Counter plugin
--- @allow_host example.com

counter = 0
llm_router.register("counter-type", {
  complete = function(ctx, request)
    counter = counter + 1
    return {
      id = "chatcmpl-counter",
      object = "chat.completion",
      created = 1700000000,
      model = request.model,
      choices = {
        { index = 0, message = { role = "assistant", content = "call-" .. tostring(counter) }, finish_reason = "stop" },
      },
      usage = { prompt_tokens = 1, completion_tokens = 1, total_tokens = 2 },
    }
  end,
})
`
	if _, err := service.InstallSQLite(context.Background(), []byte(source), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	first := completeWithHello(t, service, "counter-type")
	second := completeWithHello(t, service, "counter-type")
	if first != second {
		t.Fatalf("contents differ across pooled calls: %q vs %q", first, second)
	}
}

func TestPoolInvalidatedOnUpdate(t *testing.T) {
	resetSharedPool(t)
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	bucketKey := qualifiedBucketKey(record, "test-type")
	if content := completeWithHello(t, service, "test-type"); content != "hello" {
		t.Fatalf("first content = %q", content)
	}
	if got := statepool.Length(bucketKey); got != 1 {
		t.Fatalf("length before update = %d, want 1", got)
	}
	updated := strings.Replace(testPluginSource, "hello", "updated", 1)
	updated = strings.Replace(updated, "@version 1.0.0", "@version 2.0.0", 1)
	if _, err := service.InstallSQLite(context.Background(), []byte(updated), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := statepool.Length(bucketKey); got != 0 {
		t.Fatalf("length after update = %d, want 0", got)
	}
	if content := completeWithHello(t, service, "test-type"); content != "updated" {
		t.Fatalf("content after update = %q, want updated", content)
	}
}

func TestPoolInvalidatedOnDisable(t *testing.T) {
	resetSharedPool(t)
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	bucketKey := qualifiedBucketKey(record, "test-type")
	completeWithHello(t, service, "test-type")
	if got := statepool.Length(bucketKey); got != 1 {
		t.Fatalf("length before disable = %d, want 1", got)
	}
	if _, err := service.SetDisabled(context.Background(), record.ID, true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if got := statepool.Length(bucketKey); got != 0 {
		t.Fatalf("length after disable = %d, want 0", got)
	}
	if _, err := service.SetDisabled(context.Background(), record.ID, false); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if content := completeWithHello(t, service, "test-type"); content != "hello" {
		t.Fatalf("content after enable = %q", content)
	}
}

func TestPoolInvalidatedOnDelete(t *testing.T) {
	resetSharedPool(t)
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	bucketKey := qualifiedBucketKey(record, "test-type")
	completeWithHello(t, service, "test-type")
	if err := service.DeleteSQLite(context.Background(), record.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := statepool.Length(bucketKey); got != 0 {
		t.Fatalf("length after delete = %d, want 0", got)
	}
}

func TestPoolInvalidatedOnRollback(t *testing.T) {
	resetSharedPool(t)
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install v1: %v", err)
	}
	bucketKey := qualifiedBucketKey(record, "test-type")
	second := strings.Replace(testPluginSource, "hello", "second", 1)
	second = strings.Replace(second, "@version 1.0.0", "@version 2.0.0", 1)
	if _, err := service.InstallSQLite(context.Background(), []byte(second), PluginOrigin{Manual: true}); err != nil {
		t.Fatalf("install v2: %v", err)
	}
	if content := completeWithHello(t, service, "test-type"); content != "second" {
		t.Fatalf("v2 content = %q", content)
	}
	if _, err := service.RollbackSQLite(context.Background(), record.ID); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := statepool.Length(bucketKey); got != 0 {
		t.Fatalf("length after rollback = %d, want 0", got)
	}
	if content := completeWithHello(t, service, "test-type"); content != "hello" {
		t.Fatalf("content after rollback = %q, want hello", content)
	}
}

func TestConcurrentPooledCompletes(t *testing.T) {
	resetSharedPool(t)
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(testPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	bucketKey := qualifiedBucketKey(record, "test-type")
	var group sync.WaitGroup
	for worker := range 16 {
		_ = worker
		group.Add(1)
		go func() {
			defer group.Done()
			for range 10 {
				if content := completeWithHello(t, service, "test-type"); content != "hello" {
					t.Errorf("content = %q, want hello", content)
					return
				}
			}
		}()
	}
	group.Wait()
	if got := statepool.Length(bucketKey); got < 1 || got > statepool.DefaultBucketSize {
		t.Fatalf("bucket length = %d, want 1..%d", got, statepool.DefaultBucketSize)
	}
}

func TestPooledStreamHoldsStateUntilDone(t *testing.T) {
	resetSharedPool(t)
	service := setupService(t)
	record, err := service.InstallSQLite(context.Background(), []byte(streamPluginSource), PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	bucketKey := qualifiedBucketKey(record, "stream-type")
	var output bytes.Buffer
	metadata := testMetadata("stream-type",
		&models.Credential{ID: "c1", Data: map[string]any{"api_key": "k"}}, "stream-type/model-a", nil)
	request := &models.ChatCompletionRequest{Model: "stream-type/model-a", Messages: []models.ChatMessage{{Role: "user", Content: "hi"}}}
	if err := service.CompleteStream(context.Background(), metadata, request, &output); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if !strings.Contains(output.String(), "[DONE]") {
		t.Fatalf("stream output misses DONE: %q", output.String())
	}
	if got := statepool.Length(bucketKey); got < 1 {
		t.Fatalf("bucket length = %d, want at least 1", got)
	}
	// The state served the stream and returned; a later call reuses it.
	response, err := service.Complete(context.Background(), testMetadata("stream-type",
		&models.Credential{ID: "c1", Data: map[string]any{"api_key": "k"}}, "stream-type/model-a", nil), request)
	if err != nil {
		t.Fatalf("complete after stream: %v", err)
	}
	if len(response.Choices) != 1 || response.Choices[0].Message.Content != "hi" {
		t.Fatalf("content after stream: %+v", response)
	}
}

func TestPoolBucketKeyQualified(t *testing.T) {
	first := poolBucketKey(&PluginRecord{ID: "manual/tester/Alpha"}, "test-type")
	second := poolBucketKey(&PluginRecord{ID: "manual/tester/Beta"}, "test-type")
	if first == second {
		t.Fatal("distinct plugins shared one bucket key")
	}
	if got := poolBucketKey(nil, "test-type"); got != "" {
		t.Fatalf("nil record key = %q, want empty", got)
	}
	if got := poolBucketKey(&PluginRecord{ID: "manual/tester/Alpha"}, ""); got != "" {
		t.Fatalf("empty lookup key = %q, want empty", got)
	}
}
