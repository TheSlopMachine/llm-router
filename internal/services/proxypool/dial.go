package proxypool

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// TransportFor builds an HTTP transport for a library-supported proxy URL.
func TransportFor(proxyURL string, timeout time.Duration) (*http.Transport, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}
	if u.Scheme != "http" || u.Host == "" {
		return nil, fmt.Errorf("unsupported proxy URL %q", proxyURL)
	}
	return &http.Transport{
		Proxy:       http.ProxyURL(u),
		DialContext: (&net.Dialer{Timeout: timeout}).DialContext,
	}, nil
}
