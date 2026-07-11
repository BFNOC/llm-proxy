package proxy

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldFailoverStatus(t *testing.T) {
	assert.True(t, shouldFailoverStatus(401))
	assert.True(t, shouldFailoverStatus(403))
	assert.True(t, shouldFailoverStatus(429))
	assert.True(t, shouldFailoverStatus(502))
	assert.True(t, shouldFailoverStatus(503))
	assert.True(t, shouldFailoverStatus(504))
	assert.False(t, shouldFailoverStatus(200))
	assert.False(t, shouldFailoverStatus(400))
	assert.False(t, shouldFailoverStatus(500))
}

func TestClassifyUpstreamFailure_Quota(t *testing.T) {
	body := []byte(`{"error":{"code":"insufficient_quota","message":"You exceeded your current quota"}}`)
	kind := classifyUpstreamFailure(429, http.Header{}, body)
	assert.Equal(t, FailureQuota, kind)
	assert.True(t, shouldCountKeyFailure(kind))
}

func TestClassifyUpstreamFailure_RateLimitWithRetryAfter(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "30")
	h.Set("x-ratelimit-remaining-requests", "0")
	kind := classifyUpstreamFailure(429, h, []byte(`{"error":{"message":"rate limit"}}`))
	assert.Equal(t, FailureRateLimit, kind)
}

func TestClassifyUpstreamFailure_AuthAndServer(t *testing.T) {
	assert.Equal(t, FailureAuth, classifyUpstreamFailure(401, nil, nil))
	assert.Equal(t, FailureServerError, classifyUpstreamFailure(503, nil, nil))
	assert.False(t, shouldCountKeyFailure(FailureServerError))
}

func TestPeekResponseBody_PreservesCloser(t *testing.T) {
	closed := false
	orig := &closeTracker{Reader: strings.NewReader("hello world extra"), onClose: func() { closed = true }}
	resp := &http.Response{Body: orig}
	got := peekResponseBody(resp, 5)
	assert.Equal(t, "hello", string(got))
	all, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "hello world extra", string(all))
	require.NoError(t, resp.Body.Close())
	assert.True(t, closed, "original closer must be invoked")
}

func TestStripRequestHopByHop(t *testing.T) {
	h := http.Header{}
	h.Set("Connection", "Keep-Alive, X-Foo")
	h.Set("Keep-Alive", "timeout=5")
	h.Set("X-Foo", "bar")
	h.Set("Authorization", "Bearer sk")
	h.Set("Content-Type", "application/json")
	stripRequestHopByHop(h)
	assert.Empty(t, h.Get("Connection"))
	assert.Empty(t, h.Get("Keep-Alive"))
	assert.Empty(t, h.Get("X-Foo"))
	assert.Equal(t, "Bearer sk", h.Get("Authorization"))
	assert.Equal(t, "application/json", h.Get("Content-Type"))
}

type closeTracker struct {
	io.Reader
	onClose func()
}

func (c *closeTracker) Close() error {
	if c.onClose != nil {
		c.onClose()
	}
	return nil
}
