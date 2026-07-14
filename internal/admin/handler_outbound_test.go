package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Instawork/llm-proxy/internal/middleware"
	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain disables SSRF protection for the admin test suite.
// Test servers created by httptest.NewServer listen on 127.0.0.1, which would
// be rejected by safeDialContext in BuildTransport.
func TestMain(m *testing.M) {
	proxy.SSRFProtection = false
	os.Exit(m.Run())
}

// setupOutboundTestAdmin creates an AdminHandler wired to a temporary SQLite DB
// and returns the handler's underlying store so tests can insert upstreams
// directly (bypassing the HTTP handler's SSRF URL validation).
func setupOutboundTestAdmin(t *testing.T) (*store.Store, *mux.Router) {
	t.Helper()
	dir := t.TempDir()
	encKey := []byte("01234567890123456789012345678901") // 32 bytes
	s, err := store.NewStore(filepath.Join(dir, "test.db"), encKey)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	keyCache := middleware.NewKeyCache()
	keyCache.Reload(s)
	rateLimiter := middleware.NewPerKeyRPMLimiter()
	dp := proxy.NewDynamicProxy()
	overrideCache := middleware.NewModelOverrideCache(s)
	bindingCache := middleware.NewBindingCache(s)
	modelFilter := middleware.NewModelFilter(s)
	globalCounter := middleware.NewGlobalRequestCounter()
	perKeyStats := middleware.NewPerKeyStatsCollector()
	headerCapture := middleware.NewHeaderCapture(20)

	prober := proxy.NewUpstreamProber(s, dp, 30*time.Second, 5*time.Second)

	h := NewAdminHandler(s, keyCache, rateLimiter, prober, dp, nil, modelFilter, globalCounter, perKeyStats, overrideCache, bindingCache, headerCapture, testAdminToken, "test")
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	return s, r
}

// --- testUpstreamProxy Tests ---

func TestTestUpstreamProxy_Success(t *testing.T) {
	// Fake upstream serving GET /v1/models with a valid OpenAI-style response.
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" && r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "list",
				"data": []map[string]interface{}{
					{"id": "gpt-4", "object": "model"},
					{"id": "gpt-3.5-turbo", "object": "model"},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer fakeUpstream.Close()

	s, router := setupOutboundTestAdmin(t)

	// Create upstream directly via store (bypasses SSRF URL validation).
	upstream, err := s.CreateUpstream("test-proxy-ok", fakeUpstream.URL, []string{"sk-test-key"}, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	path := fmt.Sprintf("/admin/api/upstreams/%d/test-proxy", upstream.ID)
	rr := doRequest(t, router, "POST", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, true, result["success"])
	assert.Equal(t, float64(200), result["status_code"])

	models, ok := result["models"].([]interface{})
	require.True(t, ok, "models should be an array")
	assert.Len(t, models, 2)
	assert.Contains(t, models, "gpt-4")
	assert.Contains(t, models, "gpt-3.5-turbo")
}

func TestTestUpstreamProxy_UnreachableURL(t *testing.T) {
	s, router := setupOutboundTestAdmin(t)

	// Create upstream pointing to a port that nothing listens on.
	upstream, err := s.CreateUpstream("unreachable", "http://127.0.0.1:1", nil, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	path := fmt.Sprintf("/admin/api/upstreams/%d/test-proxy", upstream.ID)
	rr := doRequest(t, router, "POST", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code) // handler returns 200 with success=false

	result := decodeJSON(t, rr)
	assert.Equal(t, false, result["success"])
	assert.NotEmpty(t, result["error"])
}

func TestTestUpstreamProxy_NotFound(t *testing.T) {
	_, router := setupOutboundTestAdmin(t)

	rr := doRequest(t, router, "POST", "/admin/api/upstreams/99999/test-proxy", nil)
	assert.Equal(t, http.StatusNotFound, rr.Code)

	result := decodeJSON(t, rr)
	assert.Contains(t, result, "error")
}

func TestTestUpstreamProxy_Non2xx(t *testing.T) {
	// Upstream returns 401.
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid api key"})
	}))
	defer fakeUpstream.Close()

	s, router := setupOutboundTestAdmin(t)

	upstream, err := s.CreateUpstream("test-proxy-401", fakeUpstream.URL, []string{"sk-bad"}, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	path := fmt.Sprintf("/admin/api/upstreams/%d/test-proxy", upstream.ID)
	rr := doRequest(t, router, "POST", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, false, result["success"])
	assert.Equal(t, float64(401), result["status_code"])
}

func TestTestUpstreamProxy_NoAPIKey(t *testing.T) {
	// Upstream serving /v1/models without requiring auth (public upstream).
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "list",
			"data":   []map[string]interface{}{{"id": "llama-3", "object": "model"}},
		})
	}))
	defer fakeUpstream.Close()

	s, router := setupOutboundTestAdmin(t)

	upstream, err := s.CreateUpstream("public", fakeUpstream.URL, nil, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	path := fmt.Sprintf("/admin/api/upstreams/%d/test-proxy", upstream.ID)
	rr := doRequest(t, router, "POST", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, true, result["success"])
}

