package proxy

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// ActiveUpstream holds the currently selected upstream endpoint and its key.
type ActiveUpstream struct {
	BaseURL *url.URL
	APIKey  string
}

// DynamicProxy is a reverse proxy whose upstream can be hot-swapped at
// runtime without restarting. Upstream switches are lock-free via atomic.Value.
type DynamicProxy struct {
	activeUpstream atomic.Value // stores *ActiveUpstream
	transport      *http.Transport
}

// NewDynamicProxy creates a DynamicProxy with a pre-configured transport.
func NewDynamicProxy() *DynamicProxy {
	dp := &DynamicProxy{
		transport: newProxyTransport(),
	}
	return dp
}

// SetActiveUpstream atomically replaces the upstream the proxy forwards to.
func (dp *DynamicProxy) SetActiveUpstream(baseURL *url.URL, apiKey string) {
	dp.activeUpstream.Store(&ActiveUpstream{BaseURL: baseURL, APIKey: apiKey})
}

// GetActiveUpstream returns the currently active upstream, or nil if none has
// been set yet.
func (dp *DynamicProxy) GetActiveUpstream() *ActiveUpstream {
	v := dp.activeUpstream.Load()
	if v == nil {
		return nil
	}
	return v.(*ActiveUpstream)
}

// ServeHTTP implements http.Handler. It forwards the request to the active
// upstream, rewriting the scheme and host while preserving the original path
// so that /v1/... routes reach the upstream as-is.
func (dp *DynamicProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	active := dp.GetActiveUpstream()
	if active == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "no active upstream available"}) //nolint:errcheck
		return
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = active.BaseURL.Scheme
			req.URL.Host = active.BaseURL.Host
			req.Host = active.BaseURL.Host
			// Path stays as-is (/v1/... -> /v1/...)
		},
		Transport: dp.transport,
		ModifyResponse: func(resp *http.Response) error {
			ct := resp.Header.Get("Content-Type")
			if ct == "text/event-stream" || strings.Contains(ct, "text/event-stream") {
				resp.Header.Set("Cache-Control", "no-cache")
				resp.Header.Set("X-Accel-Buffering", "no")
				resp.Header.Del("Content-Length")
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("proxy error", "error", err, "path", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "bad gateway: " + err.Error()}) //nolint:errcheck
		},
	}
	proxy.ServeHTTP(w, r)
}

// newProxyTransport returns an *http.Transport tuned for proxying LLM API
// requests, including long-running streaming responses.
func newProxyTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 3 * time.Minute,
		DisableCompression:    true,
	}
}
