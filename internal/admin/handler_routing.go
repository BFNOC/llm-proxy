package admin

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/Instawork/llm-proxy/internal/store"
)

// --- 模型白名单 ---

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
	// 校验 glob 语法，防止非法模式静默拦截所有请求
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

// --- Key-上游绑定关系 ---

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
	// 校验 Key 存在
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
	// 校验 Key 存在
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
	if h.bindingCache != nil {
		h.bindingCache.Reload()
	}
	slog.Info("admin: updated key upstream bindings", "key_id", id, "upstream_ids", req.UpstreamIDs)
	jsonOK(w, map[string]interface{}{"status": "updated", "upstream_ids": req.UpstreamIDs})
}

// --- Key 模型路由覆盖 ---

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

	// 校验并去重覆盖规则
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
			// 校验模式语法
			if _, err := path.Match(o.ModelPattern, "test"); err != nil {
				jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid pattern %q: %v", o.ModelPattern, err))
				return
			}
			if !validIDs[o.UpstreamID] {
				jsonError(w, http.StatusBadRequest, fmt.Sprintf("upstream %d not found", o.UpstreamID))
				return
			}
			// 去重
			key := fmt.Sprintf("%s:%d", o.ModelPattern, o.UpstreamID)
			if seen[key] {
				jsonError(w, http.StatusBadRequest, fmt.Sprintf("duplicate override: pattern=%q upstream=%d", o.ModelPattern, o.UpstreamID))
				return
			}
			seen[key] = true
		}
	}

	// 转换为 store 层输入结构
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

	// 刷新覆盖规则缓存
	if h.overrideCache != nil {
		h.overrideCache.Reload()
	}

	slog.Info("admin: updated key model overrides", "key_id", id, "count", len(inputs))
	jsonOK(w, map[string]interface{}{"status": "updated", "count": len(inputs)})
}

// --- 上游模型模式 ---

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
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	slog.Info("admin: updated upstream model patterns", "upstream_id", id, "patterns", cleaned)
	jsonOK(w, map[string]interface{}{"status": "updated", "patterns": cleaned})
}

// --- 上游声明模型 ---

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
