package proxypool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// detectURL is the lightweight endpoint used to resolve a proxy exit
// country. Overridden in tests to point at a local stub.
var detectURL = "http://ip-api.com/json/?fields=status,countryCode"

// detectTimeout bounds a single exit-country probe.
const detectTimeout = 10 * time.Second

// DetectExitCountry resolves the country a proxyegresses from by querying
// the detection endpoint through the proxy. It reports ground truth for
// the exit IP; list-supplied country metadata is only a seed value.
// A probe failure returns an error and the caller keeps the stored value.
func DetectExitCountry(ctx context.Context, proxyURL string) (string, error) {
	transport, err := TransportFor(proxyURL, detectTimeout)
	if err != nil {
		return "", err
	}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, detectURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
	if err != nil {
		return "", err
	}
	var out struct {
		Status      string `json:"status"`
		CountryCode string `json:"countryCode"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("detect exit country: %w", err)
	}
	if out.Status != "success" || out.CountryCode == "" {
		return "", fmt.Errorf("detect exit country: unsuccessful")
	}
	return NormalizeCountryCode(out.CountryCode), nil
}
