package middleware

import (
	"bytes"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	headerCaptureDefaultMax     = 20
	headerCaptureDefaultBodyMax = 2 * 1024 * 1024 // store up to 2 MiB body text per capture
	headerCaptureMaxRead        = 8 * 1024 * 1024 // hard cap when buffering request for tee
)

// CapturedHeaderRequest is one full inbound /v1 request snapshot for debugging
// (headers + body). Values are stored as received — secrets are NOT redacted.
// Only enable on a trusted admin host.
type CapturedHeaderRequest struct {
	ID             int64               `json:"id"`
	Time           time.Time           `json:"time"`
	Method         string              `json:"method"`
	Path           string              `json:"path"`
	Query          string              `json:"query,omitempty"`
	Proto          string              `json:"proto,omitempty"`
	Host           string              `json:"host,omitempty"`
	RemoteAddr     string              `json:"remote_addr,omitempty"`
	ContentLength  int64               `json:"content_length"`
	Headers        map[string][]string `json:"headers"` // multi-value, full values
	Flat           map[string]string   `json:"flat"`    // first value per key
	Body           string              `json:"body,omitempty"`
	BodyBytes      int                 `json:"body_bytes"`
	BodyTruncated  bool                `json:"body_truncated"`
	BodyCaptureMax int                 `json:"body_capture_max"`
}

// HeaderCapture records full inbound requests while enabled.
// Safe for concurrent use.
type HeaderCapture struct {
	mu      sync.Mutex
	enabled bool
	max     int
	bodyMax int
	seq     int64
	items   []CapturedHeaderRequest
}

// NewHeaderCapture creates a capture buffer (disabled by default).
func NewHeaderCapture(maxItems int) *HeaderCapture {
	if maxItems <= 0 {
		maxItems = headerCaptureDefaultMax
	}
	return &HeaderCapture{max: maxItems, bodyMax: headerCaptureDefaultBodyMax}
}

// Middleware records the full request for every /v1 hit when capture is enabled.
// Body is buffered and restored so downstream handlers still receive the payload.
func (c *HeaderCapture) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c != nil && c.IsEnabled() {
			r = c.Capture(r)
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

// Capture stores a full snapshot of r (headers + body) and returns r with a
// restored body so the rest of the chain can read it.
func (c *HeaderCapture) Capture(r *http.Request) *http.Request {
	if c == nil || r == nil {
		return r
	}

	bodyMax := c.bodyMax
	if bodyMax <= 0 {
		bodyMax = headerCaptureDefaultBodyMax
	}

	headers := make(map[string][]string, len(r.Header))
	flat := make(map[string]string, len(r.Header))
	for k, vv := range r.Header {
		copied := make([]string, len(vv))
		copy(copied, vv)
		headers[k] = copied
		if len(copied) > 0 {
			flat[k] = copied[0]
		}
	}

	var (
		bodyStr    string
		bodyBytes  int
		truncated  bool
		fullBody   []byte
		origLength = r.ContentLength
	)

	if r.Body != nil {
		// Buffer the whole request (capped) so we can both capture and forward.
		lr := io.LimitReader(r.Body, headerCaptureMaxRead+1)
		buf, err := io.ReadAll(lr)
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		if err != nil {
			r.Body = io.NopCloser(bytes.NewReader(nil))
			r.ContentLength = 0
		} else {
			if len(buf) > headerCaptureMaxRead {
				// Extremely large body: keep first maxRead for proxy, mark truncated.
				buf = buf[:headerCaptureMaxRead]
				truncated = true
			}
			fullBody = buf
			// What we store for admin may be further limited.
			store := buf
			if len(store) > bodyMax {
				store = store[:bodyMax]
				truncated = true
			}
			bodyBytes = len(store)
			bodyStr = string(store)

			r.Body = io.NopCloser(bytes.NewReader(fullBody))
			r.ContentLength = int64(len(fullBody))
			r.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(fullBody)), nil
			}
		}
	}

	item := CapturedHeaderRequest{
		Time:           time.Now(),
		Method:         r.Method,
		Path:           r.URL.Path,
		Query:          r.URL.RawQuery,
		Proto:          r.Proto,
		Host:           r.Host,
		RemoteAddr:     r.RemoteAddr,
		ContentLength:  origLength,
		Headers:        headers,
		Flat:           flat,
		Body:           bodyStr,
		BodyBytes:      bodyBytes,
		BodyTruncated:  truncated,
		BodyCaptureMax: bodyMax,
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled {
		return r
	}
	c.seq++
	item.ID = c.seq
	// Newest first.
	c.items = append([]CapturedHeaderRequest{item}, c.items...)
	if len(c.items) > c.max {
		c.items = c.items[:c.max]
	}
	return r
}
