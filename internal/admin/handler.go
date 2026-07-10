package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Instawork/llm-proxy/internal/middleware"
	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/gorilla/mux"
)

var startTime = time.Now()

type AdminHandler struct {
	store          *store.Store
	keyCache       *middleware.KeyCache
	rateLimiter    *middleware.PerKeyRPMLimiter
	prober         *proxy.UpstreamProber
	dynamicProxy   *proxy.DynamicProxy
	auditLogger    *middleware.AuditLogger
	modelFilter    *middleware.ModelFilter
	requestCounter *middleware.GlobalRequestCounter
	perKeyStats    *middleware.PerKeyStatsCollector
	overrideCache  *middleware.ModelOverrideCache
	adminToken     string
	version        string
}

func NewAdminHandler(
	s *store.Store,
	kc *middleware.KeyCache,
	rl *middleware.PerKeyRPMLimiter,
	prober *proxy.UpstreamProber,
	dp *proxy.DynamicProxy,
	al *middleware.AuditLogger,
	mf *middleware.ModelFilter,
	rc *middleware.GlobalRequestCounter,
	pks *middleware.PerKeyStatsCollector,
	oc *middleware.ModelOverrideCache,
	adminToken string,
	version string,
) *AdminHandler {
	return &AdminHandler{
		store:          s,
		keyCache:       kc,
		rateLimiter:    rl,
		prober:         prober,
		dynamicProxy:   dp,
		auditLogger:    al,
		modelFilter:    mf,
		requestCounter: rc,
		perKeyStats:    pks,
		overrideCache:  oc,
		adminToken:     adminToken,
		version:        version,
	}
}

// RegisterRoutes registers admin API routes on the given subrouter.
func (h *AdminHandler) RegisterRoutes(r *mux.Router) {
	// All admin routes require admin auth
	api := r.PathPrefix("/admin/api").Subrouter()
	api.Use(h.authMiddleware)

	// Upstreams
	api.HandleFunc("/upstreams", h.listUpstreams).Methods("GET")
	api.HandleFunc("/upstreams", h.createUpstream).Methods("POST")
	api.HandleFunc("/upstreams/{id}", h.updateUpstream).Methods("PUT")
	api.HandleFunc("/upstreams/{id}", h.deleteUpstream).Methods("DELETE")
	api.HandleFunc("/upstreams/{id}/test-proxy", h.testUpstreamProxy).Methods("POST")
	api.HandleFunc("/upstreams/{id}/check-quota", h.checkUpstreamQuota).Methods("POST")
	api.HandleFunc("/upstreams/models", h.getAllUpstreamModelPatterns).Methods("GET")
	api.HandleFunc("/upstreams/{id}/models", h.getUpstreamModelPatterns).Methods("GET")
	api.HandleFunc("/upstreams/{id}/models", h.setUpstreamModelPatterns).Methods("PUT")
	api.HandleFunc("/upstreams/declared-models", h.getAllUpstreamDeclaredModels).Methods("GET")
	api.HandleFunc("/upstreams/{id}/declared-models", h.getUpstreamDeclaredModels).Methods("GET")
	api.HandleFunc("/upstreams/{id}/declared-models", h.setUpstreamDeclaredModels).Methods("PUT")
	// Per-key API key management
	api.HandleFunc("/upstreams/{id}/apikeys", h.listUpstreamAPIKeys).Methods("GET")
	api.HandleFunc("/upstreams/{id}/apikeys", h.addUpstreamAPIKeys).Methods("POST")
	api.HandleFunc("/upstreams/{id}/apikeys/{key_id}", h.deleteUpstreamAPIKey).Methods("DELETE")
	api.HandleFunc("/upstreams/{id}/apikeys/{key_id}/enabled", h.setAPIKeyEnabled).Methods("PUT")
	api.HandleFunc("/upstreams/{id}/apikeys/{key_id}/test", h.testUpstreamAPIKey).Methods("POST")

	// Keys
	api.HandleFunc("/keys", h.listKeys).Methods("GET")
	api.HandleFunc("/keys", h.createKey).Methods("POST")
	api.HandleFunc("/keys/{id}", h.updateKey).Methods("PUT")
	api.HandleFunc("/keys/{id}", h.deleteKey).Methods("DELETE")
	api.HandleFunc("/keys/{id}/reveal", h.revealKey).Methods("GET")

	// Logs
	api.HandleFunc("/logs", h.queryLogs).Methods("GET")
	api.HandleFunc("/logs/key-stats", h.getKeyUsageStats).Methods("GET")

	// Model whitelist
	api.HandleFunc("/models/whitelist", h.listModelWhitelist).Methods("GET")
	api.HandleFunc("/models/whitelist", h.addModelWhitelist).Methods("POST")
	api.HandleFunc("/models/whitelist/batch", h.batchDeleteModelWhitelist).Methods("DELETE")
	api.HandleFunc("/models/whitelist/{id}", h.deleteModelWhitelist).Methods("DELETE")

	// 绑定接口拆成“全量查看”“单 Key 查询”“全量覆盖更新”三类，
	// 让管理端既能一次加载总览，也能按 Key 精确编辑。
	api.HandleFunc("/keys/bindings", h.getAllKeyBindings).Methods("GET")
	api.HandleFunc("/keys/{id}/upstreams", h.getKeyUpstreams).Methods("GET")
	api.HandleFunc("/keys/{id}/upstreams", h.setKeyUpstreams).Methods("PUT")

	// Key model overrides
	api.HandleFunc("/keys/model-overrides", h.getAllKeyModelOverrides).Methods("GET")
	api.HandleFunc("/keys/{id}/model-overrides", h.getKeyModelOverrides).Methods("GET")
	api.HandleFunc("/keys/{id}/model-overrides", h.setKeyModelOverrides).Methods("PUT")

	// Status
	api.HandleFunc("/status", h.getStatus).Methods("GET")
	api.HandleFunc("/key-rpm", h.getKeyRPM).Methods("GET")

	// Test models
	api.HandleFunc("/test-models", h.listTestModels).Methods("GET")
	api.HandleFunc("/test-models", h.createTestModel).Methods("POST")
	api.HandleFunc("/test-models/{id}", h.updateTestModel).Methods("PUT")
	api.HandleFunc("/test-models/{id}", h.deleteTestModel).Methods("DELETE")

	api.HandleFunc("/settings", h.getSettings).Methods("GET")
	api.HandleFunc("/settings", h.updateSettings).Methods("PUT")

	// Dashboard (serve embedded HTML)
	r.PathPrefix("/admin/").HandlerFunc(h.serveDashboard)
}

