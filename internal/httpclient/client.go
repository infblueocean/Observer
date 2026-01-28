// Package httpclient provides shared HTTP clients with connection pooling.
//
// IMPORTANT: Callers MUST close response bodies:
//
//	resp, err := httpclient.Default().Get(url)
//	if err != nil {
//	    return err
//	}
//	defer resp.Body.Close()  // Required even on non-2xx status
//
// This package centralizes HTTP client creation to ensure proper connection
// reuse across the application. Creating separate http.Client instances per
// request wastes connection pool resources.
//
// # Usage
//
// For regular requests with a standard timeout:
//
//	resp, err := httpclient.Default().Get(url)
//
// For requests that may take longer (e.g., LLM API calls):
//
//	resp, err := httpclient.LongTimeout().Get(url)
//
// For streaming responses with no timeout:
//
//	resp, err := httpclient.Streaming().Get(url)
//
// # Connection Pooling
//
// The default transport settings allow up to 100 idle connections total,
// and 10 per host. Idle connections are kept alive for 90 seconds.
package httpclient

import (
	"net"
	"net/http"
	"sync"
	"time"
)

var (
	// Shared transport for connection pooling
	sharedTransport *http.Transport
	transportOnce   sync.Once

	// Shared clients
	defaultClient     *http.Client
	longTimeoutClient *http.Client
	streamingClient   *http.Client
	clientOnce        sync.Once
)

// getSharedTransport returns the shared transport with connection pooling settings.
func getSharedTransport() *http.Transport {
	transportOnce.Do(func() {
		sharedTransport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		}
	})
	return sharedTransport
}

func initClients() {
	clientOnce.Do(func() {
		transport := getSharedTransport()

		// Default client: 30s timeout, suitable for most API calls
		defaultClient = &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		}

		// Long timeout client: 2 minute timeout, for LLM API calls
		longTimeoutClient = &http.Client{
			Transport: transport,
			Timeout:   120 * time.Second,
		}

		// Streaming client: no timeout, for SSE/streaming responses
		streamingClient = &http.Client{
			Transport: transport,
			// No timeout - streaming responses can take indefinitely
		}
	})
}

// Default returns a shared HTTP client with a 30-second timeout.
// Suitable for most API calls including RSS feeds, REST APIs, etc.
func Default() *http.Client {
	initClients()
	return defaultClient
}

// LongTimeout returns a shared HTTP client with a 2-minute timeout.
// Suitable for LLM API calls that may take longer to respond.
func LongTimeout() *http.Client {
	initClients()
	return longTimeoutClient
}

// Streaming returns a shared HTTP client with no timeout.
// Suitable for SSE/streaming responses that may continue indefinitely.
// Callers should use context cancellation to control request lifetime.
func Streaming() *http.Client {
	initClients()
	return streamingClient
}
