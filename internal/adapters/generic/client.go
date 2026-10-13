package generic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// Client wraps HTTP requests to an OpenAI-compatible endpoint.
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
}

func newClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		logger: slog.Default(),
	}
}

// SetLogger installs structured client logging.
func (c *Client) SetLogger(logger *slog.Logger) {
	if logger == nil {
		return
	}
	c.logger = logger
}

func (c *Client) currentLogger() *slog.Logger {
	if c == nil || c.logger == nil {
		return slog.Default()
	}
	return c.logger
}

func truncateBody(body string) string {
	if len(body) > 1000 {
		return body[:1000] + "...[truncated]"
	}
	return body
}

// ChatCompletion sends a non-streaming chat completion request.
func (c *Client) ChatCompletion(
	ctx context.Context,
	apiKey string,
	modelName string,
	request *models.ChatCompletionRequest,
) (*models.ChatCompletionResponse, error) {
	payload, err := passthroughPayload(request, modelName)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		c.currentLogger().Warn("Upstream request failed", "endpoint", "/chat/completions", "model", modelName, "error", err)
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		classified := classifyHTTPError(response.StatusCode, string(bodyBytes))
		c.currentLogger().Warn("Upstream returned error", "endpoint", "/chat/completions", "model", modelName, "status", response.StatusCode, "body", truncateBody(string(bodyBytes)))
		return nil, classified
	}

	var result models.ChatCompletionResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		c.currentLogger().Warn("Decoding upstream response failed", "endpoint", "/chat/completions", "model", modelName, "error", err)
		return nil, fmt.Errorf("decode response: %w", err)
	}

	c.currentLogger().Debug("Upstream request succeeded", "endpoint", "/chat/completions", "model", modelName)
	return &result, nil
}

// ChatCompletionStream sends a streaming chat completion request.
func (c *Client) ChatCompletionStream(
	ctx context.Context,
	apiKey string,
	modelName string,
	request *models.ChatCompletionRequest,
	writer io.Writer,
) error {
	payload, err := passthroughPayload(request, modelName)
	if err != nil {
		return err
	}
	payload["stream"] = true

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
	httpRequest.Header.Set("Accept", "text/event-stream")

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		c.currentLogger().Warn("Upstream request failed", "endpoint", "/chat/completions", "model", modelName, "error", err)
		return fmt.Errorf("http request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		classified := classifyHTTPError(response.StatusCode, string(bodyBytes))
		c.currentLogger().Warn("Upstream returned error", "endpoint", "/chat/completions", "model", modelName, "status", response.StatusCode, "body", truncateBody(string(bodyBytes)))
		return classified
	}

	_, err = io.Copy(writer, response.Body)
	if err != nil {
		c.currentLogger().Warn("Reading upstream stream failed", "endpoint", "/chat/completions", "model", modelName, "error", err)
	}
	return err
}

// ListModels fetches the available models from the provider.
// Tolerant of compat variances: 404 is not an error (routing still works),
// extra fields like object/limit are ignored.
func (c *Client) ListModels(ctx context.Context, apiKey string) ([]models.ModelInfo, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		c.currentLogger().Warn("Upstream request failed", "endpoint", "/models", "error", err)
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		c.currentLogger().Debug("Upstream model catalog missing", "endpoint", "/models")
		return nil, nil
	}

	if response.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		classified := classifyHTTPError(response.StatusCode, string(bodyBytes))
		c.currentLogger().Warn("Upstream returned error", "endpoint", "/models", "status", response.StatusCode, "body", truncateBody(string(bodyBytes)))
		return nil, classified
	}

	var result struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
		} `json:"data"`
	}

	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		c.currentLogger().Warn("Decoding upstream response failed", "endpoint", "/models", "error", err)
		return nil, fmt.Errorf("decode response: %w", err)
	}

	modelsList := make([]models.ModelInfo, 0, len(result.Data))
	for _, entry := range result.Data {
		if strings.TrimSpace(entry.ID) == "" {
			continue
		}
		modelsList = append(modelsList, models.ModelInfo{
			Name:        entry.ID,
			DisplayName: entry.ID,
		})
	}

	return modelsList, nil
}

// passthroughPayload forwards the normalized request as-is, replacing only
// the composite ModelId with the upstream model name. The struct owns the
// OpenAI schema; the adapter holds no field allowlist.
func passthroughPayload(request *models.ChatCompletionRequest, modelName string) (map[string]any, error) {
	raw, err := json.Marshal(request)
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

// videoPayload forwards the normalized video request as-is, replacing only
// the composite ModelId with the upstream model name.
func videoPayload(request *models.VideoGenerationRequest, modelName string) (map[string]any, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal video request: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("normalize video request: %w", err)
	}
	payload["model"] = modelName
	return payload, nil
}

