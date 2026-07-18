package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFullRecordingConfig_DefaultSetAndKeyCascade(t *testing.T) {
	s := newTestStore(t)
	config, err := s.GetFullRecordingConfig()
	require.NoError(t, err)
	assert.False(t, config.Enabled)
	assert.True(t, config.AllKeys)
	assert.Empty(t, config.DownstreamKeyIDs)

	_, keyA, err := s.CreateKey("record-a", 0)
	require.NoError(t, err)
	_, keyB, err := s.CreateKey("record-b", 0)
	require.NoError(t, err)
	require.NoError(t, s.SetFullRecordingConfig(FullRecordingConfig{
		Enabled:          true,
		AllKeys:          false,
		DownstreamKeyIDs: []int64{keyB.ID, keyA.ID, keyB.ID},
	}))

	config, err = s.GetFullRecordingConfig()
	require.NoError(t, err)
	assert.True(t, config.Enabled)
	assert.False(t, config.AllKeys)
	assert.Equal(t, []int64{keyA.ID, keyB.ID}, config.DownstreamKeyIDs)

	require.NoError(t, s.DeleteKey(keyA.ID))
	config, err = s.GetFullRecordingConfig()
	require.NoError(t, err)
	assert.False(t, config.AllKeys)
	assert.Equal(t, []int64{keyB.ID}, config.DownstreamKeyIDs)

	require.NoError(t, s.DeleteKey(keyB.ID))
	config, err = s.GetFullRecordingConfig()
	require.NoError(t, err)
	assert.False(t, config.AllKeys)
	assert.Empty(t, config.DownstreamKeyIDs)
}