func (h *AdminHandler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if !strings.HasPrefix(token, "Bearer ") || strings.TrimPrefix(token, "Bearer ") != h.adminToken {
			jsonError(w, http.StatusUnauthorized, "invalid admin token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Upstreams ---

func (h *AdminHandler) listUpstreams(w http.ResponseWriter, r *http.Request) {
	upstreams, err := h.store.ListUpstreams()
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// API Keys are now returned unmasked for admin convenience
	type apiKeyInfo struct {
		RowID   int64  `json:"row_id"`
		Key     string `json:"key"`
		Enabled bool   `json:"enabled"`
	}
	type upstreamResponse struct {
		ID                int64        `json:"id"`
		Name              string       `json:"name"`
		BaseURL           string       `json:"base_url"`
		APIKeys           []string     `json:"api_keys"`
		APIKeyDetails     []apiKeyInfo `json:"api_key_details"`
		ProxyURL          string       `json:"proxy_url"`
		Priority          int          `json:"priority"`
		Enabled           bool         `json:"enabled"`
		KeySchedulingMode string       `json:"key_scheduling_mode"`
		AuthMode          string       `json:"auth_mode"`
		Remark            string       `json:"remark"`
		CreatedAt         time.Time    `json:"created_at"`
		UpdatedAt         time.Time    `json:"updated_at"`
	}
	result := make([]upstreamResponse, len(upstreams))
	for i, u := range upstreams {
		keys := u.APIKeys
		if keys == nil {
			keys = []string{}
		}
		// 加载每个 Key 的详细信息（含启用状态和 row ID）
		keyDetails, err := h.store.GetUpstreamAllAPIKeys(u.ID)
		if err != nil {
			slog.Error("admin: failed to load api key details", "upstream_id", u.ID, "error", err)
			keyDetails = []store.APIKeyInfo{}
		}
		details := make([]apiKeyInfo, len(keyDetails))
		for j, kd := range keyDetails {
			details[j] = apiKeyInfo{RowID: kd.RowID, Key: kd.Key, Enabled: kd.Enabled}
		}
		authMode := u.AuthMode
		if authMode == "" {
			authMode = "api_key"
		}
		result[i] = upstreamResponse{
			ID: u.ID, Name: u.Name, BaseURL: u.BaseURL, APIKeys: keys, APIKeyDetails: details,
			ProxyURL: u.ProxyURL, Priority: u.Priority, Enabled: u.Enabled,
			KeySchedulingMode: u.KeySchedulingMode, AuthMode: authMode, Remark: u.Remark,
			CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
		}
	}
	jsonOK(w, result)
}

func (h *AdminHandler) createUpstream(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name              string   `json:"name"`
		BaseURL           string   `json:"base_url"`
		APIKey            string   `json:"api_key"`  // 向后兼容单 Key
		APIKeys           []string `json:"api_keys"` // 新多 Key 字段
		ProxyURL          string   `json:"proxy_url"`
		Priority          int      `json:"priority"`
		KeySchedulingMode string   `json:"key_scheduling_mode"`
		AuthMode          string   `json:"auth_mode"`
		Remark            string   `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// 兼容旧的单 api_key 字段
	apiKeys := cleanAPIKeys(req.APIKeys)
	if req.APIKey != "" {
		apiKeys = append(apiKeys, normalizeAPIKeyValues(req.APIKey)...)
		apiKeys = cleanAPIKeys(apiKeys)
	}
	if req.Name == "" || req.BaseURL == "" {
		jsonError(w, http.StatusBadRequest, "name and base_url are required")
		return
	}

	// 校验调度模式
	schedulingMode := req.KeySchedulingMode
	if schedulingMode == "" {
		schedulingMode = "round-robin"
	}
	if schedulingMode != "round-robin" && schedulingMode != "fill" {
		jsonError(w, http.StatusBadRequest, "key_scheduling_mode must be 'round-robin' or 'fill'")
		return
	}

	authMode := req.AuthMode
	if authMode == "" {
		authMode = "api_key"
	}
	if authMode != "api_key" && authMode != "oauth" {
		jsonError(w, http.StatusBadRequest, "auth_mode must be 'api_key' or 'oauth'")
		return
	}

	// SSRF validation
	if err := validateBaseURL(req.BaseURL); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Proxy URL validation
	if req.ProxyURL != "" {
		if err := validateProxyURL(req.ProxyURL); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	upstream, err := h.store.CreateUpstream(req.Name, req.BaseURL, apiKeys, req.Priority, req.ProxyURL, schedulingMode, authMode, req.Remark)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	slog.Info("admin: created upstream", "id", upstream.ID, "name", upstream.Name, "key_count", len(apiKeys), "proxy_url", sanitizeProxyForLog(req.ProxyURL))
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, map[string]interface{}{"id": upstream.ID, "name": upstream.Name, "base_url": upstream.BaseURL, "priority": upstream.Priority})
}

func (h *AdminHandler) updateUpstream(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Read existing upstream to preserve fields not provided in the request.
	existing, err := h.store.GetUpstream(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("upstream %d not found", id))
		return
	}

	var req struct {
		Name              *string   `json:"name"`
		BaseURL           *string   `json:"base_url"`
		APIKey            *string   `json:"api_key"`  // 向后兼容单 Key
		APIKeys           *[]string `json:"api_keys"` // 新多 Key 字段
		ProxyURL          *string   `json:"proxy_url"`
		Priority          *int      `json:"priority"`
		Enabled           *bool     `json:"enabled"`
		KeySchedulingMode *string   `json:"key_scheduling_mode"`
		AuthMode          *string   `json:"auth_mode"`
		Remark            *string   `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	name := existing.Name
	if req.Name != nil {
		name = *req.Name
	}
	baseURL := existing.BaseURL
	if req.BaseURL != nil {
		baseURL = *req.BaseURL
	}
	// API Keys: 优先用 api_keys 数组，其次兼容 api_key 单值，都不提供则传 nil 表示不修改
	var apiKeys []string // nil = don't change
	if req.APIKeys != nil {
		apiKeys = cleanAPIKeys(*req.APIKeys)
	} else if req.APIKey != nil && *req.APIKey != "" {
		apiKeys = cleanAPIKeys([]string{*req.APIKey})
	}
	priority := existing.Priority
	if req.Priority != nil {
		priority = *req.Priority
	}
	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	proxyURL := existing.ProxyURL
	if req.ProxyURL != nil {
		proxyURL = *req.ProxyURL
	}
	schedulingMode := existing.KeySchedulingMode
	if req.KeySchedulingMode != nil {
		schedulingMode = *req.KeySchedulingMode
		if schedulingMode != "round-robin" && schedulingMode != "fill" {
			jsonError(w, http.StatusBadRequest, "key_scheduling_mode must be 'round-robin' or 'fill'")
			return
		}
	}
	authMode := existing.AuthMode
	if authMode == "" {
		authMode = "api_key"
	}
	if req.AuthMode != nil {
		authMode = *req.AuthMode
		if authMode != "api_key" && authMode != "oauth" {
			jsonError(w, http.StatusBadRequest, "auth_mode must be 'api_key' or 'oauth'")
			return
		}
	}
	remark := existing.Remark
	if req.Remark != nil {
		remark = *req.Remark
	}

	if baseURL != existing.BaseURL {
		if err := validateBaseURL(baseURL); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if proxyURL != "" && proxyURL != existing.ProxyURL {
		if err := validateProxyURL(proxyURL); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	upstream, err := h.store.UpdateUpstream(id, name, baseURL, apiKeys, priority, enabled, proxyURL, schedulingMode, authMode, remark)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// 代理配置变更时回收旧 transport 连接池（仅当没有其他上游复用该代理时）
	if proxyURL != existing.ProxyURL {
		h.tryRemoveTransport(existing.ProxyURL, id)
	}
	// Trigger re-probe so disabled/enabled change takes effect immediately.
	go h.prober.ProbeNow()
	slog.Info("admin: updated upstream", "id", upstream.ID, "enabled", upstream.Enabled)
	jsonOK(w, map[string]interface{}{"id": upstream.ID, "name": upstream.Name, "enabled": upstream.Enabled})
}

func (h *AdminHandler) deleteUpstream(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 先获取上游信息以备回收 transport
	existing, _ := h.store.GetUpstream(id)
	if err := h.store.DeleteUpstream(id); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// 回收被删除上游的 transport 连接池（仅当没有其他上游复用该代理时）
	if existing != nil {
		h.tryRemoveTransport(existing.ProxyURL, id)
	}
	// Trigger immediate probe to update active upstream if needed.
	go h.prober.ProbeNow()
	// FK cascade may have removed overrides referencing this upstream
	if h.overrideCache != nil {
		h.overrideCache.Reload()
	}
	if h.modelFilter != nil {
		h.modelFilter.ReloadDeclaredModels()
	}
	slog.Info("admin: deleted upstream", "id", id)

	jsonOK(w, map[string]string{"status": "deleted"})
}

// --- Keys ---

func (h *AdminHandler) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.store.ListKeys()
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	type keyResponse struct {
		ID        int64     `json:"id"`
		KeyPrefix string    `json:"key_prefix"`
		Name      string    `json:"name"`
		RPMLimit  int       `json:"rpm_limit"`
		Enabled   bool      `json:"enabled"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	result := make([]keyResponse, len(keys))
	for i, k := range keys {
		result[i] = keyResponse{
			ID: k.ID, KeyPrefix: k.KeyPrefix, Name: k.Name,
			RPMLimit: k.RPMLimit, Enabled: k.Enabled,
			CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt,
		}
	}
	jsonOK(w, result)
}

func (h *AdminHandler) createKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		RPMLimit int    `json:"rpm_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" {
		jsonError(w, http.StatusBadRequest, "name is required")
		return
	}

	plaintext, key, err := h.store.CreateKey(req.Name, req.RPMLimit)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Reload key cache
	if err := h.keyCache.Reload(h.store); err != nil {
		slog.Error("admin: failed to reload key cache", "error", err)
	}

	slog.Info("admin: created key", "id", key.ID, "name", key.Name)
	w.WriteHeader(http.StatusCreated)
	// Return plaintext ONCE
	jsonOK(w, map[string]interface{}{
		"id":        key.ID,
		"key":       plaintext,
		"name":      key.Name,
		"rpm_limit": key.RPMLimit,
	})
}

func (h *AdminHandler) updateKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Read existing key to preserve fields not provided in the request.
	existing, err := h.store.LookupKeyByID(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("key %d not found", id))
		return
	}

	var req struct {
		Name     *string `json:"name"`
		RPMLimit *int    `json:"rpm_limit"`
		Enabled  *bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	name := existing.Name
	if req.Name != nil {
		name = *req.Name
	}
	rpmLimit := existing.RPMLimit
	if req.RPMLimit != nil {
		rpmLimit = *req.RPMLimit
	}
	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	key, err := h.store.UpdateKey(id, name, rpmLimit, enabled)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Reload key cache
	if err := h.keyCache.Reload(h.store); err != nil {
		slog.Error("admin: failed to reload key cache", "error", err)
	}

	slog.Info("admin: updated key", "id", key.ID)
	jsonOK(w, map[string]interface{}{"id": key.ID, "name": key.Name, "rpm_limit": key.RPMLimit, "enabled": key.Enabled})
}

func (h *AdminHandler) deleteKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.DeleteKey(id); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Reload key cache + clean rate limiter + clean stats
	if err := h.keyCache.Reload(h.store); err != nil {
		slog.Error("admin: failed to reload key cache", "error", err)
	}
	h.rateLimiter.RemoveKey(id)
	if h.perKeyStats != nil {
		h.perKeyStats.RemoveKey(id)
	}
	// FK cascade may have removed overrides for this key
	if h.overrideCache != nil {
		h.overrideCache.Reload()
	}

	slog.Info("admin: deleted key", "id", id)
	jsonOK(w, map[string]string{"status": "deleted"})
}

func (h *AdminHandler) revealKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	plain, err := h.store.GetKeyPlaintext(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	if plain == "" {
		jsonError(w, http.StatusGone, "该密钥创建于旧版本，无法恢复明文")
		return
	}
	jsonOK(w, map[string]string{"key": plain})
}

// --- Logs ---

func (h *AdminHandler) queryLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var keyID int64
	if v := q.Get("key_id"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid key_id")
			return
		}
		keyID = parsed
	}

	from := time.Now().UTC().Add(-24 * time.Hour)
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid from date (use RFC3339)")
			return
		}
		from = t
	}

	to := time.Now().UTC()
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid to date (use RFC3339)")
			return
		}
		to = t
	}

	limit := 100
	if v := q.Get("limit"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}

	logs, err := h.store.QueryLogs(keyID, from, to, limit)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}

	jsonOK(w, logs)
}

