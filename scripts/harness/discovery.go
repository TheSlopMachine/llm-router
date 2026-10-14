package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// harnessMockType is the plugin type key of the mock plugin uploaded by CI.
const harnessMockType = "harness-mock"

var httpClient = &http.Client{Timeout: 60 * time.Second}

// requestJSON performs one JSON request against the admin listener. A nil
// output discards the body; a non-2xx status becomes an error with the body.
func requestJSON(method, url string, body any, output any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, url, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s %s: http %d: %s", method, url, response.StatusCode, strings.TrimSpace(string(raw)))
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(raw, output); err != nil {
		return fmt.Errorf("%s %s: decode: %w", method, url, err)
	}
	return nil
}

// ensureBootstrapped creates the single dashboard account the first time the
// backend starts, as smoke does. The admin listener itself needs no login.
func ensureBootstrapped(base string) error {
	var status struct {
		Bootstrapped bool `json:"bootstrapped"`
	}
	if err := requestJSON("GET", base+"/api/llm-router/status", nil, &status); err != nil {
		return err
	}
	if status.Bootstrapped {
		return nil
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	return requestJSON("POST", base+"/api/llm-router/bootstrap", map[string]any{
		"username": "harness",
		"password": hex.EncodeToString(random[:]),
	}, nil)
}

// discoverTarget finds the harness-mock provider instance and checks that the
// harness's model is declared and enabled, so a missing model fails the run
// before any harness starts.
func discoverTarget(base, bareModel string) (routerTarget, error) {
	var providers []struct {
		ID      string `json:"id"`
		TypeKey string `json:"type_key"`
	}
	if err := requestJSON("GET", base+"/api/llm-router/dashboard/providers", nil, &providers); err != nil {
		return routerTarget{}, err
	}
	providerID := ""
	for _, provider := range providers {
		if provider.TypeKey == harnessMockType {
			providerID = provider.ID
			break
		}
	}
	if providerID == "" {
		return routerTarget{}, fmt.Errorf("no %s provider instance: run make upload-plugins first", harnessMockType)
	}

	var models []struct {
		Name     string `json:"name"`
		Disabled bool   `json:"disabled"`
	}
	if err := requestJSON("GET", base+"/api/llm-router/dashboard/providers/"+providerID+"/models", nil, &models); err != nil {
		return routerTarget{}, err
	}
	var declared []string
	for _, model := range models {
		declared = append(declared, model.Name)
		if model.Name == bareModel && !model.Disabled {
			return routerTarget{
				adminURL:    base,
				providerID:  providerID,
				bareModel:   bareModel,
				routerModel: providerID + "/" + bareModel,
			}, nil
		}
	}
	return routerTarget{}, fmt.Errorf("model %q is not enabled on provider %s (declared: %s)",
		bareModel, providerID, strings.Join(declared, ", "))
}
