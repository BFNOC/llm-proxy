package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTransport_EmptyUsesProxyFromEnvironment(t *testing.T) {
	// 空 proxy_url 应返回使用 http.ProxyFromEnvironment 的 transport
	tr, err := BuildTransport("")
	require.NoError(t, err)
	require.NotNil(t, tr)
	assert.NotNil(t, tr.Proxy, "empty proxy_url should set Proxy to ProxyFromEnvironment, not nil")
}

func TestBuildTransport_HTTPProxy(t *testing.T) {
	tr, err := BuildTransport("http://proxy.example.com:8080")
	require.NoError(t, err)
	require.NotNil(t, tr)
	assert.NotNil(t, tr.Proxy, "HTTP proxy should set Proxy function")
}

func TestBuildTransport_SOCKS5Proxy(t *testing.T) {
	tr, err := BuildTransport("socks5://127.0.0.1:1080")
	require.NoError(t, err)
	require.NotNil(t, tr)
	// SOCKS5 transport 使用自定义 dialer，Proxy 字段为 nil
	assert.Nil(t, tr.Proxy, "SOCKS5 proxy should not set Proxy function (uses custom dialer)")
}

func TestBuildTransport_InvalidScheme(t *testing.T) {
	_, err := BuildTransport("ftp://bad.example.com")
	assert.Error(t, err, "unsupported scheme should return error")
	assert.Contains(t, err.Error(), "unsupported proxy scheme")
}

func TestBuildTransport_CacheReuse(t *testing.T) {
	// 清除缓存以隔离测试
	transportCache.Delete("http://cache-test.example.com:8080")
	defer transportCache.Delete("http://cache-test.example.com:8080")

	t1, err := BuildTransport("http://cache-test.example.com:8080")
	require.NoError(t, err)
	t2, err := BuildTransport("http://cache-test.example.com:8080")
	require.NoError(t, err)
	assert.Same(t, t1, t2, "same proxy_url should return the same cached transport")
}

func TestRemoveTransport_CleansCache(t *testing.T) {
	proxyURL := "http://remove-test.example.com:9999"
	// 确保缓存中有这个 transport
	tr, err := BuildTransport(proxyURL)
	require.NoError(t, err)
	require.NotNil(t, tr)
	_, ok := transportCache.Load(proxyURL)
	assert.True(t, ok, "transport should be cached before removal")

	RemoveTransport(proxyURL)
	_, ok = transportCache.Load(proxyURL)
	assert.False(t, ok, "transport should be removed from cache")
}

func TestRemoveTransport_NonExistentIsNoop(t *testing.T) {
	// 移除不存在的 key 不应 panic
	RemoveTransport("http://does-not-exist.example.com:1234")
}

func TestProbeUpstream_NoFollowRedirect(t *testing.T) {
	// 重定向目标：如果被访问说明 prober 跟随了 302
	redirectHit := false
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL+"/v1/models", http.StatusFound)
	}))
	defer upstream.Close()

	prober := &UpstreamProber{timeout: 5 * 1e9} // 5s
	result := prober.probeUpstream(upstream.URL, "")
	// 302 < 500，应视为可达
	assert.True(t, result, "302 should be treated as reachable")
	// 关键断言：重定向目标绝对不应该被访问
	assert.False(t, redirectHit, "prober should NOT follow redirects (SSRF prevention)")
}

func TestBuildTransport_EmptyProxyURL_HasValidProxy(t *testing.T) {
	// 验证空 proxy_url 返回的 transport 的 Proxy 函数行为与 ProxyFromEnvironment 一致
	tr, err := BuildTransport("")
	require.NoError(t, err)
	// 用一个普通请求测试 Proxy 函数不会 panic
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	proxyURL, err := tr.Proxy(req)
	// 没有设环境变量时返回 nil（直连），有的话返回代理 URL，两种都是合法行为
	assert.NoError(t, err)
	_ = proxyURL // 不关心具体值，只要不 panic 且不报错
}
