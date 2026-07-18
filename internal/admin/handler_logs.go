package admin

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Instawork/llm-proxy/internal/store"
)

// --- 日志 ---

func (h *AdminHandler) queryLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	filter, err := parseLogQuery(r, 100, 1000)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	logs, err := h.store.QueryLogsFiltered(filter)
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

// --- 请求重放 ---

// replayRequest 根据日志 ID 返回请求元数据和可用的完整请求，供前端预填测试对话框。
// 该接口只准备重放数据，不主动向外部上游发起请求。
func (h *AdminHandler) replayRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	logEntry, err := h.store.GetLogByID(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("日志 %d 未找到", id))
		return
	}

	providerStyle := logEntry.ProviderStyle
	if providerStyle == "" {
		providerStyle = "openai"
	}

	response := map[string]interface{}{
		"log_id":          logEntry.ID,
		"upstream_name":   logEntry.UpstreamName,
		"model":           logEntry.Model,
		"path":            logEntry.Path,
		"provider_style":  providerStyle,
		"has_full_record": logEntry.HasFullRecord,
	}
	if logEntry.HasFullRecord {
		detail, detailErr := h.store.GetRequestLogDetail(id)
		if detailErr != nil {
			slog.Error("admin: 读取重放详情失败", "log_id", id, "error", detailErr)
			jsonError(w, http.StatusInternalServerError, "internal error")
			return
		}
		response["method"] = detail.Method
		response["raw_query"] = detail.RawQuery
		response["request_headers"] = rawJSONOrEmptyObject(detail.RequestHeadersJSON)
		response["request_body"] = detail.RequestBody
		response["session_id"] = detail.SessionID
		response["session_source"] = detail.SessionSource
	}

	slog.Info("admin: 请求重放预填", "log_id", logEntry.ID)
	jsonOK(w, response)
}
