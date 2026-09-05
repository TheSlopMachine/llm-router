package generic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	sdk "github.com/TheSlopMachine/llm-router-sdk"
)

// Client wraps HTTP requests to an OpenAI-compatible endpoint.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func newClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// ChatCompletion sends a non-streaming chat completion request.
func (c *Client) ChatCompletion(
	ctx context.Context,
	apiKey string,
	modelName string,
	req *sdk.ChatCompletionRequest,
) (*sdk.ChatCompletionResponse, error) {
	payload := transformRequest(req, modelName)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result sdk.ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

// ChatCompletionStream sends a streaming chat completion request.
func (c *Client) ChatCompletionStream(
	ctx context.Context,
	apiKey string,
	modelName string,
	req *sdk.ChatCompletionRequest,
	w io.Writer,
) error {
	payload := transformRequest(req, modelName)
	payload["stream"] = true

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Stream response directly to writer
	_, err = io.Copy(w, resp.Body)
	return err
}

// ListModels fetches the available models from the provider.
// Tolerant of compat variances: 404 is not an error (routing still works),
// extra fields like object/limit are ignored.
func (c *Client) ListModels(ctx context.Context, apiKey string) ([]sdk.ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Many compat servers don't implement GET /models — not fatal. Routing still works.
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	models := make([]sdk.ModelInfo, 0, len(result.Data))
	for _, m := range result.Data {
		if strings.TrimSpace(m.ID) == "" {
			continue
		}
		models = append(models, sdk.ModelInfo{
			Name:        m.ID,
			DisplayName: m.ID,
		})
	}

	return models, nil
}

// transformRequest converts SDK request to OpenAI-compatible format.
func transformRequest(req *sdk.ChatCompletionRequest, modelName string) map[string]interface{} {
	payload := map[string]interface{}{
		"model":    modelName,
		"messages": req.Messages,
	}

	// Only include non-zero values to respect omitempty behavior
	if req.Temperature != 0 {
		payload["temperature"] = req.Temperature
	}
	if req.MaxTokens != 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.TopP != 0 {
		payload["top_p"] = req.TopP
	}
	if req.N != nil {
		payload["n"] = *req.N
	}
	if req.Stop != nil {
		payload["stop"] = req.Stop
	}
	if req.PresencePenalty != nil {
		payload["presence_penalty"] = *req.PresencePenalty
	}
	if req.FrequencyPenalty != nil {
		payload["frequency_penalty"] = *req.FrequencyPenalty
	}
	if req.MaxCompletionTokens != nil {
		payload["max_completion_tokens"] = *req.MaxCompletionTokens
	}

	return payload
}
