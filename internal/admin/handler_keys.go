package admin

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// --- Key ---

func (h *AdminHandler) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.store.ListKeys()
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	type keyResponse struct {
		ID            int64     `json:"id"`
		KeyPrefix     string    `json:"key_prefix"`
		Name          string    `json:"name"`
		RPMLimit      int       `json:"rpm_limit"`
		MaxConcurrent int       `json:"max_concurrent"`
		Enabled       bool      `json:"enabled"`
		CreatedAt     time.Time `json:"created_at"`
		UpdatedAt     time.Time `json:"updated_at"`
	}
	result := make([]keyResponse, len(keys))
	for i, k := range keys {
		result[i] = keyResponse{
			ID: k.ID, KeyPrefix: k.KeyPrefix, Name: k.Name,
			RPMLimit: k.RPMLimit, MaxConcurrent: k.MaxConcurrent, Enabled: k.Enabled,
			CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt,
		}
	}
	jsonOK(w, result)
}

func (h *AdminHandler) createKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"name"`
		RPMLimit      int    `json:"rpm_limit"`
		MaxConcurrent int    `json:"max_concurrent"`
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

	// 若指定了 max_concurrent，额外更新该字段
	if req.MaxConcurrent > 0 {
		mc := req.MaxConcurrent
		if _, err := h.store.UpdateKey(key.ID, key.Name, key.RPMLimit, key.Enabled, &mc); err != nil {
			slog.Warn("admin: failed to set max_concurrent on new key", "error", err)
		}
	}

	// 重新加载 Key 缓存
	if err := h.keyCache.Reload(h.store); err != nil {
		slog.Error("admin: failed to reload key cache", "error", err)
	}

	slog.Info("admin: created key", "id", key.ID, "name", key.Name, "max_concurrent", req.MaxConcurrent)
	w.WriteHeader(http.StatusCreated)
	// 明文仅返回一次
	jsonOK(w, map[string]interface{}{
		"id":             key.ID,
		"key":            plaintext,
		"name":           key.Name,
		"rpm_limit":      key.RPMLimit,
		"max_concurrent": req.MaxConcurrent,
	})
}

func (h *AdminHandler) updateKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 读取现有 Key，保留请求中未提供的字段。
	existing, err := h.store.LookupKeyByID(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("key %d not found", id))
		return
	}

	var req struct {
		Name          *string `json:"name"`
		RPMLimit      *int    `json:"rpm_limit"`
		Enabled       *bool   `json:"enabled"`
		MaxConcurrent *int    `json:"max_concurrent"`
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

	key, err := h.store.UpdateKey(id, name, rpmLimit, enabled, req.MaxConcurrent)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// 重新加载 Key 缓存
	if err := h.keyCache.Reload(h.store); err != nil {
		slog.Error("admin: failed to reload key cache", "error", err)
	}

	slog.Info("admin: updated key", "id", key.ID)
	resp := map[string]interface{}{"id": key.ID, "name": key.Name, "rpm_limit": key.RPMLimit, "enabled": key.Enabled}
	if req.MaxConcurrent != nil {
		resp["max_concurrent"] = *req.MaxConcurrent
	}
	jsonOK(w, resp)
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

	// 重新加载 Key 缓存 + 清理限流器 + 清理统计
	if err := h.keyCache.Reload(h.store); err != nil {
		slog.Error("admin: failed to reload key cache", "error", err)
	}
	h.rateLimiter.RemoveKey(id)
	if h.perKeyStats != nil {
		h.perKeyStats.RemoveKey(id)
	}
	// FK 级联可能已删除此 Key 的覆盖规则/绑定
	if h.overrideCache != nil {
		h.overrideCache.Reload()
	}
	if h.bindingCache != nil {
		h.bindingCache.Reload()
	}
	h.reloadFullRecordingPolicy()

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