// --- checkUpstreamQuota Tests ---

func TestCheckUpstreamQuota_Success(t *testing.T) {
	// Fake upstream serving the new-api quota endpoint.
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/usage/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"code":    true,
				"message": "success",
				"data": map[string]interface{}{
					"object":          "token_usage",
					"name":            "test-user",
					"total_available": 900,
					"total_granted":   1000,
					"total_used":      100,
					"unlimited_quota": false,
					"expires_at":      0,
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer fakeUpstream.Close()

	s, router := setupOutboundTestAdmin(t)

	upstream, err := s.CreateUpstream("quota-ok", fakeUpstream.URL, []string{"sk-quota-key"}, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	path := fmt.Sprintf("/admin/api/upstreams/%d/check-quota", upstream.ID)
	rr := doRequest(t, router, "POST", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, true, result["success"])

	data, ok := result["data"].(map[string]interface{})
	require.True(t, ok, "data should be a map")
	assert.Equal(t, "test-user", data["name"])
	assert.Equal(t, float64(900), data["total_available"])
	assert.Equal(t, float64(1000), data["total_granted"])
	assert.Equal(t, float64(100), data["total_used"])
}

func TestCheckUpstreamQuota_NonNewAPI(t *testing.T) {
	// Upstream returns JSON but not in new-api format (no data.object=token_usage).
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"usage":  42,
		})
	}))
	defer fakeUpstream.Close()

	s, router := setupOutboundTestAdmin(t)

	upstream, err := s.CreateUpstream("quota-noapi", fakeUpstream.URL, []string{"sk-key"}, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	path := fmt.Sprintf("/admin/api/upstreams/%d/check-quota", upstream.ID)
	rr := doRequest(t, router, "POST", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, false, result["success"])
	assert.Equal(t, "error", result["message"])
	// origin_content should contain the raw upstream response.
	assert.NotEmpty(t, result["origin_content"])
}

func TestCheckUpstreamQuota_NotFound(t *testing.T) {
	_, router := setupOutboundTestAdmin(t)

	rr := doRequest(t, router, "POST", "/admin/api/upstreams/99999/check-quota", nil)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestCheckUpstreamQuota_UpstreamHTTPError(t *testing.T) {
	// Upstream returns 500.
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer fakeUpstream.Close()

	s, router := setupOutboundTestAdmin(t)

	upstream, err := s.CreateUpstream("quota-500", fakeUpstream.URL, []string{"sk-key"}, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	path := fmt.Sprintf("/admin/api/upstreams/%d/check-quota", upstream.ID)
	rr := doRequest(t, router, "POST", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, false, result["success"])
	assert.Contains(t, result["message"], "HTTP 500")
}

func TestCheckUpstreamQuota_NonJSONContentType(t *testing.T) {
	// Upstream returns HTML instead of JSON.
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>not json</html>"))
	}))
	defer fakeUpstream.Close()

	s, router := setupOutboundTestAdmin(t)

	upstream, err := s.CreateUpstream("quota-html", fakeUpstream.URL, []string{"sk-key"}, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	path := fmt.Sprintf("/admin/api/upstreams/%d/check-quota", upstream.ID)
	rr := doRequest(t, router, "POST", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, false, result["success"])
	assert.Equal(t, "error", result["message"])
	assert.Contains(t, result["origin_content"], "not json")
}