// getKeyUsageStats 按下游 Key 聚合请求统计。
func (h *AdminHandler) getKeyUsageStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.GetKeyUsageStats()
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if stats == nil {
		stats = []store.KeyUsageStats{}
	}
	jsonOK(w, stats)
}

// --- Model Whitelist ---

func (h *AdminHandler) listModelWhitelist(w http.ResponseWriter, r *http.Request) {
	entries, err := h.store.ListModelWhitelist()
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	jsonOK(w, entries)
}

func (h *AdminHandler) addModelWhitelist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pattern string `json:"pattern"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Pattern == "" {
		jsonError(w, http.StatusBadRequest, "pattern is required")
		return
	}
	// Validate glob syntax to prevent invalid patterns from silently blocking all requests
	if _, err := path.Match(req.Pattern, "test"); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid pattern %q: %v", req.Pattern, err))
		return
	}
	entry, err := h.store.AddModelWhitelist(req.Pattern)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	slog.Info("admin: added model whitelist pattern", "pattern", entry.Pattern)
	if h.modelFilter != nil {
		h.modelFilter.Reload()
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, entry)
}

func (h *AdminHandler) deleteModelWhitelist(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.DeleteModelWhitelist(id); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	slog.Info("admin: deleted model whitelist pattern", "id", id)
	if h.modelFilter != nil {
		h.modelFilter.Reload()
	}
	jsonOK(w, map[string]string{"status": "deleted"})
}

// batchDeleteModelWhitelist 批量删除白名单条目。
func (h *AdminHandler) batchDeleteModelWhitelist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(req.IDs) == 0 {
		jsonError(w, http.StatusBadRequest, "ids is required")
		return
	}
	deleted, err := h.store.BatchDeleteModelWhitelist(req.IDs)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	slog.Info("admin: batch deleted model whitelist patterns", "ids", req.IDs, "deleted", deleted)
	if h.modelFilter != nil {
		h.modelFilter.Reload()
	}
	jsonOK(w, map[string]interface{}{"status": "deleted", "deleted": deleted})
}

// --- Key-Upstream Bindings ---

// getAllKeyBindings 返回所有 Key 的显式上游绑定，供管理页批量渲染。
// 结果里不存在的 Key 应按“未绑定 = 允许全部健康上游”解释。
func (h *AdminHandler) getAllKeyBindings(w http.ResponseWriter, r *http.Request) {
	bindings, err := h.store.GetAllKeyBindings()
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	jsonOK(w, bindings)
}

// getKeyUpstreams 返回单个 Key 的显式绑定集合。
// 即使没有绑定也返回空数组，减少前端对三态值的处理复杂度。
func (h *AdminHandler) getKeyUpstreams(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Verify key exists
	if _, err := h.store.LookupKeyByID(id); err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("key %d not found", id))
		return
	}
	ids, err := h.store.GetKeyUpstreamIDs(id)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if ids == nil {
		ids = []int64{}
	}
	jsonOK(w, map[string]interface{}{"upstream_ids": ids})
}

// setKeyUpstreams 以全量覆盖方式更新某个 Key 的上游白名单。
// 空数组表示清空显式绑定并回退到默认路由，而不是把该 Key 锁死。
func (h *AdminHandler) setKeyUpstreams(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Verify key exists
	if _, err := h.store.LookupKeyByID(id); err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("key %d not found", id))
		return
	}
	var req struct {
		UpstreamIDs []int64 `json:"upstream_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// 先在 handler 层做存在性校验和去重，把输入问题收敛为 400，
	// 避免落到存储层后变成外键或唯一约束错误。
	if len(req.UpstreamIDs) > 0 {
		upstreams, err := h.store.ListUpstreams()
		if err != nil {
			slog.Error("admin: store error", "error", err)
			jsonError(w, http.StatusInternalServerError, "internal error")
			return
		}
		validIDs := make(map[int64]bool, len(upstreams))
		for _, u := range upstreams {
			validIDs[u.ID] = true
		}
		seen := make(map[int64]bool, len(req.UpstreamIDs))
		var deduped []int64
		for _, uid := range req.UpstreamIDs {
			if !validIDs[uid] {
				jsonError(w, http.StatusBadRequest, fmt.Sprintf("upstream %d not found", uid))
				return
			}
			if !seen[uid] {
				seen[uid] = true
				deduped = append(deduped, uid)
			}
		}
		req.UpstreamIDs = deduped
	}
	if err := h.store.SetKeyUpstreams(id, req.UpstreamIDs); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	slog.Info("admin: updated key upstream bindings", "key_id", id, "upstream_ids", req.UpstreamIDs)
	jsonOK(w, map[string]interface{}{"status": "updated", "upstream_ids": req.UpstreamIDs})
}

