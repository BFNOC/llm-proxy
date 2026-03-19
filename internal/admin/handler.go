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
var Version = "2.1.0" // set by main or build flags

type AdminHandler struct {
	store        *store.Store
	keyCache     *middleware.KeyCache
	rateLimiter  *middleware.PerKeyRPMLimiter
	prober       *proxy.UpstreamProber
	dynamicProxy *proxy.DynamicProxy
	auditLogger  *middleware.AuditLogger
	modelFilter  *middleware.ModelFilter
	adminToken   string
}

func NewAdminHandler(
	s *store.Store,
	kc *middleware.KeyCache,
	rl *middleware.PerKeyRPMLimiter,
	prober *proxy.UpstreamProber,
	dp *proxy.DynamicProxy,
	al *middleware.AuditLogger,
	mf *middleware.ModelFilter,
	adminToken string,
) *AdminHandler {
	return &AdminHandler{
		store:        s,
		keyCache:     kc,
		rateLimiter:  rl,
		prober:       prober,
		dynamicProxy: dp,
		auditLogger:  al,
		modelFilter:  mf,
		adminToken:   adminToken,
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

	// Keys
	api.HandleFunc("/keys", h.listKeys).Methods("GET")
	api.HandleFunc("/keys", h.createKey).Methods("POST")
	api.HandleFunc("/keys/{id}", h.updateKey).Methods("PUT")
	api.HandleFunc("/keys/{id}", h.deleteKey).Methods("DELETE")

	// Logs
	api.HandleFunc("/logs", h.queryLogs).Methods("GET")

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

	// Status
	api.HandleFunc("/status", h.getStatus).Methods("GET")

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
	// Redact API keys in listing
	type upstreamResponse struct {
		ID        int64     `json:"id"`
		Name      string    `json:"name"`
		BaseURL   string    `json:"base_url"`
		ProxyURL  string    `json:"proxy_url"`
		Priority  int       `json:"priority"`
		Enabled   bool      `json:"enabled"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	result := make([]upstreamResponse, len(upstreams))
	for i, u := range upstreams {
		result[i] = upstreamResponse{
			ID: u.ID, Name: u.Name, BaseURL: u.BaseURL, ProxyURL: u.ProxyURL,
			Priority: u.Priority, Enabled: u.Enabled, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
		}
	}
	jsonOK(w, result)
}

func (h *AdminHandler) createUpstream(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		BaseURL  string `json:"base_url"`
		APIKey   string `json:"api_key"`
		ProxyURL string `json:"proxy_url"`
		Priority int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" || req.BaseURL == "" || req.APIKey == "" {
		jsonError(w, http.StatusBadRequest, "name, base_url, and api_key are required")
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

	upstream, err := h.store.CreateUpstream(req.Name, req.BaseURL, req.APIKey, req.Priority, req.ProxyURL)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	slog.Info("admin: created upstream", "id", upstream.ID, "name", upstream.Name, "proxy_url", sanitizeProxyForLog(req.ProxyURL))
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
		Name     *string `json:"name"`
		BaseURL  *string `json:"base_url"`
		APIKey   *string `json:"api_key"`
		ProxyURL *string `json:"proxy_url"`
		Priority *int    `json:"priority"`
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
	baseURL := existing.BaseURL
	if req.BaseURL != nil {
		baseURL = *req.BaseURL
	}
	apiKey := existing.APIKey
	if req.APIKey != nil && *req.APIKey != "" {
		apiKey = *req.APIKey
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

	upstream, err := h.store.UpdateUpstream(id, name, baseURL, apiKey, priority, enabled, proxyURL)
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

	// Reload key cache + clean rate limiter
	if err := h.keyCache.Reload(h.store); err != nil {
		slog.Error("admin: failed to reload key cache", "error", err)
	}
	h.rateLimiter.RemoveKey(id)

	slog.Info("admin: deleted key", "id", id)
	jsonOK(w, map[string]string{"status": "deleted"})
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

// getStatus 返回运行时视角的状态快照。
// healthy_upstreams 取自 DynamicProxy 当前可用的健康列表，
// 而不是数据库静态配置，这样管理端看到的状态才和实际转发行为一致。

func (h *AdminHandler) getStatus(w http.ResponseWriter, r *http.Request) {
	var auditDropped int64
	if h.auditLogger != nil {
		auditDropped = h.auditLogger.DroppedCount()
	}

	// Healthy upstreams
	type upstreamInfo struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	var healthyList []upstreamInfo
	if all := h.dynamicProxy.GetAllUpstreams(); len(all) > 0 {
		for _, u := range all {
			healthyList = append(healthyList, upstreamInfo{ID: u.ID, Name: u.Name, URL: u.BaseURL.String()})
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
		"version":           Version,
		"timestamp":         time.Now().UTC(),
	}
	jsonOK(w, status)
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
	req.Header.Set("Authorization", "Bearer "+upstream.APIKey)

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
	req.Header.Set("Authorization", "Bearer "+upstream.APIKey)

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
			Object             string `json:"object"`
			Name               string `json:"name"`
			TotalAvailable     int64  `json:"total_available"`
			TotalGranted       int64  `json:"total_granted"`
			TotalUsed          int64  `json:"total_used"`
			UnlimitedQuota     bool   `json:"unlimited_quota"`
			ExpiresAt          int64  `json:"expires_at"`
			ModelLimitsEnabled bool   `json:"model_limits_enabled"`
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
		},
	})
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

