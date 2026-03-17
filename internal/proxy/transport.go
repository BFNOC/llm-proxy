package proxy

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	xproxy "golang.org/x/net/proxy"
)

// transportCache 按 proxyURL 缓存 *http.Transport，
// 相同代理配置的上游复用同一 transport，避免重复创建连接池。
var (
	transportCache sync.Map // map[string]*http.Transport
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

// RemoveTransport 从缓存中移除指定 proxyURL 的 transport 并关闭其空闲连接。
// 应在删除或更新上游代理配置时调用，避免连接池泄漏。
func RemoveTransport(proxyURL string) {
	if v, ok := transportCache.LoadAndDelete(proxyURL); ok {
		v.(*http.Transport).CloseIdleConnections()
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