// --- Key Model Overrides ---

// getAllKeyModelOverrides 返回所有 Key 的模型路由覆盖，供管理页批量渲染。
func (h *AdminHandler) getAllKeyModelOverrides(w http.ResponseWriter, r *http.Request) {
	overrides, err := h.store.GetAllKeyModelOverrides()
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if overrides == nil {
		overrides = make(map[int64][]store.KeyModelOverride)
	}
	jsonOK(w, overrides)
}

// getKeyModelOverrides 返回单个 Key 的模型路由覆盖。
func (h *AdminHandler) getKeyModelOverrides(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.store.LookupKeyByID(id); err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("key %d not found", id))
		return
	}
	overrides, err := h.store.GetKeyModelOverrides(id)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if overrides == nil {
		overrides = []store.KeyModelOverride{}
	}
	jsonOK(w, overrides)
}

// setKeyModelOverrides 以全量覆盖方式更新某个 Key 的模型路由覆盖。
// 空数组表示清空所有覆盖。
func (h *AdminHandler) setKeyModelOverrides(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.store.LookupKeyByID(id); err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("key %d not found", id))
		return
	}

	var req struct {
		Overrides []struct {
			ModelPattern string `json:"model_pattern"`
			UpstreamID   int64  `json:"upstream_id"`
		} `json:"overrides"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Validate and deduplicate overrides
	if len(req.Overrides) > 0 {
		upstreams, err := h.store.ListUpstreams()
		if err != nil {
			slog.Error("admin: store error", "error", err)
			jsonError(w, http.StatusInternalServerError, "internal error")
			return
		}
		validIDs := make(map[int64]bool, len(upstreams))
		for _, u := range upstreams {
			validIDs[u.ID] = true
		}

		seen := make(map[string]bool)
		for _, o := range req.Overrides {
			if o.ModelPattern == "" {
				jsonError(w, http.StatusBadRequest, "model_pattern is required")
				return
			}
			// Validate pattern syntax
			if _, err := path.Match(o.ModelPattern, "test"); err != nil {
				jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid pattern %q: %v", o.ModelPattern, err))
				return
			}
			if !validIDs[o.UpstreamID] {
				jsonError(w, http.StatusBadRequest, fmt.Sprintf("upstream %d not found", o.UpstreamID))
				return
			}
			// Deduplicate
			key := fmt.Sprintf("%s:%d", o.ModelPattern, o.UpstreamID)
			if seen[key] {
				jsonError(w, http.StatusBadRequest, fmt.Sprintf("duplicate override: pattern=%q upstream=%d", o.ModelPattern, o.UpstreamID))
				return
			}
			seen[key] = true
		}
	}

	// Convert to store input
	inputs := make([]store.KeyModelOverrideInput, len(req.Overrides))
	for i, o := range req.Overrides {
		inputs[i] = store.KeyModelOverrideInput{
			ModelPattern: o.ModelPattern,
			UpstreamID:   o.UpstreamID,
		}
	}

	if err := h.store.SetKeyModelOverrides(id, inputs); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Refresh override cache
	if h.overrideCache != nil {
		h.overrideCache.Reload()
	}

	slog.Info("admin: updated key model overrides", "key_id", id, "count", len(inputs))
	jsonOK(w, map[string]interface{}{"status": "updated", "count": len(inputs)})
}

// healthy_upstreams 取自 DynamicProxy 当前可用的健康列表，
// 而不是数据库静态配置，这样管理端看到的状态才和实际转发行为一致。

func (h *AdminHandler) getStatus(w http.ResponseWriter, r *http.Request) {
	var auditDropped int64
	if h.auditLogger != nil {
		auditDropped = h.auditLogger.DroppedCount()
	}

	// Healthy upstreams
	type upstreamInfo struct {
		ID                int64  `json:"id"`
		Name              string `json:"name"`
		URL               string `json:"url"`
		KeyCount          int    `json:"key_count"`
		KeySchedulingMode string `json:"key_scheduling_mode"`
	}
	var healthyList []upstreamInfo
	if all := h.dynamicProxy.GetAllUpstreams(); len(all) > 0 {
		for _, u := range all {
			mode := u.KeySchedulingMode
			if mode == "" {
				mode = "round-robin"
			}
			healthyList = append(healthyList, upstreamInfo{
				ID: u.ID, Name: u.Name, URL: u.BaseURL.String(),
				KeyCount: len(u.APIKeys), KeySchedulingMode: mode,
			})
		}
	}
	// 固定返回空数组，避免前端在 null 和 [] 之间做额外分支。
	if healthyList == nil {
		healthyList = []upstreamInfo{}
	}

	// Key count
	// 统计信息采用尽力而为策略；即使计数失败，也不让状态接口整体不可用。
	keyCount, _ := h.store.CountKeys()

	// Today's request count
	now := time.Now().UTC()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	todayRequests, _ := h.store.CountLogsSince(startOfDay)

	// Uptime
	uptime := time.Since(startTime).Truncate(time.Second).String()

	status := map[string]interface{}{
		"healthy_upstreams": healthyList,
		"total_keys":        keyCount,
		"today_requests":    todayRequests,
		"audit_dropped":     auditDropped,
		"uptime":            uptime,
		"version":           h.version,
		"timestamp":         time.Now().UTC(),
		"active_requests":   h.dynamicProxy.ActiveRequests(),
	}

	// 实时 RPM/RPS 统计；计数器可能未初始化（单元测试场景），用尽力而为策略。
	if h.requestCounter != nil {
		status["rpm"] = h.requestCounter.RPM()
		status["rps"] = fmt.Sprintf("%.1f", h.requestCounter.RPS())
	} else {
		status["rpm"] = 0
		status["rps"] = "0.0"
	}

	// 连接池统计
	status["transport_pool"] = proxy.TransportPoolStats()

	jsonOK(w, status)
}

// getKeyRPM 返回所有活跃 Key 的实时 RPM 数据。
// 拆分为独立端点，避免 /status 轮询时携带大量 per-key 数据。
func (h *AdminHandler) getKeyRPM(w http.ResponseWriter, r *http.Request) {
	if h.perKeyStats == nil {
		jsonOK(w, map[string]int{})
		return
	}
	jsonOK(w, h.perKeyStats.AllActiveRPMs())
}

// --- Test Models ---

func (h *AdminHandler) listTestModels(w http.ResponseWriter, r *http.Request) {
	protocol := r.URL.Query().Get("protocol")
	models, err := h.store.ListTestModels(protocol)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if models == nil {
		models = []store.TestModel{}
	}
	jsonOK(w, models)
}

func (h *AdminHandler) createTestModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" {
		jsonError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Protocol == "" {
		req.Protocol = "openai"
	}
	m, err := h.store.CreateTestModel(req.Name, req.Protocol)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, m)
}

func (h *AdminHandler) updateTestModel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" {
		jsonError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := h.store.UpdateTestModel(id, req.Name, req.Protocol); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	jsonOK(w, map[string]interface{}{"id": id, "name": req.Name, "protocol": req.Protocol})
}

func (h *AdminHandler) deleteTestModel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.DeleteTestModel(id); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	jsonOK(w, map[string]string{"status": "deleted"})
}

// --- Dashboard ---

func (h *AdminHandler) serveDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(dashboardHTML)
}

// --- Helpers ---

func parseID(r *http.Request) (int64, error) {
	vars := mux.Vars(r)
	return strconv.ParseInt(vars["id"], 10, 64)
}

func parseAPIKeyRowID(r *http.Request) (int64, error) {
	keyID, err := strconv.ParseInt(mux.Vars(r)["key_id"], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid key_id")
	}
	return keyID, nil
}

func cleanAPIKeys(keys []string) []string {
	seen := make(map[string]bool, len(keys))
	cleaned := make([]string, 0, len(keys))
	for _, key := range keys {
		for _, value := range normalizeAPIKeyValues(key) {
			if seen[value] {
				continue
			}
			seen[value] = true
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}

func normalizeAPIKeyValues(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ','
	})
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			values = append(values, field)
		}
	}
	return values
}

func jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// tryRemoveTransport 仅当没有其他上游仍在使用同一 proxyURL 时，
// 才从缓存中移除对应 transport 并关闭空闲连接。
// excludeID 是正在删除或修改的上游 ID，在判断"是否还有其他"时排除它。
func (h *AdminHandler) tryRemoveTransport(proxyURL string, excludeID int64) {
	upstreams, err := h.store.ListUpstreams()
	if err != nil {
		slog.Warn("admin: failed to list upstreams for transport cleanup", "error", err)
		return
	}
	for _, u := range upstreams {
		if u.ID != excludeID && u.ProxyURL == proxyURL {
			// 还有其他上游在用同一代理，保留 transport
			return
		}
	}
	proxy.RemoveTransport(proxyURL)
}

func jsonError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// applyCFHeaders 在传出请求上注入 Cloudflare 绕过所需的 Cookie 和 User-Agent。
// 仅当 clearance 非空时才设置，避免覆盖默认行为。
func applyCFHeaders(req *http.Request, clearance, userAgent string) {
	if clearance != "" {
		req.AddCookie(&http.Cookie{Name: "cf_clearance", Value: clearance})
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
}

// validateBaseURL enforces https and rejects private/loopback/link-local IPs.
func validateBaseURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("base_url must use http or https scheme")
	}

	host := parsed.Hostname()
	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("cannot resolve host %s: %w", host, err)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("base_url resolves to private/loopback IP %s", ipStr)
		}
	}

	return nil
}

// validateProxyURL 校验代理地址格式，仅允许 http/https/socks5 协议，且必须包含主机名。
func validateProxyURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid proxy_url: %w", err)
	}
	switch parsed.Scheme {
	case "http", "https", "socks5":
		// ok
	default:
		return fmt.Errorf("proxy_url must use http, https, or socks5 scheme")
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("proxy_url must include a hostname")
	}
	return nil
}

// sanitizeProxyForLog 抹除 proxy URL 中的用户凭据，防止密码写入日志。
func sanitizeProxyForLog(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "<invalid>"
	}
	parsed.User = nil
	return parsed.String()
}

// testUpstreamProxy 通过上游配置的代理对其 base_url 发 GET /v1/models 请求，
// 携带 API Key 验证连通性并返回支持的模型列表。
func (h *AdminHandler) testUpstreamProxy(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 解析可选的 CF 绕过参数
	var cfOpts struct {
		CFClearance string `json:"cf_clearance"`
		CFUserAgent string `json:"cf_user_agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cfOpts); err != nil && err.Error() != "EOF" {
		jsonError(w, http.StatusBadRequest, "invalid CF params JSON")
		return
	}

	upstream, err := h.store.GetUpstream(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("upstream %d not found", id))
		return
	}

	// 构造带代理的 HTTP client
	transport, err := proxy.BuildTransport(upstream.ProxyURL)
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("invalid proxy config: %v", err),
		})
		return
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		// 禁止跟随重定向，防止 302 到内网地址的 SSRF 绕过
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	testURL := strings.TrimRight(upstream.BaseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(r.Context(), "GET", testURL, nil)
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("构造请求失败: %v", err),
		})
		return
	}
	var firstKey string
	if len(upstream.APIKeys) > 0 {
		firstKey = upstream.APIKeys[0]
	}
	req.Header.Set("Authorization", "Bearer "+firstKey)
	applyCFHeaders(req, cfOpts.CFClearance, cfOpts.CFUserAgent)

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success":    false,
			"error":      err.Error(),
			"latency_ms": latency.Milliseconds(),
		})
		return
	}
	defer resp.Body.Close()

	// 限制读取 256KB
	body, err := io.ReadAll(io.LimitReader(resp.Body, 262144))
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success":     false,
			"error":       fmt.Sprintf("读取响应失败: %v", err),
			"status_code": resp.StatusCode,
			"latency_ms":  latency.Milliseconds(),
		})
		return
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		jsonOK(w, map[string]interface{}{
			"success":     false,
			"error":       fmt.Sprintf("上游返回 HTTP %d", resp.StatusCode),
			"status_code": resp.StatusCode,
			"latency_ms":  latency.Milliseconds(),
		})
		return
	}

	// 解析 OpenAI 风格的 /v1/models 响应
	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	var models []string
	if err := json.Unmarshal(body, &modelsResp); err == nil && len(modelsResp.Data) > 0 {
		for _, m := range modelsResp.Data {
			if m.ID != "" {
				models = append(models, m.ID)
			}
		}
	}

	jsonOK(w, map[string]interface{}{
		"success":     true,
		"status_code": resp.StatusCode,
		"latency_ms":  latency.Milliseconds(),
		"models":      models,
	})
}

