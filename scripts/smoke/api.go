package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// apiClient is the harness HTTP client: generous per-request timeout,
// sequential calls only.
var apiClient = &http.Client{Timeout: 120 * time.Second}

// wireError carries an HTTP failure with the OpenAI wire code when the
// envelope carries one.
type wireError struct {
	status  int
	code    string
	message string
}

func (e *wireError) Error() string {
	if e.code != "" {
		return fmt.Sprintf("http %d (%s): %s", e.status, e.code, e.message)
	}
	return fmt.Sprintf("http %d: %s", e.status, e.message)
}

// wireCode extracts error.code from an OpenAI error envelope.
func wireCode(body []byte) string {
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	return envelope.Error.Code
}

func doJSON(method, url string, body any) (int, []byte, error) {
	var bodyReader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		bodyReader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := apiClient.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, raw, nil
}

// doRaw POSTs raw bytes (plugin sources) and returns status plus body.
func doRaw(method, url string, body []byte) (int, []byte, error) {
	request, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	response, err := apiClient.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, raw, nil
}

// requireOK decodes a 2xx JSON body into output, else returns a wireError.
func requireOK(status int, raw []byte, output any) error {
	if status < 200 || status >= 300 {
		return &wireError{status: status, code: wireCode(raw), message: strings.TrimSpace(string(raw))}
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(raw, output); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func postMultipart(url, model, fileName string, file []byte, format string) (int, []byte, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	_ = writer.WriteField("model", model)
	_ = writer.WriteField("response_format", format)
	fileWriter, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return 0, nil, err
	}
	if _, err := fileWriter.Write(file); err != nil {
		return 0, nil, err
	}
	if err := writer.Close(); err != nil {
		return 0, nil, err
	}
	request, err := http.NewRequest("POST", url, &buffer)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := apiClient.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, raw, nil
}
