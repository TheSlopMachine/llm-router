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

// videoPayload forwards the normalized video request as-is, replacing only
// the composite ModelId with the upstream model name.
func videoPayload(req *models.VideoGenerationRequest, modelName string) (map[string]any, error) {
	raw, err := json.Marshal(req)
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
func (c *Client) postVideoJSON(ctx context.Context, apiKey, path string, payload map[string]any, out *models.VideoGenerationResponse) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return classifyHTTPError(resp.StatusCode, string(bodyBytes))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// SubmitVideo posts one video generation request to the upstream.
func (c *Client) SubmitVideo(ctx context.Context, apiKey, modelName string, req *models.VideoGenerationRequest) (*models.VideoGenerationResponse, error) {
	payload, err := videoPayload(req, modelName)
	if err != nil {
		return nil, err
	}
	var out models.VideoGenerationResponse
	if err := c.postVideoJSON(ctx, apiKey, "/videos", payload, &out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, fmt.Errorf("decode response: empty job id")
	}
	if !models.ValidVideoStatus(out.Status) {
		return nil, fmt.Errorf("decode response: unknown job status %q", out.Status)
	}
	return &out, nil
}

// PollVideo fetches one upstream video job status.
func (c *Client) PollVideo(ctx context.Context, apiKey, upstreamJobID string) (*models.VideoGenerationResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/videos/"+upstreamJobID, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
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
	var out models.VideoGenerationResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if !models.ValidVideoStatus(out.Status) {
		return nil, fmt.Errorf("decode response: unknown job status %q", out.Status)
	}
	return &out, nil
}

// VideoContent downloads one upstream video asset.
func (c *Client) VideoContent(ctx context.Context, apiKey, upstreamJobID string, index int) (*models.VideoContentResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/videos/%s/content?index=%d", c.baseURL, upstreamJobID, index), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
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
	video, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, fmt.Errorf("read video: %w", err)
	}
	if len(video) == 0 {
		return nil, fmt.Errorf("decode response: empty video")
	}
	return &models.VideoContentResponse{Video: video, ContentType: resp.Header.Get("Content-Type")}, nil
}

// ListVideoModels fetches the upstream video model catalog. A 404 is not
// an error: upstreams without a video catalog still route video jobs.
func (c *Client) ListVideoModels(ctx context.Context, apiKey string) ([]models.VideoModel, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/videos/models", nil)
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
	var result models.VideoModelsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if result.Data == nil {
		return []models.VideoModel{}, nil
	}
	return result.Data, nil
}