// checkUpstreamQuota 通过 new-api 的 /api/usage/token 接口查询上游 Key 的剩余额度。
// 仅解析 new-api 风格的响应（code=true, data.object="token_usage"），
// 非 new-api 格式时返回截断的原始内容供管理员在 DevTools 中查看。
func (h *AdminHandler) checkUpstreamQuota(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 解析可选的 CF 绕过参数
	var cfOpts struct {
		CFClearance string `json:"cf_clearance"`
		CFUserAgent string `json:"cf_user_agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cfOpts); err != nil && err.Error() != "EOF" {
		jsonError(w, http.StatusBadRequest, "invalid CF params JSON")
		return
	}

	upstream, err := h.store.GetUpstream(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("upstream %d not found", id))
		return
	}

	// 构造带代理的 HTTP client，复用 testUpstreamProxy 的安全策略
	transport, err := proxy.BuildTransport(upstream.ProxyURL)
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("invalid proxy config: %v", err),
		})
		return
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		// 允许同域重定向（如 /api/usage/token → /api/usage/token/），
		// 但跨域时阻止，防止 Authorization 头泄露到意外域名
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("cross-host redirect blocked: %s → %s", via[0].URL.Host, req.URL.Host)
			}
			return nil
		},
	}

	quotaURL := strings.TrimRight(upstream.BaseURL, "/") + "/api/usage/token"
	req, err := http.NewRequestWithContext(r.Context(), "GET", quotaURL, nil)
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("构造请求失败: %v", err),
		})
		return
	}
	var firstKey string
	if len(upstream.APIKeys) > 0 {
		firstKey = upstream.APIKeys[0]
	}
	req.Header.Set("Authorization", "Bearer "+firstKey)
	applyCFHeaders(req, cfOpts.CFClearance, cfOpts.CFUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("请求失败: %v", err),
		})
		return
	}
	defer resp.Body.Close()

	// 限制读取 64KB，防止大响应体占满内存（同时避免截断合法 JSON）
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("读取响应失败: %v", err),
		})
		return
	}

	// 非 2xx 状态码直接报错
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		jsonOK(w, map[string]interface{}{
			"success":        false,
			"message":        fmt.Sprintf("上游返回 HTTP %d", resp.StatusCode),
			"origin_content": string(body),
		})
		return
	}

	// Content-Type 非 JSON 时直接走"非 new-api"分支（大小写不敏感）
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(strings.ToLower(ct), "json") {
		jsonOK(w, map[string]interface{}{
			"success":        false,
			"message":        "error",
			"origin_content": string(body),
		})
		return
	}

	// 尝试解析 new-api 风格的响应
	var apiResp struct {
		Code    interface{} `json:"code"`
		Message string      `json:"message"`
		Data    struct {
			Object             string          `json:"object"`
			Name               string          `json:"name"`
			TotalAvailable     int64           `json:"total_available"`
			TotalGranted       int64           `json:"total_granted"`
			TotalUsed          int64           `json:"total_used"`
			UnlimitedQuota     bool            `json:"unlimited_quota"`
			ExpiresAt          int64           `json:"expires_at"`
			ModelLimitsEnabled bool            `json:"model_limits_enabled"`
			ModelLimits        map[string]bool `json:"model_limits"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		jsonOK(w, map[string]interface{}{
			"success":        false,
			"message":        "error",
			"origin_content": string(body),
		})
		return
	}

	// 校验是否为 new-api 风格：data.object 必须是 "token_usage"
	if apiResp.Data.Object != "token_usage" {
		jsonOK(w, map[string]interface{}{
			"success":        false,
			"message":        "error",
			"origin_content": string(body),
		})
		return
	}

	// 处理 code=false 的情况（new-api 返回错误）
	codeOK := false
	switch v := apiResp.Code.(type) {
	case bool:
		codeOK = v
	case float64:
		codeOK = v != 0
	}
	if !codeOK {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("上游返回错误: %s", apiResp.Message),
		})
		return
	}

	// 成功：返回解析后的额度信息
	jsonOK(w, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"name":                 apiResp.Data.Name,
			"total_available":      apiResp.Data.TotalAvailable,
			"total_granted":        apiResp.Data.TotalGranted,
			"total_used":           apiResp.Data.TotalUsed,
			"unlimited_quota":      apiResp.Data.UnlimitedQuota,
			"expires_at":           apiResp.Data.ExpiresAt,
			"model_limits_enabled": apiResp.Data.ModelLimitsEnabled,
			"model_limits":         apiResp.Data.ModelLimits,
		},
	})
}

