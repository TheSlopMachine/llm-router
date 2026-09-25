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

	"github.com/TheSlopMachine/llm-router/internal/models"
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
	req *models.ChatCompletionRequest,
) (*models.ChatCompletionResponse, error) {
	payload, err := passthroughPayload(req, modelName)
	if err != nil {
		return nil, err
	}
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
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, classifyHTTPError(resp.StatusCode, string(bodyBytes))
	}

	var result models.ChatCompletionResponse
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
	req *models.ChatCompletionRequest,
	w io.Writer,
) error {
	payload, err := passthroughPayload(req, modelName)
	if err != nil {
		return err
	}
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
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return classifyHTTPError(resp.StatusCode, string(bodyBytes))
	}

	_, err = io.Copy(w, resp.Body)
	return err
}

// ListModels fetches the available models from the provider.
// Tolerant of compat variances: 404 is not an error (routing still works),
// extra fields like object/limit are ignored.
func (c *Client) ListModels(ctx context.Context, apiKey string) ([]models.ModelInfo, error) {
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
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, classifyHTTPError(resp.StatusCode, string(bodyBytes))
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

	modelsList := make([]models.ModelInfo, 0, len(result.Data))
	for _, m := range result.Data {
		if strings.TrimSpace(m.ID) == "" {
			continue
		}
		modelsList = append(modelsList, models.ModelInfo{
			Name:        m.ID,
			DisplayName: m.ID,
		})
	}

	return modelsList, nil
}

// passthroughPayload forwards the normalized request as-is, replacing only
// the composite ModelId with the upstream model name. The struct owns the
// OpenAI schema; the adapter holds no field allowlist.
func passthroughPayload(req *models.ChatCompletionRequest, modelName string) (map[string]any, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("normalize request: %w", err)
	}
	payload["model"] = modelName
	return payload, nil
}
