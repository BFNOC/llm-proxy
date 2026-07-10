package proxy

import "net/http"

// AuthMode values for Anthropic credential header selection.
const (
	AuthModeAPIKey = "api_key" // x-api-key (default)
	AuthModeOAuth  = "oauth"   // Authorization: Bearer (Claude OAuth / sk-ant-oai)
)

// RewriteAuthHeaders rewrites authentication headers on the outgoing request so
// that the upstream provider receives the correct credential format.
//
// OpenAI style:
//   - Sets "Authorization: Bearer {upstreamKey}"
//   - Removes "x-api-key"
//
// Anthropic style (authMode=api_key, default):
//   - Sets "x-api-key: {upstreamKey}"
//   - Removes "Authorization"
//
// Anthropic style (authMode=oauth):
//   - Sets "Authorization: Bearer {upstreamKey}"
//   - Removes "x-api-key"
func RewriteAuthHeaders(r *http.Request, style ProviderStyle, upstreamKey string, authMode string) {
	if upstreamKey == "" {
		r.Header.Del("Authorization")
		r.Header.Del("x-api-key")
		return
	}
	switch style {
	case StyleAnthropic:
		if authMode == AuthModeOAuth {
			r.Header.Set("Authorization", "Bearer "+upstreamKey)
			r.Header.Del("x-api-key")
		} else {
			r.Header.Set("x-api-key", upstreamKey)
			r.Header.Del("Authorization")
		}
	default: // StyleOpenAI
		r.Header.Set("Authorization", "Bearer "+upstreamKey)
		r.Header.Del("x-api-key")
	}
}