// --- Per-Key API Key Management ---

// listUpstreamAPIKeys 返回指定上游的所有 API Key 及启用状态。
func (h *AdminHandler) listUpstreamAPIKeys(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	keys, err := h.store.GetUpstreamAllAPIKeys(id)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	type keyInfo struct {
		RowID   int64  `json:"row_id"`
		Key     string `json:"key"`
		Enabled bool   `json:"enabled"`
	}
	result := make([]keyInfo, len(keys))
	for i, k := range keys {
		result[i] = keyInfo{RowID: k.RowID, Key: k.Key, Enabled: k.Enabled}
	}
	jsonOK(w, result)
}

// setAPIKeyEnabled 启用或禁用指定上游的某个 API Key。
func (h *AdminHandler) setAPIKeyEnabled(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	keyID, err := parseAPIKeyRowID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.store.SetAPIKeyEnabled(id, keyID, req.Enabled); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	// Trigger re-probe so key changes take effect immediately.
	go h.prober.ProbeNow()
	slog.Info("admin: updated api key enabled", "upstream_id", id, "key_id", keyID, "enabled", req.Enabled)
	jsonOK(w, map[string]interface{}{"upstream_id": id, "key_id": keyID, "enabled": req.Enabled})
}

// addUpstreamAPIKeys 为上游追加一个或多个 API Key，不影响现有 Key。
func (h *AdminHandler) addUpstreamAPIKeys(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		APIKey  string   `json:"api_key"`
		APIKeys []string `json:"api_keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	keys := cleanAPIKeys(req.APIKeys)
	if req.APIKey != "" {
		keys = append(keys, normalizeAPIKeyValues(req.APIKey)...)
	}
	keys = cleanAPIKeys(keys)
	if len(keys) == 0 {
		jsonError(w, http.StatusBadRequest, "api_keys is required")
		return
	}
	added, err := h.store.AddUpstreamAPIKeys(id, keys)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	go h.prober.ProbeNow()
	slog.Info("admin: added upstream api keys", "upstream_id", id, "count", len(keys))
	jsonOK(w, map[string]interface{}{"status": "created", "count": len(keys), "api_keys": added})
}

// deleteUpstreamAPIKey 删除上游中的单个 API Key。
func (h *AdminHandler) deleteUpstreamAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	keyID, err := parseAPIKeyRowID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.DeleteUpstreamAPIKey(id, keyID); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	go h.prober.ProbeNow()
	slog.Info("admin: deleted upstream api key", "upstream_id", id, "key_id", keyID)
	jsonOK(w, map[string]interface{}{"status": "deleted", "upstream_id": id, "key_id": keyID})
}

// testUpstreamAPIKey 测试指定上游的某个 API Key，支持选择协议、模型和提示词。
func (h *AdminHandler) testUpstreamAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	keyID, err := parseAPIKeyRowID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req struct {
		Protocol    string `json:"protocol"`      // "openai" or "anthropic"
		Model       string `json:"model"`         // 测试模型
		Prompt      string `json:"prompt"`        // 测试提示词
		CFClearance string `json:"cf_clearance"`  // CF 绕过
		CFUserAgent string `json:"cf_user_agent"` // CF 绕过
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Protocol == "" {
		req.Protocol = "openai"
	}
	if req.Prompt == "" {
		req.Prompt = "你是什么模型？"
	}
	if req.Model == "" {
		switch req.Protocol {
		case "anthropic":
			req.Model = "claude-sonnet-4-20250514"
		case "responses":
			req.Model = "gpt-4o"
		default:
			req.Model = "gpt-4o-mini"
		}
	}

	// 获取上游信息
	upstream, err := h.store.GetUpstream(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("upstream %d not found", id))
		return
	}

	// 找到指定 row ID 的 Key（keyID=0 表示无鉴权，跳过查找）
	var targetKey string
	if keyID != 0 {
		keyInfos, err := h.store.GetUpstreamAllAPIKeys(id)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "failed to load api keys")
			return
		}
		for _, ki := range keyInfos {
			if ki.RowID == keyID {
				targetKey = ki.Key
				break
			}
		}
		if targetKey == "" {
			jsonError(w, http.StatusNotFound, fmt.Sprintf("api key %d not found", keyID))
			return
		}
	}

	// 构造请求体
	var body []byte
	var testURL string
	var headers map[string]string

	switch req.Protocol {
	case "anthropic":
		testURL = strings.TrimRight(upstream.BaseURL, "/") + "/v1/messages"
		body, _ = json.Marshal(map[string]interface{}{
			"model":      req.Model,
			"max_tokens": 100,
			"messages":   []map[string]string{{"role": "user", "content": req.Prompt}},
		})
		headers = map[string]string{
			"anthropic-version": "2023-06-01",
		}
		if targetKey != "" {
			if upstream.AuthMode == "oauth" {
				headers["Authorization"] = "Bearer " + targetKey
			} else {
				headers["x-api-key"] = targetKey
			}
		}
	case "responses":
		testURL = strings.TrimRight(upstream.BaseURL, "/") + "/v1/responses"
		body, _ = json.Marshal(map[string]interface{}{
			"model":  req.Model,
			"input":  req.Prompt,
			"stream": false,
		})
		headers = map[string]string{}
		if targetKey != "" {
			headers["Authorization"] = "Bearer " + targetKey
		}
	default: // openai
		testURL = strings.TrimRight(upstream.BaseURL, "/") + "/v1/chat/completions"
		body, _ = json.Marshal(map[string]interface{}{
			"model":      req.Model,
			"max_tokens": 100,
			"messages":   []map[string]string{{"role": "user", "content": req.Prompt}},
		})
		headers = map[string]string{}
		if targetKey != "" {
			headers["Authorization"] = "Bearer " + targetKey
		}
	}

	// 构造带代理的 HTTP client
	transport, err := proxy.BuildTransport(upstream.ProxyURL)
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("invalid proxy config: %v", err),
		})
		return
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	httpReq, err := http.NewRequestWithContext(r.Context(), "POST", testURL, strings.NewReader(string(body)))
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("构造请求失败: %v", err),
		})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	applyCFHeaders(httpReq, req.CFClearance, req.CFUserAgent)

	start := time.Now()
	resp, err := client.Do(httpReq)
	latency := time.Since(start)

	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success":    false,
			"error":      err.Error(),
			"latency_ms": latency.Milliseconds(),
		})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 262144))
	if err != nil {
		jsonOK(w, map[string]interface{}{
			"success":     false,
			"error":       fmt.Sprintf("读取响应失败: %v", err),
			"status_code": resp.StatusCode,
			"latency_ms":  latency.Milliseconds(),
		})
		return
	}

	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	result := map[string]interface{}{
		"success":     success,
		"status_code": resp.StatusCode,
		"latency_ms":  latency.Milliseconds(),
		"model":       req.Model,
		"protocol":    req.Protocol,
	}
	if !success {
		result["error"] = fmt.Sprintf("上游返回 HTTP %d", resp.StatusCode)
		// 原始响应体（管理面板直接展示，便于排查 OAuth/鉴权等非标准错误结构）
		if len(respBody) > 0 {
			result["raw_body"] = string(respBody)
		}
		// 尝试解析常见错误字段
		var errResp struct {
			Error interface{} `json:"error"`
			// Anthropic 有时用 type/message 顶层字段
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(respBody, &errResp) == nil {
			switch e := errResp.Error.(type) {
			case string:
				if e != "" {
					result["error_message"] = e
				}
			case map[string]interface{}:
				if msg, ok := e["message"].(string); ok && msg != "" {
					result["error_message"] = msg
				} else if t, ok := e["type"].(string); ok && t != "" {
					result["error_message"] = t
				}
			}
			if result["error_message"] == nil {
				if errResp.Message != "" {
					result["error_message"] = errResp.Message
				} else if errResp.Type != "" {
					result["error_message"] = errResp.Type
				}
			}
		}
	} else {
		// 尝试提取回复内容
		switch req.Protocol {
		case "anthropic":
			var anthropicResp struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
				Model string `json:"model"`
			}
			if json.Unmarshal(respBody, &anthropicResp) == nil {
				if len(anthropicResp.Content) > 0 {
					result["reply"] = anthropicResp.Content[0].Text
				}
				result["actual_model"] = anthropicResp.Model
			}
		case "responses":
			var responsesResp struct {
				Output []struct {
					Type    string `json:"type"`
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"output"`
				Model string `json:"model"`
			}
			if json.Unmarshal(respBody, &responsesResp) == nil {
				for _, item := range responsesResp.Output {
					if item.Type == "message" {
						for _, c := range item.Content {
							if c.Type == "output_text" && c.Text != "" {
								result["reply"] = c.Text
								break
							}
						}
					}
				}
				result["actual_model"] = responsesResp.Model
			}
		default: // openai
			var openaiResp struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
				Model string `json:"model"`
			}
			if json.Unmarshal(respBody, &openaiResp) == nil {
				if len(openaiResp.Choices) > 0 {
					result["reply"] = openaiResp.Choices[0].Message.Content
				}
				result["actual_model"] = openaiResp.Model
			}
		}
	}
	jsonOK(w, result)
}

