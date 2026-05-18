package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
)

// ModelFilter holds compiled whitelist patterns and declared models, providing
// model filtering and synthetic /v1/models response injection.
type ModelFilter struct {
	patterns       atomic.Value // stores []string
	declaredModels atomic.Value // stores map[int64][]string (upstream_id -> model IDs)
	store          *store.Store
}

// NewModelFilter creates a ModelFilter and loads patterns + declared models from the store.
func NewModelFilter(s *store.Store) *ModelFilter {
	mf := &ModelFilter{store: s}
	mf.Reload()
	mf.ReloadDeclaredModels()
	return mf
}

// Reload refreshes whitelist patterns from the database.
func (mf *ModelFilter) Reload() {
	entries, err := mf.store.ListModelWhitelist()
	if err != nil {
		slog.Error("model filter: failed to load whitelist", "error", err)
		return
	}
	patterns := make([]string, len(entries))
	for i, e := range entries {
		patterns[i] = e.Pattern
	}
	mf.patterns.Store(patterns)
	slog.Info("model filter: loaded whitelist", "count", len(patterns))
}

// ReloadDeclaredModels refreshes declared models from the database.
func (mf *ModelFilter) ReloadDeclaredModels() {
	models, err := mf.store.GetAllUpstreamDeclaredModels()
	if err != nil {
		slog.Error("model filter: failed to load declared models", "error", err)
		return
	}
	mf.declaredModels.Store(models)
	slog.Info("model filter: loaded declared models", "upstreams", len(models))
}

func (mf *ModelFilter) getPatterns() []string {
	v := mf.patterns.Load()
	if v == nil {
		return nil
	}
	return v.([]string)
}

func (mf *ModelFilter) getDeclaredModels() map[int64][]string {
	v := mf.declaredModels.Load()
	if v == nil {
		return nil
	}
	return v.(map[int64][]string)
}

// MatchModel checks if a model ID matches any whitelist pattern.
// Returns true if the whitelist is empty (allow all).
func (mf *ModelFilter) MatchModel(modelID string) bool {
	patterns := mf.getPatterns()
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if strings.Contains(p, "*") || strings.Contains(p, "?") {
			if matched, _ := path.Match(p, modelID); matched {
				return true
			}
		} else {
			if modelID == p {
				return true
			}
		}
	}
	return false
}

// openAIModelsResponse is the structure of /v1/models responses.
type openAIModelsResponse struct {
	Object string                   `json:"object"`
	Data   []map[string]interface{} `json:"data"`
}

// ModelFilterMiddleware intercepts GET /v1/models responses, injects declared
// models from bound upstreams, and filters them against the whitelist.
func ModelFilterMiddleware(mf *ModelFilter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || !isModelsPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Capture the upstream response.
			capture := &responseCapture{
				header:     make(http.Header),
				statusCode: http.StatusOK,
				body:       &bytes.Buffer{},
			}
			next.ServeHTTP(capture, r)

			// Collect declared models for the bound upstreams.
			declaredModels := mf.getDeclaredModels()
			var injected []map[string]interface{}
			if len(declaredModels) > 0 {
				allowedIDs := proxy.AllowedUpstreamIDsFromContext(r.Context())
				injected = buildDeclaredModelEntries(declaredModels, allowedIDs)
			}

			// If upstream returned non-200, decide whether to build synthetic response.
			// Only synthesize for 404/502 (upstream doesn't support /v1/models).
			// Pass through 403/503/429 (proxy-layer auth/rate errors) unchanged.
			if capture.statusCode != http.StatusOK {
				if len(injected) > 0 && canSynthesizeResponse(capture.statusCode) {
					filtered := applyWhitelist(mf, deduplicateEntries(injected))
					writeModelsResponse(w, filtered)
					return
				}
				replayCapture(w, capture)
				return
			}

			// Parse upstream response.
			var modelsResp openAIModelsResponse
			if err := json.Unmarshal(capture.body.Bytes(), &modelsResp); err != nil {
				if len(injected) > 0 {
					filtered := applyWhitelist(mf, deduplicateEntries(injected))
					writeModelsResponse(w, filtered)
					return
				}
				replayCapture(w, capture)
				return
			}

			// Merge: upstream models + declared models (deduplicate by ID).
			// Count injections before mutating seen.
			seen := make(map[string]bool, len(modelsResp.Data))
			for _, model := range modelsResp.Data {
				if id, ok := model["id"].(string); ok {
					seen[id] = true
				}
			}
			declaredCount := 0
			for _, entry := range injected {
				if id, ok := entry["id"].(string); ok && !seen[id] {
					modelsResp.Data = append(modelsResp.Data, entry)
					seen[id] = true
					declaredCount++
				}
			}

			// Apply whitelist filter.
			patterns := mf.getPatterns()
			if len(patterns) > 0 {
				var filtered []map[string]interface{}
				for _, model := range modelsResp.Data {
					if id, ok := model["id"].(string); ok && mf.MatchModel(id) {
						filtered = append(filtered, model)
					}
				}
				modelsResp.Data = filtered
			}

			slog.Info("model filter: /v1/models response",
				"upstream_models", len(seen)-declaredCount,
				"declared_injected", declaredCount,
				"final", len(modelsResp.Data))

			out, _ := json.Marshal(modelsResp)
			copyHeader(w.Header(), capture.header)
			w.Header().Del("Content-Length")
			w.Header().Del("Content-Encoding")
			w.Header().Del("Etag")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(out) //nolint:errcheck
		})
	}
}