func TestCheckUpstreamQuota_CodeFalse(t *testing.T) {
	// new-api returns code=false with an error message.
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    false,
			"message": "invalid token",
			"data": map[string]interface{}{
				"object": "token_usage",
			},
		})
	}))
	defer fakeUpstream.Close()

	s, router := setupOutboundTestAdmin(t)

	upstream, err := s.CreateUpstream("quota-code-false", fakeUpstream.URL, []string{"sk-key"}, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	path := fmt.Sprintf("/admin/api/upstreams/%d/check-quota", upstream.ID)
	rr := doRequest(t, router, "POST", path, nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, false, result["success"])
	assert.Contains(t, result["message"], "invalid token")
}

// --- testUpstreamAPIKey Tests ---

func TestTestUpstreamAPIKey_OpenAI_Success(t *testing.T) {
	// Fake upstream serving POST /v1/chat/completions (OpenAI protocol).
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" && r.Method == "POST" {
			// Verify Authorization header is set.
			assert.NotEmpty(t, r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"model": "gpt-4",
				"choices": []map[string]interface{}{
					{"message": map[string]string{"content": "I am GPT-4."}},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer fakeUpstream.Close()

	s, router := setupOutboundTestAdmin(t)

	upstream, err := s.CreateUpstream("test-key-openai", fakeUpstream.URL, []string{"sk-test-api-key"}, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	// Look up the API key row ID.
	keys, err := s.GetUpstreamAllAPIKeys(upstream.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	keyRowID := keys[0].RowID

	path := fmt.Sprintf("/admin/api/upstreams/%d/apikeys/%d/test", upstream.ID, keyRowID)
	rr := doRequest(t, router, "POST", path, map[string]interface{}{
		"protocol": "openai",
		"model":    "gpt-4",
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, true, result["success"])
	assert.Equal(t, float64(200), result["status_code"])
	assert.Equal(t, "gpt-4", result["model"])
	assert.Equal(t, "openai", result["protocol"])
	assert.Equal(t, "I am GPT-4.", result["reply"])
	assert.Equal(t, "gpt-4", result["actual_model"])
}

func TestTestUpstreamAPIKey_Anthropic_Success(t *testing.T) {
	// Fake upstream serving POST /v1/messages (Anthropic protocol).
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/messages" && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"model": "claude-sonnet-4-5-20250929",
				"content": []map[string]interface{}{
					{"type": "text", "text": "I am Claude."},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer fakeUpstream.Close()

	s, router := setupOutboundTestAdmin(t)

	upstream, err := s.CreateUpstream("test-key-anthropic", fakeUpstream.URL, []string{"sk-anthropic-key"}, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(upstream.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)

	path := fmt.Sprintf("/admin/api/upstreams/%d/apikeys/%d/test", upstream.ID, keys[0].RowID)
	rr := doRequest(t, router, "POST", path, map[string]interface{}{
		"protocol": "anthropic",
		"model":    "claude-sonnet-4-5-20250929",
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, true, result["success"])
	assert.Equal(t, float64(200), result["status_code"])
	assert.Equal(t, "anthropic", result["protocol"])
}

func TestTestUpstreamAPIKey_NotFoundUpstream(t *testing.T) {
	_, router := setupOutboundTestAdmin(t)

	rr := doRequest(t, router, "POST", "/admin/api/upstreams/99999/apikeys/1/test", map[string]interface{}{
		"protocol": "openai",
		"model":    "gpt-4",
	})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestTestUpstreamAPIKey_NotFoundKey(t *testing.T) {
	s, router := setupOutboundTestAdmin(t)

	upstream, err := s.CreateUpstream("test-key-notfound", "http://127.0.0.1:1", []string{"sk-key"}, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	// Use a non-existent key_id (99999).
	path := fmt.Sprintf("/admin/api/upstreams/%d/apikeys/99999/test", upstream.ID)
	rr := doRequest(t, router, "POST", path, map[string]interface{}{
		"protocol": "openai",
		"model":    "gpt-4",
	})
	assert.Equal(t, http.StatusNotFound, rr.Code)

	result := decodeJSON(t, rr)
	assert.Contains(t, result, "error")
}

func TestTestUpstreamAPIKey_OpenAI_ErrorResponse(t *testing.T) {
	// Upstream returns 429.
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{
				"message": "Rate limit exceeded",
				"type":    "rate_limit_error",
			},
		})
	}))
	defer fakeUpstream.Close()

	s, router := setupOutboundTestAdmin(t)

	upstream, err := s.CreateUpstream("test-key-ratelimit", fakeUpstream.URL, []string{"sk-limited"}, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(upstream.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)

	path := fmt.Sprintf("/admin/api/upstreams/%d/apikeys/%d/test", upstream.ID, keys[0].RowID)
	rr := doRequest(t, router, "POST", path, map[string]interface{}{
		"protocol": "openai",
		"model":    "gpt-4",
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, false, result["success"])
	assert.Equal(t, float64(429), result["status_code"])
	assert.Equal(t, "Rate limit exceeded", result["error_message"])
}

func TestTestUpstreamAPIKey_DefaultProtocol(t *testing.T) {
	// When protocol is not specified, it should default to "openai".
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Expect OpenAI endpoint.
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"model":   "gpt-4o-mini",
			"choices": []map[string]interface{}{{"message": map[string]string{"content": "hi"}}},
		})
	}))
	defer fakeUpstream.Close()

	s, router := setupOutboundTestAdmin(t)

	upstream, err := s.CreateUpstream("test-default-proto", fakeUpstream.URL, []string{"sk-key"}, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(upstream.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)

	path := fmt.Sprintf("/admin/api/upstreams/%d/apikeys/%d/test", upstream.ID, keys[0].RowID)
	// No protocol or model specified.
	rr := doRequest(t, router, "POST", path, map[string]interface{}{})
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, true, result["success"])
	assert.Equal(t, "openai", result["protocol"])
	assert.Equal(t, "gpt-4o-mini", result["model"])
}

func TestTestUpstreamAPIKey_NoAuthKey(t *testing.T) {
	// Test with keyID=0 (no auth key scenario).
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// key_id=0 means no key; Authorization should be "Bearer " (empty key).
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"model":   "llama-3",
			"choices": []map[string]interface{}{{"message": map[string]string{"content": "hi"}}},
		})
	}))
	defer fakeUpstream.Close()

	s, router := setupOutboundTestAdmin(t)

	upstream, err := s.CreateUpstream("test-no-auth", fakeUpstream.URL, nil, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	// key_id=0 means "no auth key" in the handler.
	path := fmt.Sprintf("/admin/api/upstreams/%d/apikeys/0/test", upstream.ID)
	rr := doRequest(t, router, "POST", path, map[string]interface{}{
		"protocol": "openai",
		"model":    "llama-3",
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, true, result["success"])
}

func TestTestUpstreamAPIKey_Unreachable(t *testing.T) {
	s, router := setupOutboundTestAdmin(t)

	upstream, err := s.CreateUpstream("test-key-unreachable", "http://127.0.0.1:1", []string{"sk-key"}, 1, "", "round-robin", "api_key", "", false)
	require.NoError(t, err)

	keys, err := s.GetUpstreamAllAPIKeys(upstream.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)

	path := fmt.Sprintf("/admin/api/upstreams/%d/apikeys/%d/test", upstream.ID, keys[0].RowID)
	rr := doRequest(t, router, "POST", path, map[string]interface{}{
		"protocol": "openai",
		"model":    "gpt-4",
	})
	assert.Equal(t, http.StatusOK, rr.Code)

	result := decodeJSON(t, rr)
	assert.Equal(t, false, result["success"])
	assert.NotEmpty(t, result["error"])
}
