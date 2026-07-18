package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Instawork/llm-proxy/internal/store"
)

var (
	errAuditLoggingDisabled    = errors.New("audit logging is disabled by YAML configuration")
	errInvalidFullRecordingKey = errors.New("invalid full recording key")
)

const (
	maxLogSessionRecords = 5000
	maxLogExportRecords  = 10000
	logExportBatchSize   = 250
)

func parseLogQuery(r *http.Request, defaultLimit, maxLimit int) (store.LogQuery, error) {
	query := r.URL.Query()
	filter := store.LogQuery{
		From:      time.Now().UTC().Add(-24 * time.Hour),
		To:        time.Now().UTC(),
		Limit:     defaultLimit,
		SessionID: strings.TrimSpace(query.Get("session_id")),
		Model:     strings.TrimSpace(query.Get("model")),
		Path:      strings.TrimSpace(query.Get("path")),
	}
	if filter.SessionID != "" {
		filter.From = time.Unix(0, 0).UTC()
	}

	if value := query.Get("key_id"); value != "" {
		keyID, err := strconv.ParseInt(value, 10, 64)
		if err != nil || keyID <= 0 {
			return filter, fmt.Errorf("invalid key_id")
		}
		filter.KeyID = keyID
	}
	if value := query.Get("from"); value != "" {
		from, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return filter, fmt.Errorf("invalid from date (use RFC3339)")
		}
		filter.From = from
	}
	if value := query.Get("to"); value != "" {
		to, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return filter, fmt.Errorf("invalid to date (use RFC3339)")
		}
		filter.To = to
	}
	if filter.From.After(filter.To) {
		return filter, fmt.Errorf("from must not be after to")
	}
	if value := query.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit <= 0 {
			return filter, fmt.Errorf("invalid limit")
		}
		filter.Limit = limit
	}
	if filter.Limit > maxLimit {
		filter.Limit = maxLimit
	}
	if value := query.Get("full_only"); value != "" {
		fullOnly, err := strconv.ParseBool(value)
		if err != nil {
			return filter, fmt.Errorf("invalid full_only")
		}
		filter.FullOnly = fullOnly
	}
	if value := query.Get("status_code"); value != "" {
		statusCode, err := strconv.Atoi(value)
		if err != nil || statusCode < 100 || statusCode > 599 {
			return filter, fmt.Errorf("invalid status_code")
		}
		filter.StatusCode = statusCode
	}
	return filter, nil
}

func (h *AdminHandler) getLogDetail(w http.ResponseWriter, r *http.Request) {
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
	var detail interface{}
	if logEntry.HasFullRecord {
		storedDetail, detailErr := h.store.GetRequestLogDetail(id)
		if detailErr != nil {
			slog.Error("admin: 读取日志详情失败", "log_id", id, "error", detailErr)
			jsonError(w, http.StatusInternalServerError, "internal error")
			return
		}
		detail = requestDetailResponse(storedDetail)
	}
	jsonOK(w, map[string]interface{}{"log": logEntry, "detail": detail})
}

