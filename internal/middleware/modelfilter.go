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

// ModelFilter 持有已编译的白名单 pattern 和声明模型，
// 提供模型过滤及合成 /v1/models 响应注入的能力。
type ModelFilter struct {
	patterns       atomic.Value // 存储 []string
	declaredModels atomic.Value // 存储 map[int64][]string（upstream_id -> model IDs）
	store          *store.Store
}

// NewModelFilter 创建 ModelFilter 并从 store 加载 pattern 和声明模型。
func NewModelFilter(s *store.Store) *ModelFilter {
	mf := &ModelFilter{store: s}
	mf.Reload()
	mf.ReloadDeclaredModels()
	return mf
}

// Reload 从数据库刷新白名单 pattern。
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

// ReloadDeclaredModels 从数据库刷新声明模型。
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

// MatchModel 检查某个模型 ID 是否匹配任意白名单 pattern。
// 白名单为空时返回 true（全部允许）。
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

// openAIModelsResponse 是 /v1/models 响应的结构体。
type openAIModelsResponse struct {
	Object string                   `json:"object"`
	Data   []map[string]interface{} `json:"data"`
}

// ModelFilterMiddleware 拦截 GET /v1/models 响应，注入已绑定上游的声明模型，
// 并按白名单进行过滤。
func ModelFilterMiddleware(mf *ModelFilter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || !isModelsPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// 捕获上游响应。
			capture := &responseCapture{
				header:     make(http.Header),
				statusCode: http.StatusOK,
				body:       &bytes.Buffer{},
			}
			next.ServeHTTP(capture, r)

			// 收集已绑定上游的声明模型。
			declaredModels := mf.getDeclaredModels()
			var injected []map[string]interface{}
			if len(declaredModels) > 0 {
				allowedIDs := proxy.AllowedUpstreamIDsFromContext(r.Context())
				injected = buildDeclaredModelEntries(declaredModels, allowedIDs)
			}

			// 若上游返回非 200，决定是否构建合成响应。
			// 只对 404/502（上游不支持 /v1/models）做合成。
			// 403/503/429（代理层鉴权/限流错误）原样透传。
			if capture.statusCode != http.StatusOK {
				if len(injected) > 0 && canSynthesizeResponse(capture.statusCode) {
					filtered := applyWhitelist(mf, deduplicateEntries(injected))
					writeModelsResponse(w, filtered)
					return
				}
				replayCapture(w, capture)
				return
			}

			// 解析上游响应。
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

			// 合并：上游模型 + 声明模型（按 ID 去重）。
			// 在改动 seen 之前先统计注入数量。
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

			// 应用白名单过滤。
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

// canSynthesizeResponse 对表明上游确实不支持 /v1/models 的状态码返回 true
// （404、502，以及以 502 呈现的连接错误）。
// 鉴权/限流错误（403、429、503）会原样透传。
func canSynthesizeResponse(statusCode int) bool {
	return statusCode == http.StatusNotFound ||
		statusCode == http.StatusBadGateway ||
		statusCode == http.StatusNotImplemented
}

// buildDeclaredModelEntries 从声明模型构建 OpenAI 风格的模型条目。
// 若 allowedIDs 为空，则包含全部声明模型（表示该 Key 没有显式绑定）。
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

// deduplicateEntries 移除 entries 中重复的模型 ID。
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

// responseCapture 完整捕获一个 HTTP 响应。
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