// canSynthesizeResponse returns true for status codes that indicate the upstream
// genuinely doesn't support /v1/models (404, 502, connection errors surfaced as 502).
// Auth/rate errors (403, 429, 503) are passed through unchanged.
func canSynthesizeResponse(statusCode int) bool {
	return statusCode == http.StatusNotFound ||
		statusCode == http.StatusBadGateway ||
		statusCode == http.StatusNotImplemented
}

// buildDeclaredModelEntries builds OpenAI-style model entries from declared models.
// If allowedIDs is empty, all declared models are included (key has no explicit binding).
func buildDeclaredModelEntries(declaredModels map[int64][]string, allowedIDs []int64) []map[string]interface{} {
	now := time.Now().Unix()
	var entries []map[string]interface{}

	if len(allowedIDs) == 0 {
		for _, models := range declaredModels {
			for _, m := range models {
				entries = append(entries, map[string]interface{}{
					"id":       m,
					"object":   "model",
					"created":  now,
					"owned_by": "declared",
				})
			}
		}
	} else {
		allowed := make(map[int64]bool, len(allowedIDs))
		for _, id := range allowedIDs {
			allowed[id] = true
		}
		for upstreamID, models := range declaredModels {
			if !allowed[upstreamID] {
				continue
			}
			for _, m := range models {
				entries = append(entries, map[string]interface{}{
					"id":       m,
					"object":   "model",
					"created":  now,
					"owned_by": "declared",
				})
			}
		}
	}
	return entries
}

// deduplicateEntries removes duplicate model IDs from entries.
func deduplicateEntries(entries []map[string]interface{}) []map[string]interface{} {
	seen := make(map[string]bool, len(entries))
	var result []map[string]interface{}
	for _, entry := range entries {
		if id, ok := entry["id"].(string); ok && !seen[id] {
			seen[id] = true
			result = append(result, entry)
		}
	}
	return result
}

func applyWhitelist(mf *ModelFilter, entries []map[string]interface{}) []map[string]interface{} {
	patterns := mf.getPatterns()
	if len(patterns) == 0 {
		return entries
	}
	var filtered []map[string]interface{}
	for _, entry := range entries {
		if id, ok := entry["id"].(string); ok && mf.MatchModel(id) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func writeModelsResponse(w http.ResponseWriter, data []map[string]interface{}) {
	resp := openAIModelsResponse{Object: "list", Data: data}
	out, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(out) //nolint:errcheck
}

func replayCapture(w http.ResponseWriter, capture *responseCapture) {
	copyHeader(w.Header(), capture.header)
	w.WriteHeader(capture.statusCode)
	w.Write(capture.body.Bytes()) //nolint:errcheck
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func isModelsPath(path string) bool {
	return path == "/v1/models" || path == "/v1/models/"
}

// responseCapture fully captures an HTTP response.
type responseCapture struct {
	header     http.Header
	statusCode int
	body       *bytes.Buffer
}

func (rc *responseCapture) Header() http.Header {
	return rc.header
}

func (rc *responseCapture) WriteHeader(code int) {
	rc.statusCode = code
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	return rc.body.Write(b)
}

func (rc *responseCapture) Flush() {}
