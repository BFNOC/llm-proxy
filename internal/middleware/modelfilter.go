package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/Instawork/llm-proxy/internal/store"
)

// ModelFilter holds compiled whitelist patterns and provides model filtering.
type ModelFilter struct {
	patterns atomic.Value // stores []string
	store    *store.Store
}

// NewModelFilter creates a ModelFilter and loads patterns from the store.
func NewModelFilter(s *store.Store) *ModelFilter {
	mf := &ModelFilter{store: s}
	mf.Reload()
	return mf
}

// Reload refreshes patterns from the database.
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

// getPatterns returns the current patterns.
func (mf *ModelFilter) getPatterns() []string {
	v := mf.patterns.Load()
	if v == nil {
		return nil
	}
	return v.([]string)
}

// matchModel checks if a model ID matches any whitelist pattern.
// Patterns without wildcards match as substrings; patterns with * use glob matching.
func (mf *ModelFilter) matchModel(modelID string) bool {
	patterns := mf.getPatterns()
	if len(patterns) == 0 {
		return true // empty whitelist = allow all
	}
	for _, p := range patterns {
		if strings.Contains(p, "*") || strings.Contains(p, "?") {
			// Glob match
			if matched, _ := filepath.Match(p, modelID); matched {
				return true
			}
		} else {
			// Substring match
			if strings.Contains(modelID, p) {
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

// ModelFilterMiddleware intercepts GET /v1/models responses and filters them
// against the whitelist patterns. Non-matching models are removed.
func ModelFilterMiddleware(mf *ModelFilter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only intercept GET /v1/models
			if r.Method != http.MethodGet || !isModelsPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// If no patterns configured, pass through
			if len(mf.getPatterns()) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			// Capture the response
			capture := &responseCapture{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				body:           &bytes.Buffer{},
			}
			next.ServeHTTP(capture, r)

			// Only filter successful JSON responses
			if capture.statusCode != http.StatusOK {
				w.WriteHeader(capture.statusCode)
				w.Write(capture.body.Bytes()) //nolint:errcheck
				return
			}

			// Parse and filter the models response
			var modelsResp openAIModelsResponse
			if err := json.Unmarshal(capture.body.Bytes(), &modelsResp); err != nil {
				// Not valid JSON — pass through as-is
				w.WriteHeader(capture.statusCode)
				w.Write(capture.body.Bytes()) //nolint:errcheck
				return
			}

			// Filter models
			var filtered []map[string]interface{}
			for _, model := range modelsResp.Data {
				id, ok := model["id"].(string)
				if !ok {
					continue
				}
				if mf.matchModel(id) {
					filtered = append(filtered, model)
				}
			}
			modelsResp.Data = filtered

			// Write filtered response
			out, _ := json.Marshal(modelsResp)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(out) //nolint:errcheck
		})
	}
}

// isModelsPath checks if the path is a models endpoint.
func isModelsPath(path string) bool {
	return path == "/v1/models" || path == "/v1/models/"
}

// responseCapture captures the response body and status code.
type responseCapture struct {
	http.ResponseWriter
	statusCode  int
	body        *bytes.Buffer
	wroteHeader bool
}

func (rc *responseCapture) WriteHeader(code int) {
	rc.statusCode = code
	rc.wroteHeader = true
	// Don't write to underlying writer yet — we need to filter first.
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	return rc.body.Write(b)
}

// Flush implements http.Flusher (no-op for captured responses).
func (rc *responseCapture) Flush() {}
