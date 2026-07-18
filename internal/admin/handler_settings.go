package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Instawork/llm-proxy/internal/middleware"
	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
)

func (h *AdminHandler) getSettings(w http.ResponseWriter, r *http.Request) {
	threshold := h.dynamicProxy.AutoDisableThreshold.Load()
	retentionStr, _ := h.store.GetSetting("log_retention_days", "15")
	retentionDays, _ := strconv.Atoi(retentionStr)
	if retentionDays <= 0 {
		retentionDays = 15
	}
	slowThresholdStr, _ := h.store.GetSetting("slow_request_threshold_ms", "30000")
	slowThresholdMs, _ := strconv.Atoi(slowThresholdStr)
	if slowThresholdMs < 0 {
		slowThresholdMs = 30000
	}
	fullRecording, err := h.store.GetFullRecordingConfig()
	if err != nil {
		slog.Error("admin: 读取全量记录设置失败", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	jsonOK(w, map[string]interface{}{
		"auto_disable_threshold":    threshold,
		"log_retention_days":        retentionDays,
		"slow_request_threshold_ms": slowThresholdMs,
		"full_recording_enabled":    fullRecording.Enabled,
		"full_recording_all_keys":   fullRecording.AllKeys,
		"full_recording_key_ids":    fullRecording.DownstreamKeyIDs,
	})
}

func (h *AdminHandler) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AutoDisableThreshold   *int     `json:"auto_disable_threshold"`
		LogRetentionDays       *int     `json:"log_retention_days"`
		SlowRequestThresholdMs *int     `json:"slow_request_threshold_ms"`
		FullRecordingEnabled   *bool    `json:"full_recording_enabled"`
		FullRecordingAllKeys   *bool    `json:"full_recording_all_keys"`
		FullRecordingKeyIDs    *[]int64 `json:"full_recording_key_ids"`
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
	if body.LogRetentionDays != nil {
		val := *body.LogRetentionDays
		if val < 1 {
			jsonError(w, http.StatusBadRequest, "log_retention_days must be >= 1")
			return
		}
		if err := h.store.SetSetting("log_retention_days", strconv.Itoa(val)); err != nil {
			slog.Error("admin: failed to save setting", "error", err)
			jsonError(w, http.StatusInternalServerError, "failed to save")
			return
		}
		slog.Info("admin: updated log_retention_days", "value", val)
	}
	if body.SlowRequestThresholdMs != nil {
		val := *body.SlowRequestThresholdMs
		if val < 0 {
			jsonError(w, http.StatusBadRequest, "slow_request_threshold_ms must be >= 0")
			return
		}
		if err := h.store.SetSetting("slow_request_threshold_ms", strconv.Itoa(val)); err != nil {
			slog.Error("admin: failed to save setting", "error", err)
			jsonError(w, http.StatusInternalServerError, "failed to save")
			return
		}
		slog.Info("admin: updated slow_request_threshold_ms", "value", val)
	}
	if body.FullRecordingEnabled != nil || body.FullRecordingAllKeys != nil || body.FullRecordingKeyIDs != nil {
		if err := h.updateFullRecordingConfig(body.FullRecordingEnabled, body.FullRecordingAllKeys, body.FullRecordingKeyIDs); err != nil {
			if errors.Is(err, errAuditLoggingDisabled) {
				jsonError(w, http.StatusConflict, err.Error())
				return
			}
			if errors.Is(err, errInvalidFullRecordingKey) {
				jsonError(w, http.StatusBadRequest, err.Error())
				return
			}
			slog.Error("admin: 更新全量记录设置失败", "error", err)
			jsonError(w, http.StatusInternalServerError, "failed to save")
			return
		}
	}
	jsonOK(w, map[string]interface{}{"status": "updated"})
}

// --- 配置导入导出 ---

// exportConfig 导出完整配置为 JSON 文件（上游、Key、绑定、白名单、设置等）。
func (h *AdminHandler) exportConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.store.ExportConfig()
	if err != nil {
		slog.Error("admin: export config failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=llm-proxy-config.json")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

// importConfig 从 JSON 导入配置。
func (h *AdminHandler) importConfig(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10 MB
	var cfg store.ConfigExport
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.store.ImportConfig(&cfg); err != nil {
		slog.Error("admin: import config failed", "error", err)
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("import failed: %v", err))
		return
	}
	// 导入后刷新所有内存缓存
	if err := h.keyCache.Reload(h.store); err != nil {
		slog.Error("admin: failed to reload key cache after import", "error", err)
	}
	if h.overrideCache != nil {
		h.overrideCache.Reload()
	}
	if h.bindingCache != nil {
		h.bindingCache.Reload()
	}
	if h.modelFilter != nil {
		h.modelFilter.Reload()
		h.modelFilter.ReloadDeclaredModels()
	}
	go func() {
		defer func() { recover() }()
		h.prober.ProbeNow()
	}()
	slog.Info("admin: config imported successfully")
	jsonOK(w, map[string]interface{}{"status": "imported", "message": "配置导入成功，所有缓存已刷新"})
}

// getHeaderCapture 返回抓取启用标志 + 最近的快照（最新的排在前面）。
// 每一项都标注了 client_family：claude_code | codex | other。
func (h *AdminHandler) getHeaderCapture(w http.ResponseWriter, r *http.Request) {
	if h.headerCapture == nil {
		jsonOK(w, map[string]interface{}{"enabled": false, "captures": []interface{}{}})
		return
	}
	enabled, items := h.headerCapture.Snapshot()
	// 为 UI 徽标补充信息，不修改 middleware 层的存储。
	type captureView struct {
		middleware.CapturedHeaderRequest
		ClientFamily string `json:"client_family"`
	}
	out := make([]captureView, 0, len(items))
	for _, it := range items {
		out = append(out, captureView{
			CapturedHeaderRequest: it,
			ClientFamily:          proxy.DetectInboundClientFamily(it.Path, it.Flat),
		})
	}
	jsonOK(w, map[string]interface{}{
		"enabled":  enabled,
		"captures": out,
		"hint":     "完整抓取入站 /v1 Header + Body（含密钥明文）。支持 Claude Code 与 Codex。仅在可信本机开启。CC: ANTHROPIC_BASE_URL；Codex: OPENAI_BASE_URL / 代理指向本机 /v1。",
	})
}

// updateHeaderCapture 启用或禁用抓取：{"enabled": true}。
func (h *AdminHandler) updateHeaderCapture(w http.ResponseWriter, r *http.Request) {
	if h.headerCapture == nil {
		jsonError(w, http.StatusServiceUnavailable, "header capture not available")
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Enabled == nil {
		jsonError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	h.headerCapture.SetEnabled(*body.Enabled)
	slog.Info("admin: header capture toggled", "enabled", *body.Enabled)
	jsonOK(w, map[string]interface{}{"enabled": *body.Enabled})
}

// clearHeaderCapture 清空已存储的快照（不改变启用标志）。
func (h *AdminHandler) clearHeaderCapture(w http.ResponseWriter, r *http.Request) {
	if h.headerCapture == nil {
		jsonError(w, http.StatusServiceUnavailable, "header capture not available")
		return
	}
	h.headerCapture.Clear()
	jsonOK(w, map[string]interface{}{"status": "cleared"})
}
