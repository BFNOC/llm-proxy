package proxy

import (
	"net/http"
	"strings"
)

// ProviderStyle represents the API style used by a downstream client.
type ProviderStyle string

const (
	StyleOpenAI    ProviderStyle = "openai"
	StyleAnthropic ProviderStyle = "anthropic"
)

// DetectProviderStyle infers which provider API style the incoming request is
// using.  Detection order:
//  1. Path starts with /v1/messages -> Anthropic
//  2. Header x-api-key present      -> Anthropic
//  3. Header anthropic-version present -> Anthropic
//  4. Default                        -> OpenAI
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
