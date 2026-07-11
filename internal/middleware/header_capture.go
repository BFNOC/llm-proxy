package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	headerCaptureDefaultMax = 20
	// Sensitive headers are stored in redacted form only.
)

// CapturedHeaderRequest is one inbound /v1 request snapshot for debugging
// (e.g. Claude Code client fingerprints).
type CapturedHeaderRequest struct {
	ID        int64               `json:"id"`
	Time      time.Time           `json:"time"`
	Method    string              `json:"method"`
	Path      string              `json:"path"`
	Query     string              `json:"query,omitempty"`
	RemoteAddr string             `json:"remote_addr,omitempty"`
	Headers   map[string][]string `json:"headers"` // multi-value, secrets redacted
	// Flat is single-value map (first value) for easy copy into curl/tests.
	Flat map[string]string `json:"flat"`
}

// HeaderCapture records inbound request headers while enabled.
// Safe for concurrent use.
type HeaderCapture struct {
	mu      sync.Mutex
	enabled bool
	max     int
	seq     int64
	items   []CapturedHeaderRequest
}

// NewHeaderCapture creates a capture buffer (disabled by default).
func NewHeaderCapture(maxItems int) *HeaderCapture {
	if maxItems <= 0 {
		maxItems = headerCaptureDefaultMax
	}
	return &HeaderCapture{max: maxItems}
}

// Middleware records headers for every request that reaches the proxy chain
// when capture is enabled. It never blocks or alters the request.
func (c *HeaderCapture) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c != nil && c.IsEnabled() {
			c.Capture(r)
		}
		next.ServeHTTP(w, r)
	})
}

// IsEnabled reports whether capture is on.
func (c *HeaderCapture) IsEnabled() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled
}

// SetEnabled turns capture on or off.
func (c *HeaderCapture) SetEnabled(on bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.enabled = on
	c.mu.Unlock()
}

// Clear removes all stored captures.
func (c *HeaderCapture) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.items = nil
	c.mu.Unlock()
}

// Snapshot returns enabled flag and a copy of recent captures (newest first).
func (c *HeaderCapture) Snapshot() (enabled bool, items []CapturedHeaderRequest) {
	if c == nil {
		return false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	enabled = c.enabled
	items = make([]CapturedHeaderRequest, len(c.items))
	copy(items, c.items)
	return enabled, items
}

// Latest returns the newest capture, if any.
func (c *HeaderCapture) Latest() (CapturedHeaderRequest, bool) {
	if c == nil {
		return CapturedHeaderRequest{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) == 0 {
		return CapturedHeaderRequest{}, false
	}
	return c.items[0], true
}

// Capture stores a redacted header snapshot of r.
func (c *HeaderCapture) Capture(r *http.Request) {
	if c == nil || r == nil {
		return
	}
	headers := make(map[string][]string, len(r.Header))
	flat := make(map[string]string, len(r.Header))
	for k, vv := range r.Header {
		redacted := make([]string, len(vv))
		for i, v := range vv {
			redacted[i] = redactHeaderValue(k, v)
		}
		headers[k] = redacted
		if len(redacted) > 0 {
			flat[k] = redacted[0]
		}
	}

	item := CapturedHeaderRequest{
		Time:       time.Now(),
		Method:     r.Method,
		Path:       r.URL.Path,
		Query:      r.URL.RawQuery,
		RemoteAddr: r.RemoteAddr,
		Headers:    headers,
		Flat:       flat,
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled {
		return
	}
	c.seq++
	item.ID = c.seq
	// Newest first.
	c.items = append([]CapturedHeaderRequest{item}, c.items...)
	if len(c.items) > c.max {
		c.items = c.items[:c.max]
	}
}

func redactHeaderValue(name, value string) string {
	n := strings.ToLower(name)
	switch n {
	case "authorization":
		return redactAuthorization(value)
	case "x-api-key", "api-key", "proxy-authorization":
		return redactSecret(value)
	case "cookie", "set-cookie":
		if value == "" {
			return ""
		}
		return "[redacted cookie]"
	default:
		return value
	}
}

func redactAuthorization(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	parts := strings.SplitN(v, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return "Bearer " + redactSecret(parts[1])
	}
	return redactSecret(v)
}

func redactSecret(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if len(v) <= 12 {
		return v[:2] + "…" + v[len(v)-2:]
	}
	return v[:8] + "…" + v[len(v)-6:]
}
