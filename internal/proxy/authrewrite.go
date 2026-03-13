package proxy

import "net/http"

// RewriteAuthHeaders rewrites authentication headers on the outgoing request so
// that the upstream provider receives the correct credential format.
//
// OpenAI style:
//   - Sets "Authorization: Bearer {upstreamKey}"
//   - Removes "x-api-key"
//
// Anthropic style:
//   - Sets "x-api-key: {upstreamKey}"
//   - Removes "Authorization"
func RewriteAuthHeaders(r *http.Request, style ProviderStyle, upstreamKey string) {
	switch style {
	case StyleAnthropic:
		r.Header.Set("x-api-key", upstreamKey)
		r.Header.Del("Authorization")
	default: // StyleOpenAI
		r.Header.Set("Authorization", "Bearer "+upstreamKey)
		r.Header.Del("x-api-key")
	}
}