func TestRequestLogDetail_SessionChainAndQueries(t *testing.T) {
	s := newTestStore(t)
	_, key, err := s.CreateKey("session-key", 0)
	require.NoError(t, err)
	now := time.Now().UTC()

	logs := []RequestLog{
		{
			DownstreamKeyID: key.ID,
			ProviderStyle:   "responses",
			Path:            "/v1/responses",
			StatusCode:      200,
			CreatedAt:       now,
			Detail: &RequestLogDetail{
				SessionID:      "derived:root",
				SessionSource:  "derived:message_root",
				SessionPreview: "first question",
				ResponseID:     "resp_1",
				Method:         "POST",
				RequestBody:    `{"input":"first question"}`,
				ResponseBody:   `{"id":"resp_1"}`,
				CaptureStatus:  "captured",
			},
		},
		{
			DownstreamKeyID: key.ID,
			ProviderStyle:   "responses",
			Path:            "/v1/responses",
			StatusCode:      500,
			CreatedAt:       now.Add(time.Second),
			Detail: &RequestLogDetail{
				ResponseID:       "resp_2",
				ParentResponseID: "resp_1",
				Method:           "POST",
				CaptureStatus:    "captured",
			},
		},
		{
			DownstreamKeyID: key.ID,
			ProviderStyle:   "responses",
			Path:            "/v1/responses",
			StatusCode:      200,
			CreatedAt:       now.Add(2 * time.Second),
			Detail: &RequestLogDetail{
				ResponseID:       "resp_3",
				ParentResponseID: "resp_2",
				Method:           "POST",
				CaptureStatus:    "captured",
			},
		},
	}
	require.NoError(t, s.InsertRequestLogBatch(logs))

	filtered, err := s.QueryLogsFiltered(LogQuery{
		KeyID:     key.ID,
		SessionID: "derived:root",
		From:      now.Add(-time.Minute),
		To:        now.Add(time.Minute),
	})
	require.NoError(t, err)
	require.Len(t, filtered, 3)
	for _, logEntry := range filtered {
		assert.True(t, logEntry.HasFullRecord)
		detail, detailErr := s.GetRequestLogDetail(logEntry.ID)
		require.NoError(t, detailErr)
		assert.Equal(t, "derived:root", detail.SessionID)
		assert.Equal(t, "derived:message_root", detail.SessionSource)
	}

	sessions, err := s.QueryLogSessions(LogQuery{
		KeyID: key.ID,
		From:  now.Add(-time.Minute),
		To:    now.Add(time.Minute),
		Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "derived:root", sessions[0].SessionID)
	assert.Equal(t, "first question", sessions[0].SessionPreview)
	assert.Equal(t, 3, sessions[0].RequestCount)
	assert.Equal(t, 1, sessions[0].ErrorCount)

	logIDs := make([]int64, 0, len(filtered))
	for _, logEntry := range filtered {
		logIDs = append(logIDs, logEntry.ID)
	}
	details, err := s.GetRequestLogDetails(logIDs)
	require.NoError(t, err)
	require.Len(t, details, 3)
	for _, logID := range logIDs {
		assert.Equal(t, "derived:root", details[logID].SessionID)
	}
}

func TestQueryLogsFiltered_SessionKeysUseKeyAndSessionPair(t *testing.T) {
	s := newTestStore(t)
	_, keyA, err := s.CreateKey("session-filter-a", 0)
	require.NoError(t, err)
	_, keyB, err := s.CreateKey("session-filter-b", 0)
	require.NoError(t, err)
	now := time.Now().UTC()

	require.NoError(t, s.InsertRequestLogBatch([]RequestLog{
		{DownstreamKeyID: keyA.ID, CreatedAt: now, Detail: &RequestLogDetail{SessionID: "shared-session", CaptureStatus: "captured"}},
		{DownstreamKeyID: keyB.ID, CreatedAt: now.Add(time.Second), Detail: &RequestLogDetail{SessionID: "shared-session", CaptureStatus: "captured"}},
		{DownstreamKeyID: keyB.ID, CreatedAt: now.Add(2 * time.Second), Detail: &RequestLogDetail{SessionID: "selected-session", CaptureStatus: "captured"}},
	}))

	logs, err := s.QueryLogsFiltered(LogQuery{
		From: now.Add(-time.Minute),
		To:   now.Add(time.Minute),
		SessionKeys: []LogSessionKey{
			{DownstreamKeyID: keyA.ID, SessionID: "shared-session"},
			{DownstreamKeyID: keyB.ID, SessionID: "selected-session"},
		},
	})
	require.NoError(t, err)
	require.Len(t, logs, 2)
	assert.Equal(t, keyB.ID, logs[0].DownstreamKeyID)
	assert.Equal(t, keyA.ID, logs[1].DownstreamKeyID)
}

func TestRequestLogDetail_ParentResponseChainStaysWithinDownstreamKey(t *testing.T) {
	s := newTestStore(t)
	_, keyA, err := s.CreateKey("session-key-a", 0)
	require.NoError(t, err)
	_, keyB, err := s.CreateKey("session-key-b", 0)
	require.NoError(t, err)
	now := time.Now().UTC()

	require.NoError(t, s.InsertRequestLogBatch([]RequestLog{
		{
			DownstreamKeyID: keyA.ID,
			CreatedAt:       now,
			Detail: &RequestLogDetail{
				SessionID:     "key-a-session",
				SessionSource: "header:session-id",
				ResponseID:    "shared-response-id",
			},
		},
		{
			DownstreamKeyID: keyB.ID,
			CreatedAt:       now.Add(time.Second),
			Detail: &RequestLogDetail{
				ParentResponseID: "shared-response-id",
				ResponseID:       "key-b-response",
			},
		},
	}))

	logs, err := s.QueryLogs(keyB.ID, now.Add(-time.Minute), now.Add(time.Minute), 10)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	detail, err := s.GetRequestLogDetail(logs[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "shared-response-id", detail.SessionID)
	assert.Equal(t, "previous_response_id", detail.SessionSource)

	require.NoError(t, s.InsertRequestLogBatch([]RequestLog{{
		DownstreamKeyID: keyA.ID,
		CreatedAt:       now.Add(2 * time.Second),
		Detail: &RequestLogDetail{
			ParentResponseID: "shared-response-id",
			ResponseID:       "key-a-child",
		},
	}}))
	logs, err = s.QueryLogs(keyA.ID, now.Add(-time.Minute), now.Add(time.Minute), 10)
	require.NoError(t, err)
	require.Len(t, logs, 2)
	detail, err = s.GetRequestLogDetail(logs[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "key-a-session", detail.SessionID, "历史父响应应在写事务前批量解析")
	assert.Equal(t, "header:session-id", detail.SessionSource)
}

func TestRequestLogDetail_OutOfOrderBatchResolvesParentSession(t *testing.T) {
	s := newTestStore(t)
	_, key, err := s.CreateKey("out-of-order-session", 0)
	require.NoError(t, err)
	now := time.Now().UTC()

	require.NoError(t, s.InsertRequestLogBatch([]RequestLog{
		{
			DownstreamKeyID: key.ID,
			CreatedAt:       now.Add(time.Second),
			Detail: &RequestLogDetail{
				ParentResponseID: "resp-parent",
				ResponseID:       "resp-child",
			},
		},
		{
			DownstreamKeyID: key.ID,
			CreatedAt:       now,
			Detail: &RequestLogDetail{
				SessionID:     "conversation-1",
				SessionSource: "body:conversation",
				ResponseID:    "resp-parent",
			},
		},
	}))

	logs, err := s.QueryLogs(key.ID, now.Add(-time.Minute), now.Add(time.Minute), 10)
	require.NoError(t, err)
	require.Len(t, logs, 2)
	for _, logEntry := range logs {
		detail, detailErr := s.GetRequestLogDetail(logEntry.ID)
		require.NoError(t, detailErr)
		assert.Equal(t, "conversation-1", detail.SessionID)
		assert.Equal(t, "body:conversation", detail.SessionSource)
	}
}

func TestQueryLogSessions_UsesLatestSourceAndPreview(t *testing.T) {
	s := newTestStore(t)
	_, key, err := s.CreateKey("latest-session-preview", 0)
	require.NoError(t, err)
	now := time.Now().UTC()
	require.NoError(t, s.InsertRequestLogBatch([]RequestLog{
		{
			DownstreamKeyID: key.ID,
			CreatedAt:       now,
			Detail: &RequestLogDetail{
				SessionID:      "session-latest",
				SessionSource:  "z-old-source",
				SessionPreview: "z-old-preview",
			},
		},
		{
			DownstreamKeyID: key.ID,
			CreatedAt:       now.Add(time.Second),
			Detail: &RequestLogDetail{
				SessionID:      "session-latest",
				SessionSource:  "a-new-source",
				SessionPreview: "a-new-preview",
			},
		},
	}))

	sessions, err := s.QueryLogSessions(LogQuery{From: now.Add(-time.Minute), To: now.Add(time.Minute), Limit: 10})
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "a-new-source", sessions[0].SessionSource)
	assert.Equal(t, "a-new-preview", sessions[0].SessionPreview)
}

func TestRequestLogDetail_CascadesWithLogCleanup(t *testing.T) {
	s := newTestStore(t)
	_, key, err := s.CreateKey("cleanup-detail", 0)
	require.NoError(t, err)
	require.NoError(t, s.InsertRequestLogBatch([]RequestLog{{
		DownstreamKeyID: key.ID,
		ProviderStyle:   "openai",
		Path:            "/v1/chat/completions",
		StatusCode:      200,
		CreatedAt:       time.Now().UTC().Add(-48 * time.Hour),
		Detail:          &RequestLogDetail{SessionID: "session-cleanup", CaptureStatus: "captured"},
	}}))
	logs, err := s.QueryLogs(key.ID, time.Now().Add(-72*time.Hour), time.Now(), 1)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	logID := logs[0].ID

	require.NoError(t, s.DeleteLogsOlderThan(24*time.Hour))
	_, err = s.GetRequestLogDetail(logID)
	assert.Error(t, err)
}