// postVideoJSON sends one JSON video request and decodes the JSON answer.
func (c *Client) postVideoJSON(ctx context.Context, apiKey, path string, payload map[string]any, output *models.VideoGenerationResponse) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		c.currentLogger().Warn("Upstream request failed", "endpoint", path, "error", err)
		return fmt.Errorf("http request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		classified := classifyHTTPError(response.StatusCode, string(bodyBytes))
		c.currentLogger().Warn("Upstream returned error", "endpoint", path, "status", response.StatusCode, "body", truncateBody(string(bodyBytes)))
		return classified
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		c.currentLogger().Warn("Decoding upstream response failed", "endpoint", path, "error", err)
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// SubmitVideo posts one video generation request to the upstream.
func (c *Client) SubmitVideo(ctx context.Context, apiKey, modelName string, request *models.VideoGenerationRequest) (*models.VideoGenerationResponse, error) {
	payload, err := videoPayload(request, modelName)
	if err != nil {
		return nil, err
	}
	var output models.VideoGenerationResponse
	if err := c.postVideoJSON(ctx, apiKey, "/videos", payload, &output); err != nil {
		return nil, err
	}
	if output.ID == "" {
		c.currentLogger().Warn("Decoding upstream response failed", "endpoint", "/videos", "model", modelName, "reason", "empty job id")
		return nil, fmt.Errorf("decode response: empty job id")
	}
	if !models.ValidVideoStatus(output.Status) {
		c.currentLogger().Warn("Decoding upstream response failed", "endpoint", "/videos", "model", modelName, "reason", "unknown job status")
		return nil, fmt.Errorf("decode response: unknown job status %q", output.Status)
	}
	return &output, nil
}

// PollVideo fetches one upstream video job status.
func (c *Client) PollVideo(ctx context.Context, apiKey, upstreamJobID string) (*models.VideoGenerationResponse, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/videos/"+upstreamJobID, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		c.currentLogger().Warn("Upstream request failed", "endpoint", "/videos", "error", err)
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		classified := classifyHTTPError(response.StatusCode, string(bodyBytes))
		c.currentLogger().Warn("Upstream returned error", "endpoint", "/videos", "status", response.StatusCode, "body", truncateBody(string(bodyBytes)))
		return nil, classified
	}
	var output models.VideoGenerationResponse
	if err := json.NewDecoder(response.Body).Decode(&output); err != nil {
		c.currentLogger().Warn("Decoding upstream response failed", "endpoint", "/videos", "error", err)
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if !models.ValidVideoStatus(output.Status) {
		c.currentLogger().Warn("Decoding upstream response failed", "endpoint", "/videos", "reason", "unknown job status")
		return nil, fmt.Errorf("decode response: unknown job status %q", output.Status)
	}
	return &output, nil
}

// VideoContent downloads one upstream video asset.
func (c *Client) VideoContent(ctx context.Context, apiKey, upstreamJobID string, index int) (*models.VideoContentResponse, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/videos/%s/content?index=%d", c.baseURL, upstreamJobID, index), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		c.currentLogger().Warn("Upstream request failed", "endpoint", "/videos/content", "error", err)
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		classified := classifyHTTPError(response.StatusCode, string(bodyBytes))
		c.currentLogger().Warn("Upstream returned error", "endpoint", "/videos/content", "status", response.StatusCode, "body", truncateBody(string(bodyBytes)))
		return nil, classified
	}
	video, err := io.ReadAll(io.LimitReader(response.Body, 512<<20))
	if err != nil {
		c.currentLogger().Warn("Reading upstream video failed", "endpoint", "/videos/content", "error", err)
		return nil, fmt.Errorf("read video: %w", err)
	}
	if len(video) == 0 {
		c.currentLogger().Warn("Decoding upstream response failed", "endpoint", "/videos/content", "reason", "empty video")
		return nil, fmt.Errorf("decode response: empty video")
	}
	return &models.VideoContentResponse{Video: video, ContentType: response.Header.Get("Content-Type")}, nil
}

// ListVideoModels fetches the upstream video model catalog. A 404 is not
// an error: upstreams without a video catalog still route video jobs.
func (c *Client) ListVideoModels(ctx context.Context, apiKey string) ([]models.VideoModel, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/videos/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		c.currentLogger().Warn("Upstream request failed", "endpoint", "/videos/models", "error", err)
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		c.currentLogger().Debug("Upstream video catalog missing", "endpoint", "/videos/models")
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		classified := classifyHTTPError(response.StatusCode, string(bodyBytes))
		c.currentLogger().Warn("Upstream returned error", "endpoint", "/videos/models", "status", response.StatusCode, "body", truncateBody(string(bodyBytes)))
		return nil, classified
	}
	var result models.VideoModelsListResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		c.currentLogger().Warn("Decoding upstream response failed", "endpoint", "/videos/models", "error", err)
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if result.Data == nil {
		return []models.VideoModel{}, nil
	}
	return result.Data, nil
}