// --- Upstream Model Patterns ---

// getAllUpstreamModelPatterns 返回所有上游的模型模式，供管理页批量渲染。
func (h *AdminHandler) getAllUpstreamModelPatterns(w http.ResponseWriter, r *http.Request) {
	patterns, err := h.store.GetAllUpstreamModelPatterns()
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	jsonOK(w, patterns)
}

// getUpstreamModelPatterns 返回单个上游的模型模式列表。
func (h *AdminHandler) getUpstreamModelPatterns(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.store.GetUpstream(id); err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("upstream %d not found", id))
		return
	}
	patterns, err := h.store.GetUpstreamModelPatterns(id)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if patterns == nil {
		patterns = []string{}
	}
	jsonOK(w, map[string]interface{}{"patterns": patterns})
}

// setUpstreamModelPatterns 以全量覆盖方式更新上游的模型模式。
// 写入前做格式校验（path.Match 预检）、trim 空白、去重。
func (h *AdminHandler) setUpstreamModelPatterns(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.store.GetUpstream(id); err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("upstream %d not found", id))
		return
	}
	var req struct {
		Patterns *[]string `json:"patterns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// patterns 字段必填，防止 {} 漏传意外清空配置。
	// 传 {"patterns":[]} 为合法清空操作。
	if req.Patterns == nil {
		jsonError(w, http.StatusBadRequest, "missing required field: patterns")
		return
	}

	// 校验、trim、去重
	seen := make(map[string]bool, len(*req.Patterns))
	var cleaned []string
	for _, p := range *req.Patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue // 跳过空串
		}
		// 用 path.Match 预检 pattern 语法合法性
		if _, err := path.Match(p, ""); err != nil {
			jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid pattern %q: %v", p, err))
			return
		}
		if !seen[p] {
			seen[p] = true
			cleaned = append(cleaned, p)
		}
	}

	if err := h.store.SetUpstreamModelPatterns(id, cleaned); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// 即时触发 prober 刷新，让模型模式立即生效
	go h.prober.ProbeNow()
	slog.Info("admin: updated upstream model patterns", "upstream_id", id, "patterns", cleaned)
	jsonOK(w, map[string]interface{}{"status": "updated", "patterns": cleaned})
}

// --- Upstream Declared Models ---

func (h *AdminHandler) getAllUpstreamDeclaredModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.store.GetAllUpstreamDeclaredModels()
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	jsonOK(w, models)
}

func (h *AdminHandler) getUpstreamDeclaredModels(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.store.GetUpstream(id); err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("upstream %d not found", id))
		return
	}
	models, err := h.store.GetUpstreamDeclaredModels(id)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if models == nil {
		models = []string{}
	}
	jsonOK(w, map[string]interface{}{"models": models})
}

func (h *AdminHandler) setUpstreamDeclaredModels(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.store.GetUpstream(id); err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("upstream %d not found", id))
		return
	}
	var req struct {
		Models *[]string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Models == nil {
		jsonError(w, http.StatusBadRequest, "missing required field: models")
		return
	}

	seen := make(map[string]bool, len(*req.Models))
	var cleaned []string
	for _, m := range *req.Models {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if !seen[m] {
			seen[m] = true
			cleaned = append(cleaned, m)
		}
	}

	if err := h.store.SetUpstreamDeclaredModels(id, cleaned); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if h.modelFilter != nil {
		h.modelFilter.ReloadDeclaredModels()
	}
	slog.Info("admin: updated upstream declared models", "upstream_id", id, "count", len(cleaned))
	jsonOK(w, map[string]interface{}{"status": "updated", "models": cleaned})
}

func (h *AdminHandler) getSettings(w http.ResponseWriter, r *http.Request) {
	threshold := h.dynamicProxy.AutoDisableThreshold.Load()
	jsonOK(w, map[string]interface{}{
		"auto_disable_threshold": threshold,
	})
}

func (h *AdminHandler) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AutoDisableThreshold *int `json:"auto_disable_threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.AutoDisableThreshold != nil {
		val := *body.AutoDisableThreshold
		if val < 0 {
			jsonError(w, http.StatusBadRequest, "threshold must be >= 0")
			return
		}
		if err := h.store.SetSetting("auto_disable_threshold", strconv.Itoa(val)); err != nil {
			slog.Error("admin: failed to save setting", "error", err)
			jsonError(w, http.StatusInternalServerError, "failed to save")
			return
		}
		h.dynamicProxy.AutoDisableThreshold.Store(int64(val))
		slog.Info("admin: updated auto_disable_threshold", "value", val)
	}
	jsonOK(w, map[string]interface{}{"status": "updated"})
}