func (h *AdminHandler) queryLogSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	filter, err := parseLogQuery(r, 100, 1000)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	sessions, err := h.store.QueryLogSessions(filter)
	if err != nil {
		slog.Error("admin: 查询连续会话失败", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if sessions == nil {
		sessions = []store.RequestLogSession{}
	}
	jsonOK(w, sessions)
}

func (h *AdminHandler) getLogSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	filter, err := parseLogQuery(r, 1000, maxLogSessionRecords)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter.SessionID = strings.TrimSpace(r.URL.Query().Get("session_id"))
	if filter.SessionID == "" {
		jsonError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	if filter.KeyID == 0 {
		jsonError(w, http.StatusBadRequest, "key_id is required")
		return
	}
	requestedLimit := filter.Limit
	filter.Limit = requestedLimit + 1

	logs, err := h.store.QueryLogsFiltered(filter)
	if err != nil {
		slog.Error("admin: 查询会话详情失败", "session_id", filter.SessionID, "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	truncated := len(logs) > requestedLimit
	if truncated {
		logs = logs[:requestedLimit]
	}
	details, err := h.store.GetRequestLogDetails(fullRecordLogIDs(logs))
	if err != nil {
		slog.Error("admin: 批量读取会话记录详情失败", "session_id", filter.SessionID, "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	records := make([]map[string]interface{}, 0, len(logs))
	for i := len(logs) - 1; i >= 0; i-- {
		detail := details[logs[i].ID]
		if detail == nil {
			slog.Error("admin: 会话记录缺少完整详情", "log_id", logs[i].ID)
			jsonError(w, http.StatusInternalServerError, "internal error")
			return
		}
		records = append(records, map[string]interface{}{
			"log":    logs[i],
			"detail": requestDetailResponse(detail),
		})
	}
	jsonOK(w, map[string]interface{}{
		"session_id": filter.SessionID,
		"key_id":     filter.KeyID,
		"records":    records,
		"truncated":  truncated,
		"limit":      requestedLimit,
	})
}

func (h *AdminHandler) exportLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	filter, err := parseLogQuery(r, maxLogExportRecords+1, maxLogExportRecords+1)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter.Limit = maxLogExportRecords + 1
	sessionLimit, err := parseOptionalPositiveInt(r, "session_limit", 1000)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	noMatchingSessions := false
	if sessionLimit > 0 {
		sessionFilter := filter
		sessionFilter.Limit = sessionLimit
		sessions, sessionErr := h.store.QueryLogSessions(sessionFilter)
		if sessionErr != nil {
			slog.Error("admin: 查询导出会话失败", "error", sessionErr)
			jsonError(w, http.StatusInternalServerError, "internal error")
			return
		}
		filter.SessionKeys = make([]store.LogSessionKey, 0, len(sessions))
		for _, session := range sessions {
			filter.SessionKeys = append(filter.SessionKeys, store.LogSessionKey{
				DownstreamKeyID: session.DownstreamKeyID,
				SessionID:       session.SessionID,
			})
		}
		noMatchingSessions = len(filter.SessionKeys) == 0
	}
	var logs []store.RequestLog
	if !noMatchingSessions {
		logs, err = h.store.QueryLogsFiltered(filter)
	}
	if err != nil {
		slog.Error("admin: 查询日志导出失败", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	truncated := len(logs) > maxLogExportRecords
	if truncated {
		logs = logs[:maxLogExportRecords]
	}

	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="request-logs.ndjson"`)
	w.Header().Set("X-Export-Truncated", strconv.FormatBool(truncated))
	w.Header().Set("X-Export-Record-Limit", strconv.Itoa(maxLogExportRecords))
	encoder := json.NewEncoder(w)
	for start := 0; start < len(logs); start += logExportBatchSize {
		end := start + logExportBatchSize
		if end > len(logs) {
			end = len(logs)
		}
		batch := logs[start:end]
		details, detailErr := h.store.GetRequestLogDetails(fullRecordLogIDs(batch))
		if detailErr != nil {
			slog.Error("admin: 批量读取导出日志详情失败", "error", detailErr)
			return
		}
		for i := range batch {
			var detail interface{}
			if storedDetail := details[batch[i].ID]; storedDetail != nil {
				detail = requestDetailResponse(storedDetail)
			}
			if err := encoder.Encode(map[string]interface{}{"log": batch[i], "detail": detail}); err != nil {
				slog.Warn("admin: 写入日志导出响应失败", "error", err)
				return
			}
		}
	}
}

func parseOptionalPositiveInt(r *http.Request, name string, max int) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 || parsed > max {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}

func fullRecordLogIDs(logs []store.RequestLog) []int64 {
	ids := make([]int64, 0, len(logs))
	for _, logEntry := range logs {
		if !logEntry.HasFullRecord {
			continue
		}
		ids = append(ids, logEntry.ID)
	}
	return ids
}

func requestDetailResponse(detail *store.RequestLogDetail) map[string]interface{} {
	return map[string]interface{}{
		"request_log_id":          detail.RequestLogID,
		"session_id":              detail.SessionID,
		"session_source":          detail.SessionSource,
		"session_preview":         detail.SessionPreview,
		"response_id":             detail.ResponseID,
		"parent_response_id":      detail.ParentResponseID,
		"method":                  detail.Method,
		"raw_query":               detail.RawQuery,
		"request_headers":         rawJSONOrEmptyObject(detail.RequestHeadersJSON),
		"request_body":            detail.RequestBody,
		"request_body_truncated":  detail.RequestBodyTruncated,
		"response_headers":        rawJSONOrEmptyObject(detail.ResponseHeadersJSON),
		"response_body":           detail.ResponseBody,
		"response_body_truncated": detail.ResponseBodyTruncated,
		"capture_status":          detail.CaptureStatus,
	}
}

func rawJSONOrEmptyObject(value string) json.RawMessage {
	if !json.Valid([]byte(value)) {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(value)
}

func (h *AdminHandler) updateFullRecordingConfig(enabled, allKeys *bool, keyIDs *[]int64) error {
	config, err := h.store.GetFullRecordingConfig()
	if err != nil {
		return fmt.Errorf("read current full recording config: %w", err)
	}
	if enabled != nil {
		config.Enabled = *enabled
	}
	if allKeys != nil {
		config.AllKeys = *allKeys
	}
	if keyIDs != nil {
		config.DownstreamKeyIDs = append([]int64(nil), (*keyIDs)...)
	}
	if config.Enabled && h.auditLogger == nil {
		return errAuditLoggingDisabled
	}

	seen := make(map[int64]struct{}, len(config.DownstreamKeyIDs))
	cleaned := make([]int64, 0, len(config.DownstreamKeyIDs))
	for _, id := range config.DownstreamKeyIDs {
		if id <= 0 {
			return fmt.Errorf("%w: key %d not found", errInvalidFullRecordingKey, id)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		if _, err := h.store.LookupKeyByID(id); err != nil {
			return fmt.Errorf("%w: key %d not found", errInvalidFullRecordingKey, id)
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	sort.Slice(cleaned, func(i, j int) bool { return cleaned[i] < cleaned[j] })
	if config.AllKeys {
		cleaned = nil
	}
	if config.Enabled && !config.AllKeys && len(cleaned) == 0 {
		return fmt.Errorf("%w: at least one key is required for selected scope", errInvalidFullRecordingKey)
	}
	config.DownstreamKeyIDs = cleaned

	if err := h.store.SetFullRecordingConfig(config); err != nil {
		return err
	}
	h.fullRecording.Update(config)
	slog.Info("admin: 更新全量记录设置", "enabled", config.Enabled, "key_count", len(config.DownstreamKeyIDs))
	return nil
}

func (h *AdminHandler) reloadFullRecordingPolicy() {
	config, err := h.store.GetFullRecordingConfig()
	if err != nil {
		slog.Error("admin: 刷新全量记录策略失败", "error", err)
		return
	}
	h.fullRecording.Update(config)
}
