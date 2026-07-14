package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestClassifierMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		headers        map[string]string
		expectStatus   int
		expectStyle    proxy.ProviderStyle
		expectHashSet  bool // whether key hash should be non-empty in context
		expectNextCall bool
	}{
		{
			name:   "OpenAI style - Authorization Bearer",
			method: http.MethodPost,
			path:   "/v1/chat/completions",
			headers: map[string]string{
				"Authorization": "Bearer sk-test-openai-key",
			},
			expectStatus:   http.StatusOK,
			expectStyle:    proxy.StyleOpenAI,
			expectHashSet:  true,
			expectNextCall: true,
		},
		{
			name:   "Anthropic style - x-api-key header",
			method: http.MethodPost,
			path:   "/v1/chat/completions",
			headers: map[string]string{
				"x-api-key": "sk-test-anthropic-key",
			},
			expectStatus:   http.StatusOK,
			expectStyle:    proxy.StyleAnthropic,
			expectHashSet:  true,
			expectNextCall: true,
		},
		{
			name:   "Anthropic style - messages path",
			method: http.MethodPost,
			path:   "/v1/messages",
			headers: map[string]string{
				"Authorization": "Bearer sk-test-messages-key",
			},
			expectStatus:   http.StatusOK,
			expectStyle:    proxy.StyleAnthropic,
			expectHashSet:  true,
			expectNextCall: true,
		},
		{
			name:   "Anthropic style - anthropic-version header",
			method: http.MethodPost,
			path:   "/v1/chat/completions",
			headers: map[string]string{
				"anthropic-version": "2023-06-01",
				"Authorization":     "Bearer sk-test-anthver-key",
			},
			expectStatus:   http.StatusOK,
			expectStyle:    proxy.StyleAnthropic,
			expectHashSet:  true,
			expectNextCall: true,
		},
		{
			name:           "Missing key returns 401",
			method:         http.MethodPost,
			path:           "/v1/chat/completions",
			headers:        map[string]string{},
			expectStatus:   http.StatusUnauthorized,
			expectNextCall: false,
		},
		{
			name:   "Authorization header without Bearer prefix returns 401",
			method: http.MethodPost,
			path:   "/v1/chat/completions",
			headers: map[string]string{
				"Authorization": "Basic dXNlcjpwYXNz",
			},
			expectStatus:   http.StatusUnauthorized,
			expectNextCall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedStyle proxy.ProviderStyle
			var capturedHash string
			nextCalled := false

			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				capturedStyle = StyleFromContext(r.Context())
				capturedHash = KeyHashFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			})

			mw := RequestClassifierMiddleware()(next)

			req := httptest.NewRequest(tt.method, tt.path, nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()

			mw.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectStatus, rec.Code)
			assert.Equal(t, tt.expectNextCall, nextCalled, "next handler call mismatch")

			if tt.expectNextCall {
				assert.Equal(t, tt.expectStyle, capturedStyle, "provider style mismatch")
				if tt.expectHashSet {
					assert.NotEmpty(t, capturedHash, "key hash should be set")
				}
			}

			if !tt.expectNextCall {
				// Verify JSON error body on 401
				var body map[string]string
				err := json.NewDecoder(rec.Body).Decode(&body)
				require.NoError(t, err)
				assert.Contains(t, body["error"], "missing API key")
			}
		})
	}
}

func TestRequestClassifierMiddleware_KeyHashConsistency(t *testing.T) {
	// Verify that the same raw key always produces the same hash.
	const rawKey = "sk-consistent-hash-test"
	expectedHash := proxy.HashKey(rawKey)

	var capturedHash string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHash = KeyHashFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw := RequestClassifierMiddleware()(next)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	mw.ServeHTTP(rec, req)

	assert.Equal(t, expectedHash, capturedHash, "hash from context should match proxy.HashKey")
}
