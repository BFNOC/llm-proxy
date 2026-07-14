package proxy

import (
	"net/http"
	"strings"
)

// ProviderStyle 表示下游客户端使用的 API 风格。
type ProviderStyle string

const (
	StyleOpenAI    ProviderStyle = "openai"
	StyleAnthropic ProviderStyle = "anthropic"
)

// DetectProviderStyle 推断传入请求使用的是哪种服务商 API 风格。检测顺序：
//  1. 路径以 /v1/messages 开头 -> Anthropic
//  2. 存在 x-api-key 请求头   -> Anthropic
//  3. 存在 anthropic-version 请求头 -> Anthropic
//  4. 默认                    -> OpenAI
func DetectProviderStyle(r *http.Request) ProviderStyle {
	if strings.HasPrefix(r.URL.Path, "/v1/messages") {
		return StyleAnthropic
	}
	if r.Header.Get("x-api-key") != "" {
		return StyleAnthropic
	}
	if r.Header.Get("anthropic-version") != "" {
		return StyleAnthropic
	}
	return StyleOpenAI
}
