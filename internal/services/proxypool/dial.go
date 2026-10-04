package proxypool

import (
	"net/http"
	"time"

	proxypoollib "github.com/TheSlopMachine/proxypool"
)

// TransportFor builds an HTTP transport for a library-supported proxy URL
// (http/https/socks4/socks5, with optional userinfo credentials). Dialing
// lives in the proxypool library; this wrapper keeps the router-internal
// call boundary stable.
func TransportFor(proxyURL string, timeout time.Duration) (*http.Transport, error) {
	return proxypoollib.TransportFor(proxyURL, timeout)
}
