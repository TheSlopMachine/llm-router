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
	status int
	code   string
	msg    string
}

func (e *wireError) Error() string {
	if e.code != "" {
		return fmt.Sprintf("http %d (%s): %s", e.status, e.code, e.msg)
	}
	return fmt.Sprintf("http %d: %s", e.status, e.msg)
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
	var rdr io.Reader
	if body != nil {
		enc, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(enc)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := apiClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}

// doRaw POSTs raw bytes (plugin sources) and returns status plus body.
func doRaw(method, url string, body []byte) (int, []byte, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := apiClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}

// requireOK decodes a 2xx JSON body into out, else returns a wireError.
func requireOK(status int, raw []byte, out any) error {
	if status < 200 || status >= 300 {
		return &wireError{status: status, code: wireCode(raw), msg: strings.TrimSpace(string(raw))}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func postMultipart(url, model, fileName string, file []byte) (int, []byte, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("model", model)
	fw, err := w.CreateFormFile("file", fileName)
	if err != nil {
		return 0, nil, err
	}
	if _, err := fw.Write(file); err != nil {
		return 0, nil, err
	}
	if err := w.Close(); err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest("POST", url, &buf)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := apiClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}
