package admin

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFullRecordingSettings_UpdateAndRead(t *testing.T) {
	handler, router, dataStore := setupTestAdminWithStore(t)
	t.Cleanup(handler.auditLogger.Stop)
	_, key, err := dataStore.CreateKey("recorded-key", 0)
	require.NoError(t, err)

	recorder := featDoReq(t, router, http.MethodPut, "/admin/api/settings", map[string]interface{}{
		"full_recording_enabled":  true,
		"full_recording_all_keys": false,
		"full_recording_key_ids":  []int64{key.ID, key.ID},
	})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.True(t, handler.fullRecording.ShouldRecord(key.ID))
	assert.False(t, handler.fullRecording.ShouldRecord(key.ID+100))

	recorder = featDoReq(t, router, http.MethodGet, "/admin/api/settings", nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	settings := featDecodeMap(t, recorder)
	assert.Equal(t, true, settings["full_recording_enabled"])
	assert.Equal(t, false, settings["full_recording_all_keys"])
	assert.Equal(t, []interface{}{float64(key.ID)}, settings["full_recording_key_ids"])
}

func TestFullRecordingSettings_AllKeysAndEmptySelectedScope(t *testing.T) {
	handler, router, _ := setupTestAdminWithStore(t)
	t.Cleanup(handler.auditLogger.Stop)
	recorder := featDoReq(t, router, http.MethodPut, "/admin/api/settings", map[string]interface{}{
		"full_recording_enabled":  true,
		"full_recording_all_keys": true,
		"full_recording_key_ids":  []int64{},
	})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.True(t, handler.fullRecording.ShouldRecord(1))
	assert.True(t, handler.fullRecording.ShouldRecord(999))

	recorder = featDoReq(t, router, http.MethodPut, "/admin/api/settings", map[string]interface{}{
		"full_recording_enabled":  true,
		"full_recording_all_keys": false,
		"full_recording_key_ids":  []int64{},
	})
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestFullRecordingSettings_EmptyKeyListDoesNotImplicitlyEnableAllKeys(t *testing.T) {
	handler, router, dataStore := setupTestAdminWithStore(t)
	t.Cleanup(handler.auditLogger.Stop)
	_, key, err := dataStore.CreateKey("selected-key", 0)
	require.NoError(t, err)

	recorder := featDoReq(t, router, http.MethodPut, "/admin/api/settings", map[string]interface{}{
		"full_recording_enabled":  true,
		"full_recording_all_keys": false,
		"full_recording_key_ids":  []int64{key.ID},
	})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	recorder = featDoReq(t, router, http.MethodPut, "/admin/api/settings", map[string]interface{}{
		"full_recording_key_ids": []int64{},
	})
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.True(t, handler.fullRecording.ShouldRecord(key.ID))
	assert.False(t, handler.fullRecording.ShouldRecord(key.ID+1))
}

func TestFullRecordingSettings_RejectsInvalidKeyAndDisabledAudit(t *testing.T) {
	handler, router, _ := setupTestAdminWithStore(t)
	t.Cleanup(handler.auditLogger.Stop)
	recorder := featDoReq(t, router, http.MethodPut, "/admin/api/settings", map[string]interface{}{
		"full_recording_enabled":  true,
		"full_recording_all_keys": false,
		"full_recording_key_ids":  []int64{99999},
	})
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	_, noAuditRouter := setupTestAdmin(t)
	recorder = doRequest(t, noAuditRouter, http.MethodPut, "/admin/api/settings", map[string]interface{}{
		"full_recording_enabled":  true,
		"full_recording_all_keys": true,
	})
	assert.Equal(t, http.StatusConflict, recorder.Code)
}

func TestLogSessionEndpointsAndNDJSONExport(t *testing.T) {
	handler, router, dataStore := setupTestAdminWithStore(t)
	t.Cleanup(handler.auditLogger.Stop)
	_, key, err := dataStore.CreateKey("session-key", 0)
	require.NoError(t, err)
	now := time.Now().UTC().Add(-2 * time.Second)
	require.NoError(t, dataStore.InsertRequestLogBatch([]store.RequestLog{
		{
			DownstreamKeyID: key.ID,
			ProviderStyle:   "anthropic",
			Path:            "/v1/messages",
			Model:           "claude-test",
			StatusCode:      200,
			CreatedAt:       now.Add(-time.Second),
			Detail: &store.RequestLogDetail{
				SessionID:      "session:older",
				SessionSource:  "body:metadata.user_id.session_id",
				SessionPreview: "another session",
				RequestBody:    `{"messages":[{"role":"user","content":"another session"}]}`,
				ResponseBody:   `{"content":[{"type":"text","text":"another answer"}]}`,
				CaptureStatus:  "captured",
			},
		},
		{
			DownstreamKeyID: key.ID,
			ProviderStyle:   "anthropic",
			Path:            "/v1/messages",
			Model:           "claude-test",
			StatusCode:      200,
			CreatedAt:       now.Add(-48 * time.Hour),
			Detail: &store.RequestLogDetail{
				SessionID:      "session:123",
				SessionSource:  "body:metadata.user_id.session_id",
				SessionPreview: "old turn",
				RequestBody:    `{"messages":[{"role":"user","content":"old turn"}]}`,
				ResponseBody:   `{"content":[{"type":"text","text":"old answer"}]}`,
				CaptureStatus:  "captured",
			},
		},
		{
			DownstreamKeyID: key.ID,
			ProviderStyle:   "anthropic",
			Path:            "/v1/messages",
			Model:           "claude-test",
			StatusCode:      200,
			CreatedAt:       now,
			Detail: &store.RequestLogDetail{
				SessionID:          "session:123",
				SessionSource:      "body:metadata.user_id.session_id",
				SessionPreview:     "first turn",
				RequestHeadersJSON: `{"Content-Type":["application/json"]}`,
				RequestBody:        `{"messages":[{"role":"user","content":"first turn"}]}`,
				ResponseBody:       `{"id":"msg_1","content":[{"type":"text","text":"answer"}]}`,
				CaptureStatus:      "captured",
			},
		},
		{
			DownstreamKeyID: key.ID,
			ProviderStyle:   "anthropic",
			Path:            "/v1/messages",
			Model:           "claude-test",
			StatusCode:      429,
			CreatedAt:       now.Add(time.Second),
			Detail: &store.RequestLogDetail{
				SessionID:     "session:123",
				SessionSource: "body:metadata.user_id.session_id",
				RequestBody:   `{"messages":[{"role":"user","content":"next turn"}]}`,
				ResponseBody:  `{"type":"error"}`,
				CaptureStatus: "captured",
			},
		},
	}))

	sessionsPath := "/admin/api/logs/sessions?key_id=" + url.QueryEscape(strings.TrimSpace(jsonNumber(key.ID)))
	recorder := featDoReq(t, router, http.MethodGet, sessionsPath, nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	sessions := featDecodeArray(t, recorder)
	require.Len(t, sessions, 2)
	assert.Equal(t, "session:123", sessions[0]["session_id"])
	assert.Equal(t, float64(2), sessions[0]["request_count"], "会话列表仍使用最近 24 小时范围")
	assert.Equal(t, float64(1), sessions[0]["error_count"])

	sessionPath := "/admin/api/logs/session?key_id=" + jsonNumber(key.ID) + "&session_id=" + url.QueryEscape("session:123")
	recorder = featDoReq(t, router, http.MethodGet, sessionPath, nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	session := featDecodeMap(t, recorder)
	records := session["records"].([]interface{})
	require.Len(t, records, 3, "会话详情默认返回完整时间线")
	assert.Equal(t, false, session["truncated"])
	assert.Equal(t, float64(1000), session["limit"])
	firstRecord := records[0].(map[string]interface{})
	firstDetail := firstRecord["detail"].(map[string]interface{})
	assert.Equal(t, "old turn", firstDetail["session_preview"])

	firstLog := firstRecord["log"].(map[string]interface{})
	logID := int64(firstLog["ID"].(float64))
	recorder = featDoReq(t, router, http.MethodGet, "/admin/api/logs/"+jsonNumber(logID), nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	detailResponse := featDecodeMap(t, recorder)
	assert.NotNil(t, detailResponse["detail"])

	recorder = featDoReq(t, router, http.MethodGet, sessionPath+"&limit=1", nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	limitedSession := featDecodeMap(t, recorder)
	assert.Equal(t, true, limitedSession["truncated"])
	assert.Equal(t, float64(1), limitedSession["limit"])
	limitedRecords := limitedSession["records"].([]interface{})
	require.Len(t, limitedRecords, 1)
	limitedDetail := limitedRecords[0].(map[string]interface{})["detail"].(map[string]interface{})
	assert.Contains(t, limitedDetail["request_body"], "next turn")

	exportPath := "/admin/api/logs/export?key_id=" + jsonNumber(key.ID) + "&session_id=" + url.QueryEscape("session:123")
	recorder = featDoReq(t, router, http.MethodGet, exportPath, nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Header().Get("Content-Type"), "application/x-ndjson")
	scanner := bufio.NewScanner(strings.NewReader(recorder.Body.String()))
	lineCount := 0
	for scanner.Scan() {
		var record map[string]interface{}
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &record))
		assert.NotNil(t, record["detail"])
		lineCount++
	}
	require.NoError(t, scanner.Err())
	assert.Equal(t, 3, lineCount)
	assert.Equal(t, "false", recorder.Header().Get("X-Export-Truncated"))
	assert.Equal(t, "10000", recorder.Header().Get("X-Export-Record-Limit"))

	// 管理页的“会话数”只限制会话集合，不能退化为原始日志行数限制。
	recorder = featDoReq(t, router, http.MethodGet, "/admin/api/logs/export?key_id="+jsonNumber(key.ID)+"&full_only=true&session_limit=1", nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	scanner = bufio.NewScanner(strings.NewReader(recorder.Body.String()))
	lineCount = 0
	for scanner.Scan() {
		var record map[string]interface{}
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &record))
		detail := record["detail"].(map[string]interface{})
		assert.Equal(t, "session:123", detail["session_id"])
		lineCount++
	}
	require.NoError(t, scanner.Err())
	assert.Equal(t, 2, lineCount, "一个会话内的所有近期请求都应导出")
}

func jsonNumber(value int64) string {
	return strconv.FormatInt(value, 10)
}
