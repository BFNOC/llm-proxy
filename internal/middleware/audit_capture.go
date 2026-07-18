package middleware

import (
	"io"
	"sync/atomic"
	"time"
)

const (
	maxFullRecordBodyBytes     = 32 << 20
	maxFullRecordRetainedBytes = 64 << 20
	fullRecordEnqueueTimeout   = time.Second
)

// fullRecordMemoryBudget 限制尚未落库的完整正文总量，避免大请求在异步队列中放大内存占用。
type fullRecordMemoryBudget struct {
	used  int64
	limit int64
}

func (b *fullRecordMemoryBudget) reserve(size int) int {
	if b == nil || size <= 0 {
		return size
	}
	for {
		used := atomic.LoadInt64(&b.used)
		remaining := b.limit - used
		if remaining <= 0 {
			return 0
		}
		granted := int64(size)
		if granted > remaining {
			granted = remaining
		}
		if atomic.CompareAndSwapInt64(&b.used, used, used+granted) {
			return int(granted)
		}
	}
}

func (b *fullRecordMemoryBudget) release(size int64) {
	if b == nil || size <= 0 {
		return
	}
	for {
		used := atomic.LoadInt64(&b.used)
		if used <= 0 {
			return
		}
		released := size
		if released > used {
			released = used
		}
		if atomic.CompareAndSwapInt64(&b.used, used, used-released) {
			return
		}
	}
}

type limitedBodyCapture struct {
	data      []byte
	limit     int
	truncated bool
	budget    *fullRecordMemoryBudget
	reserved  int64
}

func (c *limitedBodyCapture) append(data []byte) {
	remaining := c.limit - len(c.data)
	if remaining <= 0 {
		if len(data) > 0 {
			c.truncated = true
		}
		return
	}
	wanted := len(data)
	if wanted > remaining {
		wanted = remaining
		c.truncated = true
	}
	granted := c.budget.reserve(wanted)
	if granted < wanted {
		c.truncated = true
	}
	if granted > 0 {
		c.data = append(c.data, data[:granted]...)
		c.reserved += int64(granted)
	}
}

func (c *limitedBodyCapture) takeString() (string, int64) {
	value := string(c.data)
	reserved := c.reserved
	c.data = nil
	c.reserved = 0
	return value, reserved
}

func (c *limitedBodyCapture) releaseReserved() {
	if c == nil {
		return
	}
	c.budget.release(c.reserved)
	c.data = nil
	c.reserved = 0
}

type bodyCaptureReadCloser struct {
	io.ReadCloser
	capture *limitedBodyCapture
	read    bool
}

func (r *bodyCaptureReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.read = true
		r.capture.append(p[:n])
	}
	return n, err
}
