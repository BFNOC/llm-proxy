package middleware

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"
)

type sessionMetadata struct {
	ID               string
	Source           string
	Preview          string
	ResponseID       string
	ParentResponseID string
}

// extractSessionMetadata 将不同协议和客户端的会话字段规范化。
// 明确标识优先；只有无标识且请求包含可识别首轮消息时才生成稳定指纹。
func extractSessionMetadata(header http.Header, requestBody, responseBody []byte, keyID int64, providerStyle, path string) sessionMetadata {
	metadata := sessionMetadata{ResponseID: extractResponseID(responseBody)}
	var body map[string]interface{}
	var firstUser string
	if json.Unmarshal(requestBody, &body) == nil {
		metadata.ParentResponseID = stringValue(body["previous_response_id"])
		firstUser = firstUserText(body)
		metadata.Preview = truncatePreview(firstUser)
	}

	candidates := []struct {
		value  string
		source string
	}{
		{header.Get("Session-Id"), "header:session-id"},
		{header.Get("X-Claude-Code-Session-Id"), "header:x-claude-code-session-id"},
		{header.Get("X-Session-Id"), "header:x-session-id"},
		{header.Get("Conversation-Id"), "header:conversation-id"},
		{header.Get("X-Conversation-Id"), "header:x-conversation-id"},
		{nestedString(body, "session_id"), "body:session_id"},
		{nestedString(body, "conversation_id"), "body:conversation_id"},
		{conversationID(body), "body:conversation"},
		{nestedString(body, "client_metadata", "session_id"), "body:client_metadata.session_id"},
		{nestedString(body, "client_metadata", "conversation_id"), "body:client_metadata.conversation_id"},
		{nestedString(body, "metadata", "session_id"), "body:metadata.session_id"},
		{metadataUserSessionID(body), "body:metadata.user_id.session_id"},
		{nestedString(body, "prompt_cache_key"), "body:prompt_cache_key"},
		{header.Get("Thread-Id"), "header:thread-id"},
		{nestedString(body, "thread_id"), "body:thread_id"},
		{nestedString(body, "client_metadata", "thread_id"), "body:client_metadata.thread_id"},
	}
	for _, candidate := range candidates {
		if value := strings.TrimSpace(candidate.value); value != "" {
			metadata.ID = value
			metadata.Source = candidate.source
			return metadata
		}
	}

	if metadata.Preview == "" {
		return metadata
	}
	root := map[string]interface{}{
		"downstream_key_id": keyID,
		"provider_style":    providerStyle,
		"path":              path,
		"system":            body["system"],
		"first_user":        firstUser,
	}
	encoded, err := json.Marshal(root)
	if err != nil {
		return metadata
	}
	sum := sha256.Sum256(encoded)
	metadata.ID = "derived:" + hex.EncodeToString(sum[:12])
	metadata.Source = "derived:message_root"
	return metadata
}

func conversationID(body map[string]interface{}) string {
	value, ok := body["conversation"]
	if !ok {
		return ""
	}
	if id := stringValue(value); id != "" {
		return id
	}
	if object, ok := value.(map[string]interface{}); ok {
		return stringValue(object["id"])
	}
	return ""
}

func metadataUserSessionID(body map[string]interface{}) string {
	value := nestedValue(body, "metadata", "user_id")
	if object, ok := value.(map[string]interface{}); ok {
		return stringValue(object["session_id"])
	}
	raw := stringValue(value)
	if raw == "" {
		return ""
	}
	var object map[string]interface{}
	if json.Unmarshal([]byte(raw), &object) != nil {
		return ""
	}
	return stringValue(object["session_id"])
}

func nestedString(root map[string]interface{}, path ...string) string {
	return stringValue(nestedValue(root, path...))
}

func nestedValue(root map[string]interface{}, path ...string) interface{} {
	var current interface{} = root
	for _, key := range path {
		object, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = object[key]
	}
	return current
}

func stringValue(value interface{}) string {
	text, _ := value.(string)
	return text
}

func firstUserText(body map[string]interface{}) string {
	if input, ok := body["input"].(string); ok {
		return input
	}
	for _, field := range []string{"messages", "input"} {
		items, ok := body[field].([]interface{})
		if !ok {
			continue
		}
		for _, item := range items {
			message, ok := item.(map[string]interface{})
			if !ok || stringValue(message["role"]) != "user" {
				continue
			}
			if text := contentText(message["content"]); text != "" {
				return text
			}
		}
	}
	return ""
}

func contentText(content interface{}) string {
	if text, ok := content.(string); ok {
		return text
	}
	blocks, ok := content.([]interface{})
	if !ok {
		return ""
	}
	var parts []string
	for _, block := range blocks {
		object, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		for _, field := range []string{"text", "input_text", "output_text"} {
			if text := strings.TrimSpace(stringValue(object[field])); text != "" {
				parts = append(parts, text)
				break
			}
		}
	}
	return strings.Join(parts, " ")
}

func truncatePreview(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) <= 240 {
		return value
	}
	runes := []rune(value)
	return string(runes[:240])
}

func extractResponseID(body []byte) string {
	if id := responseIDFromJSON(body); id != "" {
		return id
	}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	// 响应正文已受 32 MiB 捕获上限约束，扫描器需允许同等大小的单行 SSE 事件。
	scanner.Buffer(make([]byte, 64*1024), maxFullRecordBodyBytes+64*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		if id := responseIDFromJSON([]byte(payload)); id != "" {
			return id
		}
	}
	return ""
}

func responseIDFromJSON(data []byte) string {
	var object map[string]interface{}
	if json.Unmarshal(data, &object) != nil {
		return ""
	}
	if id := stringValue(object["id"]); id != "" {
		return id
	}
	for _, field := range []string{"response", "message"} {
		if id := nestedString(object, field, "id"); id != "" {
			return id
		}
	}
	return ""
}
