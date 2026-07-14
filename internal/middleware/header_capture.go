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
	headerCaptureDefaultBodyMax = 2 * 1024 * 1024 // 每次捕获最多存储 2 MiB body 文本
	headerCaptureMaxRead        = 8 * 1024 * 1024 // 为 tee 缓冲请求时的硬上限
)

// CapturedHeaderRequest 是用于调试的一次完整入站 /v1 请求快照
// （头 + body）。值按原样存储——不会对敏感信息脱敏。
// 仅应在可信的 admin 主机上启用。
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
	Headers        map[string][]string `json:"headers"` // 多值，完整值
	Flat           map[string]string   `json:"flat"`    // 每个 key 的第一个值
	Body           string              `json:"body,omitempty"`
	BodyBytes      int                 `json:"body_bytes"`
	BodyTruncated  bool                `json:"body_truncated"`
	BodyCaptureMax int                 `json:"body_capture_max"`
}

// HeaderCapture 在启用时记录完整的入站请求。
// 可安全并发使用。
type HeaderCapture struct {
	mu      sync.Mutex
	enabled bool
	max     int
	bodyMax int
	seq     int64
	items   []CapturedHeaderRequest
}

// NewHeaderCapture 创建一个捕获缓冲区（默认禁用）。
func NewHeaderCapture(maxItems int) *HeaderCapture {
	if maxItems <= 0 {
		maxItems = headerCaptureDefaultMax
	}
	return &HeaderCapture{max: maxItems, bodyMax: headerCaptureDefaultBodyMax}
}

// Middleware 在启用捕获时记录每个 /v1 命中的完整请求。
// Body 会被缓冲并还原，使下游 handler 仍能收到请求体。
func (c *HeaderCapture) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c != nil && c.IsEnabled() {
			r = c.Capture(r)
		}
		next.ServeHTTP(w, r)
	})
}

// IsEnabled 报告捕获功能是否开启。
func (c *HeaderCapture) IsEnabled() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled
}

// SetEnabled 打开或关闭捕获功能。
func (c *HeaderCapture) SetEnabled(on bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.enabled = on
	c.mu.Unlock()
}

// Clear 移除所有已存储的捕获记录。
func (c *HeaderCapture) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.items = nil
	c.mu.Unlock()
}

// Snapshot 返回启用标志和最近捕获记录的副本（最新的在前）。
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

// Latest 返回最新的一条捕获记录（如果存在）。
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

// Capture 存储 r 的完整快照（头 + body），并返回一个 body 已还原的 r，
// 以便链路后续部分继续读取。
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
		// 缓冲整个请求（有上限），以便既能捕获又能转发。
		lr := io.LimitReader(r.Body, headerCaptureMaxRead+1)
		buf, err := io.ReadAll(lr)
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		if err != nil {
			r.Body = io.NopCloser(bytes.NewReader(nil))
			r.ContentLength = 0
		} else {
			if len(buf) > headerCaptureMaxRead {
				// body 极大：为代理保留前 maxRead 字节，标记为已截断。
				buf = buf[:headerCaptureMaxRead]
				truncated = true
			}
			fullBody = buf
			// 存给 admin 用的部分可能会被进一步限制。
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
	// 最新的排在最前面。
	c.items = append([]CapturedHeaderRequest{item}, c.items...)
	if len(c.items) > c.max {
		c.items = c.items[:c.max]
	}
	return r
}
