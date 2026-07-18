package admin

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Instawork/llm-proxy/internal/store"
)

// --- 上游 ---

func (h *AdminHandler) listUpstreams(w http.ResponseWriter, r *http.Request) {
	upstreams, err := h.store.ListUpstreams()
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// 为方便管理员操作，API Key 现在以明文返回
	type apiKeyInfo struct {
		RowID   int64  `json:"row_id"`
		Key     string `json:"key"`
		Enabled bool   `json:"enabled"`
	}
	type upstreamResponse struct {
		ID                 int64        `json:"id"`
		Name               string       `json:"name"`
		BaseURL            string       `json:"base_url"`
		APIKeys            []string     `json:"api_keys"`
		APIKeyDetails      []apiKeyInfo `json:"api_key_details"`
		ProxyURL           string       `json:"proxy_url"`
		Priority           int          `json:"priority"`
		Enabled            bool         `json:"enabled"`
		KeySchedulingMode  string       `json:"key_scheduling_mode"`
		AuthMode           string       `json:"auth_mode"`
		Remark             string       `json:"remark"`
		WebSocketEnabled   bool         `json:"websocket_enabled"`
		AutoDiscoverModels bool         `json:"auto_discover_models"`
		LastModelDiscovery *time.Time   `json:"last_model_discovery"`
		CreatedAt          time.Time    `json:"created_at"`
		UpdatedAt          time.Time    `json:"updated_at"`
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
		// 处理 last_model_discovery 可空时间
		var lastDiscovery *time.Time
		if u.LastModelDiscovery != nil && !u.LastModelDiscovery.IsZero() {
			lastDiscovery = u.LastModelDiscovery
		}
		result[i] = upstreamResponse{
			ID: u.ID, Name: u.Name, BaseURL: u.BaseURL, APIKeys: keys, APIKeyDetails: details,
			ProxyURL: u.ProxyURL, Priority: u.Priority, Enabled: u.Enabled,
			KeySchedulingMode: u.KeySchedulingMode, AuthMode: authMode, Remark: u.Remark,
			WebSocketEnabled: u.WebSocketEnabled, AutoDiscoverModels: u.AutoDiscoverModels,
			LastModelDiscovery: lastDiscovery, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
		}
	}
	jsonOK(w, result)
}

func (h *AdminHandler) createUpstream(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name               string   `json:"name"`
		BaseURL            string   `json:"base_url"`
		APIKey             string   `json:"api_key"`  // 向后兼容单 Key
		APIKeys            []string `json:"api_keys"` // 新多 Key 字段
		ProxyURL           string   `json:"proxy_url"`
		Priority           int      `json:"priority"`
		KeySchedulingMode  string   `json:"key_scheduling_mode"`
		AuthMode           string   `json:"auth_mode"`
		Remark             string   `json:"remark"`
		WebSocketEnabled   bool     `json:"websocket_enabled"`
		AutoDiscoverModels bool     `json:"auto_discover_models"`
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

	// SSRF 校验
	if err := validateBaseURL(req.BaseURL); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 代理 URL 校验
	if req.ProxyURL != "" {
		if err := validateProxyURL(req.ProxyURL); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	upstream, err := h.store.CreateUpstream(req.Name, req.BaseURL, apiKeys, req.Priority, req.ProxyURL, schedulingMode, authMode, req.Remark, req.WebSocketEnabled, false, 0)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// 创建后若指定了模型自动发现，则额外调用 store 设置
	if req.AutoDiscoverModels {
		if err := h.store.SetAutoDiscoverModels(upstream.ID, true); err != nil {
			slog.Warn("admin: 设置模型自动发现失败", "upstream_id", upstream.ID, "error", err)
		} else {
			go func() {
				defer func() { recover() }()
				h.prober.ProbeNow()
			}()
		}
	}
	slog.Info("admin: created upstream", "id", upstream.ID, "name", upstream.Name, "key_count", len(apiKeys), "proxy_url", sanitizeProxyForLog(req.ProxyURL))
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, map[string]interface{}{"id": upstream.ID, "name": upstream.Name, "base_url": upstream.BaseURL, "priority": upstream.Priority, "auto_discover_models": req.AutoDiscoverModels})
}

