package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Instawork/llm-proxy/internal/store"
)

// AuditLogMiddleware 通过 AuditLogger 异步记录请求元数据，并按运行时策略附带完整记录。
func AuditLogMiddleware(logger *AuditLogger, policies ...*FullRecordingPolicy) func(http.Handler) http.Handler {
	var policy *FullRecordingPolicy
	if len(policies) > 0 {
		policy = policies[0]
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			keyID := DownstreamKeyIDFromContext(r.Context())
			captureFull := policy.ShouldRecord(keyID)
			isWebSocket := isWebSocketUpgrade(r)

			var requestCapture *bodyCaptureReadCloser
			requestHeadersJSON := "{}"
			if captureFull {
				requestHeadersJSON = marshalHeaders(sanitizeRequestHeaders(r.Header))
				if !isWebSocket && r.Body != nil && r.Body != http.NoBody {
					requestCapture = &bodyCaptureReadCloser{
						ReadCloser: r.Body,
						capture:    &limitedBodyCapture{limit: maxFullRecordBodyBytes, budget: logger.fullRecordMem},
					}
					r.Body = requestCapture
				}
			}

			// 请求体大小：优先使用 ContentLength，未知时记为 0。
			requestSize := r.ContentLength
			if requestSize < 0 {
				requestSize = 0
			}

			capture := &responseStatusCapture{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				captureFull:    captureFull && !isWebSocket,
				responseBody:   limitedBodyCapture{limit: maxFullRecordBodyBytes, budget: logger.fullRecordMem},
			}
			defer func() {
				if requestCapture != nil {
					requestCapture.capture.releaseReserved()
				}
				capture.responseBody.releaseReserved()
			}()
			next.ServeHTTP(capture, r)
			if !capture.wroteHeader && !capture.hijacked {
				capture.WriteHeader(http.StatusOK)
			}

			latency := time.Since(start).Milliseconds()

			style := StyleFromContext(r.Context())
			upstreamName := capture.upstreamName

			// 提取客户端 IP，优先级：CF-Connecting-IP > X-Real-IP > X-Forwarded-For > RemoteAddr。
			clientIP := r.Header.Get("CF-Connecting-IP")
			if clientIP == "" {
				clientIP = r.Header.Get("X-Real-IP")
			}
			if clientIP == "" {
				clientIP = r.Header.Get("X-Forwarded-For")
				if clientIP != "" {
					// X-Forwarded-For 可能包含多个 IP，取第一个。
					if idx := strings.Index(clientIP, ","); idx != -1 {
						clientIP = strings.TrimSpace(clientIP[:idx])
					}
				}
			}
			if clientIP == "" {
				clientIP = r.RemoteAddr
				// 从 RemoteAddr 中去除端口号（如 "1.2.3.4:12345" -> "1.2.3.4"）。
				if host, _, err := net.SplitHostPort(clientIP); err == nil {
					clientIP = host
				}
			}

			logEntry := store.RequestLog{
				DownstreamKeyID: keyID,
				UpstreamName:    upstreamName,
				UpstreamKeyIdx:  capture.upstreamKeyIdx,
				Model:           capture.model,
				UsedProxy:       capture.usedProxy,
				ClientIP:        clientIP,
				ProviderStyle:   string(style),
				Path:            r.URL.Path,
				StatusCode:      capture.statusCode,
				LatencyMs:       latency,
				RequestSize:     requestSize,
				ResponseSize:    capture.responseSize,
				CreatedAt:       time.Now().UTC(),
			}
			if captureFull {
				logEntry.Detail, logEntry.RetainedDetailBytes = buildRequestLogDetail(r, requestCapture, requestHeadersJSON, capture, isWebSocket, keyID, string(style))
			}
			logger.Log(logEntry)
		})
	}
}

func buildRequestLogDetail(r *http.Request, requestCapture *bodyCaptureReadCloser, requestHeadersJSON string, response *responseStatusCapture, isWebSocket bool, keyID int64, providerStyle string) (*store.RequestLogDetail, int64) {
	detail := &store.RequestLogDetail{
		Method:              r.Method,
		RawQuery:            sanitizeRawQuery(r.URL.RawQuery),
		RequestHeadersJSON:  requestHeadersJSON,
		ResponseHeadersJSON: marshalHeaders(response.responseHeader),
		CaptureStatus:       "captured",
	}
	if isWebSocket {
		detail.CaptureStatus = "websocket_handshake"
		session := extractSessionMetadata(r.Header, nil, nil, keyID, providerStyle, r.URL.Path)
		detail.SessionID = session.ID
		detail.SessionSource = session.Source
		return detail, 0
	}
	var retainedBytes int64
	if requestCapture != nil {
		detail.RequestBody, retainedBytes = requestCapture.capture.takeString()
		detail.RequestBodyTruncated = requestCapture.capture.truncated
		if !requestCapture.read && r.ContentLength != 0 {
			detail.CaptureStatus = "request_body_unread"
		}
	}
	var responseBytes int64
	detail.ResponseBody, responseBytes = response.responseBody.takeString()
	retainedBytes += responseBytes
	detail.ResponseBodyTruncated = response.responseBody.truncated
	session := extractSessionMetadata(r.Header, []byte(detail.RequestBody), []byte(detail.ResponseBody), keyID, providerStyle, r.URL.Path)
	detail.SessionID = session.ID
	detail.SessionSource = session.Source
	detail.SessionPreview = session.Preview
	detail.ResponseID = session.ResponseID
	detail.ParentResponseID = session.ParentResponseID
	if detail.RequestBodyTruncated || detail.ResponseBodyTruncated {
		detail.CaptureStatus = "truncated"
	}
	return detail, retainedBytes
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && headerContainsToken(r.Header, "Connection", "upgrade")
}

func headerContainsToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func sanitizeRequestHeaders(header http.Header) http.Header {
	return redactSensitiveHeaders(header)
}

func sanitizeResponseHeaders(header http.Header) http.Header {
	return redactSensitiveHeaders(header)
}

func redactSensitiveHeaders(header http.Header) http.Header {
	result := make(http.Header)
	for name, values := range header {
		canonicalName := http.CanonicalHeaderKey(name)
		if isSensitiveName(canonicalName) {
			result[canonicalName] = []string{"[REDACTED]"}
		} else {
			result[canonicalName] = append([]string(nil), values...)
		}
	}
	return result
}

func sanitizeRawQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "[REDACTED_INVALID_QUERY]"
	}
	for name := range values {
		if isSensitiveName(name) {
			values[name] = []string{"[REDACTED]"}
		}
	}
	return values.Encode()
}

func isSensitiveName(name string) bool {
	lowerName := strings.ToLower(strings.TrimSpace(name))
	compactName := strings.NewReplacer("-", "", "_", "", ".", "").Replace(lowerName)
	if lowerName == "cookie" || lowerName == "set-cookie" || lowerName == "auth" || compactName == "xauth" || strings.Contains(compactName, "authorization") {
		return true
	}
	for _, marker := range []string{"apikey", "accesstoken", "authtoken", "refreshtoken", "securitytoken", "clientsecret", "password", "passwd", "credential", "signature"} {
		if strings.Contains(compactName, marker) {
			return true
		}
	}
	return lowerName == "token" || lowerName == "secret" || lowerName == "key"
}

func marshalHeaders(header http.Header) string {
	if len(header) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(header)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}
