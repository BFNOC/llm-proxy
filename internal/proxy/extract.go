package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// ExtractDownstreamKey retrieves the raw API key supplied by the downstream
// client.
//
// OpenAI style: "Authorization: Bearer {key}"
// Anthropic style: "x-api-key: {key}"
//
// Returns an empty string when no key is present.
func ExtractDownstreamKey(r *http.Request, style ProviderStyle) string {
	switch style {
	case StyleAnthropic:
		return r.Header.Get("x-api-key")
	default: // StyleOpenAI
		authHeader := r.Header.Get("Authorization")
		const bearerPrefix = "Bearer "
		if strings.HasPrefix(authHeader, bearerPrefix) {
			return strings.TrimPrefix(authHeader, bearerPrefix)
		}
		return ""
	}
}

// HashKey returns the SHA-256 hex digest of the given key.  Use this to store
// or log key identifiers without exposing the raw secret.
func HashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