func (h *AdminHandler) updateUpstream(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 读取现有上游，保留请求中未提供的字段。
	existing, err := h.store.GetUpstream(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("upstream %d not found", id))
		return
	}

	var req struct {
		Name               *string   `json:"name"`
		BaseURL            *string   `json:"base_url"`
		APIKey             *string   `json:"api_key"`  // 向后兼容单 Key
		APIKeys            *[]string `json:"api_keys"` // 新多 Key 字段
		ProxyURL           *string   `json:"proxy_url"`
		Priority           *int      `json:"priority"`
		Enabled            *bool     `json:"enabled"`
		KeySchedulingMode  *string   `json:"key_scheduling_mode"`
		AuthMode           *string   `json:"auth_mode"`
		Remark             *string   `json:"remark"`
		WebSocketEnabled   *bool     `json:"websocket_enabled"`
		AutoDiscoverModels *bool     `json:"auto_discover_models"`
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

	upstream, err := h.store.UpdateUpstream(id, name, baseURL, apiKeys, priority, enabled, proxyURL, schedulingMode, authMode, remark, req.WebSocketEnabled)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// 代理配置变更时回收旧 transport 连接池（仅当没有其他上游复用该代理时）
	if proxyURL != existing.ProxyURL {
		h.tryRemoveTransport(existing.ProxyURL, id)
	}
	// 更新模型自动发现设置
	if req.AutoDiscoverModels != nil {
		if err := h.store.SetAutoDiscoverModels(id, *req.AutoDiscoverModels); err != nil {
			slog.Warn("admin: 设置模型自动发现失败", "upstream_id", id, "error", err)
		}
	}
	// 立即触发一次探活，让启用/禁用变更马上生效。
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	slog.Info("admin: updated upstream", "id", upstream.ID, "enabled", upstream.Enabled)
	jsonOK(w, map[string]interface{}{"id": upstream.ID, "name": upstream.Name, "enabled": upstream.Enabled})
}

func (h *AdminHandler) deleteUpstream(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.SoftDeleteUpstream(id); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// 立即触发一次探活，按需更新可用上游集合。
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	// FK 级联可能已删除引用此上游的覆盖规则/绑定
	if h.overrideCache != nil {
		h.overrideCache.Reload()
	}
	if h.bindingCache != nil {
		h.bindingCache.Reload()
	}
	if h.modelFilter != nil {
		h.modelFilter.ReloadDeclaredModels()
	}
	slog.Info("admin: soft-deleted upstream", "id", id)
	jsonOK(w, map[string]interface{}{"status": "deleted", "undo_seconds": 60})
}

// batchSetUpstreamEnabled 批量启用或禁用上游：{"ids":[1,2], "enabled":true}。
func (h *AdminHandler) batchSetUpstreamEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs     []int64 `json:"ids"`
		Enabled *bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(req.IDs) == 0 {
		jsonError(w, http.StatusBadRequest, "ids is required")
		return
	}
	if req.Enabled == nil {
		jsonError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	updated, err := h.store.BatchSetUpstreamEnabled(req.IDs, *req.Enabled)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	slog.Info("admin: batch set upstream enabled", "ids", req.IDs, "enabled", *req.Enabled, "updated", updated)
	jsonOK(w, map[string]interface{}{
		"status":  "updated",
		"enabled": *req.Enabled,
		"updated": updated,
	})
}

// batchDeleteUpstreams 批量删除上游：{"ids":[1,2]}。
func (h *AdminHandler) batchDeleteUpstreams(w http.ResponseWriter, r *http.Request) {
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
	// 删除前先快照代理 URL，供后续清理 transport 连接池使用。
	type proxyRef struct {
		id       int64
		proxyURL string
	}
	var refs []proxyRef
	for _, id := range req.IDs {
		if id <= 0 {
			continue
		}
		if u, err := h.store.GetUpstream(id); err == nil && u != nil {
			refs = append(refs, proxyRef{id: id, proxyURL: u.ProxyURL})
		}
	}
	// 批量软删除（与单个删除行为一致，支持撤销）
	var deleted int64
	for _, id := range req.IDs {
		if id <= 0 {
			continue
		}
		if err := h.store.SoftDeleteUpstream(id); err != nil {
			slog.Warn("admin: soft delete upstream failed", "id", id, "error", err)
			continue
		}
		deleted++
	}
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	if h.overrideCache != nil {
		h.overrideCache.Reload()
	}
	if h.bindingCache != nil {
		h.bindingCache.Reload()
	}
	if h.modelFilter != nil {
		h.modelFilter.ReloadDeclaredModels()
	}
	slog.Info("admin: batch deleted upstreams", "ids", req.IDs, "deleted", deleted)
	jsonOK(w, map[string]interface{}{"status": "deleted", "deleted": deleted})
}

// --- 模型自动发现 ---

