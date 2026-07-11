package proxy

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/Instawork/llm-proxy/internal/proxy/tlsfingerprint"
	xproxy "golang.org/x/net/proxy"
)

// transportCache 按 proxyURL 缓存 *http.Transport，
// 相同代理配置的上游复用同一 transport，避免重复创建连接池。
var (
	transportCache     sync.Map // map[string]*http.Transport
	utlsTransportCache sync.Map // map[string]*http.Transport — Node/Claude Code TLS fingerprint
)

// BuildTransport 根据 proxyURL 创建或返回缓存的 *http.Transport。
// 空字符串表示使用环境代理（HTTP_PROXY 等）；支持 http/https/socks5 协议。
func BuildTransport(proxyURL string) (*http.Transport, error) {
	// 先查缓存
	if v, ok := transportCache.Load(proxyURL); ok {
		return v.(*http.Transport), nil
	}

	var t *http.Transport
	if proxyURL == "" {
		// 空值保留历史行为：从环境变量读取代理配置
		t = newBaseTransport()
		t.Proxy = http.ProxyFromEnvironment
	} else {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse proxy URL: %w", err)
		}

		switch parsed.Scheme {
		case "http", "https":
			t = newBaseTransport()
			t.Proxy = http.ProxyURL(parsed)
		case "socks5":
			dialer, err := xproxy.FromURL(parsed, xproxy.Direct)
			if err != nil {
				return nil, fmt.Errorf("create SOCKS5 dialer: %w", err)
			}
			t = newBaseTransport()
			// SOCKS5 dialer 不支持 DialContext，使用 Dial 回退
			if cd, ok := dialer.(xproxy.ContextDialer); ok {
				t.DialContext = cd.DialContext
			} else {
				t.DialContext = nil
				t.Dial = dialer.Dial //nolint:staticcheck
			}
		default:
			return nil, fmt.Errorf("unsupported proxy scheme: %s", parsed.Scheme)
		}
	}

	// 存入缓存（可能有并发写入，但 sync.Map 保证安全）
	actual, _ := transportCache.LoadOrStore(proxyURL, t)
	return actual.(*http.Transport), nil
}

// BuildTransportUTLS 返回带 Claude Code / Node.js 24 TLS 指纹的 *http.Transport。
// 用于 OAuth (sk-ant-oat) 上游探测与转发，对齐 sub2api DoWithTLS 行为。
// 支持空 / http(s) / socks5 代理；与 BuildTransport 使用独立缓存键。
func BuildTransportUTLS(proxyURL string) (*http.Transport, error) {
	cacheKey := "utls:" + proxyURL
	if v, ok := utlsTransportCache.Load(cacheKey); ok {
		return v.(*http.Transport), nil
	}

	profile := tlsfingerprint.NodeClaudeCodeProfile()
	t := newBaseTransport()
	// uTLS ClientHello uses ALPN http/1.1 only; disable automatic HTTP/2 upgrade.
	t.ForceAttemptHTTP2 = false
	// DialTLSContext owns TLS; clear Proxy so net/http does not double-wrap TLS.
	t.Proxy = nil
	t.DialTLSContext = nil
	t.TLSClientConfig = nil

	if proxyURL == "" {
		d := tlsfingerprint.NewDialer(profile, (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext)
		t.DialTLSContext = d.DialTLSContext
	} else {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse proxy URL: %w", err)
		}
		switch parsed.Scheme {
		case "http", "https":
			d := tlsfingerprint.NewHTTPProxyDialer(profile, parsed)
			t.DialTLSContext = d.DialTLSContext
		case "socks5":
			d := tlsfingerprint.NewSOCKS5ProxyDialer(profile, parsed)
			t.DialTLSContext = d.DialTLSContext
		default:
			return nil, fmt.Errorf("unsupported proxy scheme for utls: %s", parsed.Scheme)
		}
	}

	actual, _ := utlsTransportCache.LoadOrStore(cacheKey, t)
	return actual.(*http.Transport), nil
}

// RemoveTransport 从缓存中移除指定 proxyURL 的 transport 并关闭其空闲连接。
// 应在删除或更新上游代理配置时调用，避免连接池泄漏。
func RemoveTransport(proxyURL string) {
	if v, ok := transportCache.LoadAndDelete(proxyURL); ok {
		v.(*http.Transport).CloseIdleConnections()
	}
	if v, ok := utlsTransportCache.LoadAndDelete("utls:" + proxyURL); ok {
		v.(*http.Transport).CloseIdleConnections()
	}
}

// TransportPoolStats 返回连接池的基本统计信息。
func TransportPoolStats() map[string]interface{} {
	count := 0
	transportCache.Range(func(_, _ any) bool {
		count++
		return true
	})
	utlsCount := 0
	utlsTransportCache.Range(func(_, _ any) bool {
		utlsCount++
		return true
	})
	return map[string]interface{}{
		"cached_transports":      count,
		"cached_utls_transports": utlsCount,
	}
}

// newBaseTransport 返回一个预配置的 *http.Transport，参数与原 newProxyTransport 一致。
func newBaseTransport() *http.Transport {
	return &http.Transport{
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