// setAutoDiscoverModels 启用或禁用上游的模型自动发现功能。
func (h *AdminHandler) setAutoDiscoverModels(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
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

	if err := h.store.SetAutoDiscoverModels(id, req.Enabled); err != nil {
		slog.Error("admin: 设置模型自动发现失败", "upstream_id", id, "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// 启用时立即触发探活以发现模型
	if req.Enabled {
		go func() {
			defer func() { recover() }()
			h.prober.ProbeNow()
		}()
	}

	slog.Info("admin: 模型自动发现设置已更新", "upstream_id", id, "enabled", req.Enabled)
	jsonOK(w, map[string]interface{}{
		"status":               "updated",
		"auto_discover_models": req.Enabled,
	})
}

// --- 上游拖拽排序 ---

// reorderUpstreams 按前端传入的 ID 顺序重新排列上游优先级。
func (h *AdminHandler) reorderUpstreams(w http.ResponseWriter, r *http.Request) {
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
	// 校验所有 ID 为正整数
	for _, id := range req.IDs {
		if id <= 0 {
			jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid upstream id: %d", id))
			return
		}
	}

	if err := h.store.ReorderUpstreams(req.IDs); err != nil {
		slog.Error("admin: 上游排序失败", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// 触发探活以按新优先级重新加载上游
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()

	slog.Info("admin: 上游排序已更新", "ids", req.IDs)
	jsonOK(w, map[string]interface{}{"status": "reordered"})
}

// --- 快捷操作 ---

// pauseAllUpstreams 一键禁用所有上游。
func (h *AdminHandler) pauseAllUpstreams(w http.ResponseWriter, r *http.Request) {
	affected, err := h.store.SetAllUpstreamsEnabled(false)
	if err != nil {
		slog.Error("admin: 全部暂停上游失败", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	slog.Info("admin: 全部上游已暂停", "affected", affected)
	jsonOK(w, map[string]interface{}{"status": "paused", "affected": affected})
}

// resumeAllUpstreams 一键启用所有上游。
func (h *AdminHandler) resumeAllUpstreams(w http.ResponseWriter, r *http.Request) {
	affected, err := h.store.SetAllUpstreamsEnabled(true)
	if err != nil {
		slog.Error("admin: 全部恢复上游失败", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	slog.Info("admin: 全部上游已恢复", "affected", affected)
	jsonOK(w, map[string]interface{}{"status": "resumed", "affected": affected})
}

// --- 上游模板 ---

// listUpstreamTemplates 返回预置的上游模板列表。
func (h *AdminHandler) listUpstreamTemplates(w http.ResponseWriter, r *http.Request) {
	templates := store.GetUpstreamTemplates()
	if len(templates) == 0 {
		jsonOK(w, []store.UpstreamTemplate{})
		return
	}
	jsonOK(w, templates)
}

// --- 上游 RPM 限制 ---

// setUpstreamRPMLimit 设置上游的每分钟请求限制。
func (h *AdminHandler) setUpstreamRPMLimit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		RPMLimit int `json:"rpm_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.RPMLimit < 0 {
		jsonError(w, http.StatusBadRequest, "rpm_limit must be >= 0")
		return
	}
	if err := h.store.SetUpstreamRPMLimit(id, req.RPMLimit); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	slog.Info("admin: updated upstream rpm limit", "upstream_id", id, "rpm_limit", req.RPMLimit)
	jsonOK(w, map[string]interface{}{"status": "updated", "rpm_limit": req.RPMLimit})
}

// --- 熔断器配置 ---

// setCircuitBreakerConfig 设置上游的熔断器参数。
func (h *AdminHandler) setCircuitBreakerConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		Threshold       int `json:"threshold"`
		RecoverySeconds int `json:"recovery_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Threshold < 0 {
		jsonError(w, http.StatusBadRequest, "threshold must be >= 0")
		return
	}
	if req.RecoverySeconds < 0 {
		jsonError(w, http.StatusBadRequest, "recovery_seconds must be >= 0")
		return
	}
	if err := h.store.SetCircuitBreakerConfig(id, req.Threshold, req.RecoverySeconds); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	slog.Info("admin: updated circuit breaker config", "upstream_id", id, "threshold", req.Threshold, "recovery_seconds", req.RecoverySeconds)
	jsonOK(w, map[string]interface{}{"status": "updated", "threshold": req.Threshold, "recovery_seconds": req.RecoverySeconds})
}

// --- 软删除撤销 ---

// undoDeleteUpstream 撤销上游的软删除操作。
func (h *AdminHandler) undoDeleteUpstream(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.UndoDeleteUpstream(id); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	slog.Info("admin: undo delete upstream", "id", id)
	jsonOK(w, map[string]interface{}{"status": "restored"})
}

// listDeletedUpstreams 返回已软删除但尚未清理的上游列表。
func (h *AdminHandler) listDeletedUpstreams(w http.ResponseWriter, r *http.Request) {
	upstreams, err := h.store.ListDeletedUpstreams()
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	jsonOK(w, upstreams)
}
