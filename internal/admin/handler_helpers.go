package admin

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/gorilla/mux"
)

// --- 辅助函数 ---

func parseID(r *http.Request) (int64, error) {
	vars := mux.Vars(r)
	return strconv.ParseInt(vars["id"], 10, 64)
}

func parseAPIKeyRowID(r *http.Request) (int64, error) {
	keyID, err := strconv.ParseInt(mux.Vars(r)["key_id"], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid key_id")
	}
	return keyID, nil
}

func cleanAPIKeys(keys []string) []string {
	seen := make(map[string]bool, len(keys))
	cleaned := make([]string, 0, len(keys))
	for _, key := range keys {
		for _, value := range normalizeAPIKeyValues(key) {
			if seen[value] {
				continue
			}
			seen[value] = true
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}

func normalizeAPIKeyValues(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ','
	})
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			values = append(values, field)
		}
	}
	return values
}

func jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// tryRemoveTransport 仅当没有其他上游仍在使用同一 proxyURL 时，
// 才从缓存中移除对应 transport 并关闭空闲连接。
// excludeID 是正在删除或修改的上游 ID，在判断"是否还有其他"时排除它。
func (h *AdminHandler) tryRemoveTransport(proxyURL string, excludeID int64) {
	upstreams, err := h.store.ListUpstreams()
	if err != nil {
		slog.Warn("admin: failed to list upstreams for transport cleanup", "error", err)
		return
	}
	for _, u := range upstreams {
		if u.ID != excludeID && u.ProxyURL == proxyURL {
			// 还有其他上游在用同一代理，保留 transport
			return
		}
	}
	proxy.RemoveTransport(proxyURL)
}

func jsonError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// applyCFHeaders 在传出请求上注入 Cloudflare 绕过所需的 Cookie 和 User-Agent。
// 仅当 clearance 非空时才设置，避免覆盖默认行为。
func applyCFHeaders(req *http.Request, clearance, userAgent string) {
	if clearance != "" {
		req.AddCookie(&http.Cookie{Name: "cf_clearance", Value: clearance})
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
}

// validateBaseURL 强制要求 http/https，并拒绝 private/loopback/link-local IP。
func validateBaseURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("base_url must use http or https scheme")
	}

	host := parsed.Hostname()
	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("cannot resolve host %s: %w", host, err)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("base_url resolves to private/loopback IP %s", ipStr)
		}
	}

	return nil
}

// validateProxyURL 校验代理地址格式，仅允许 http/https/socks5 协议，且必须包含主机名。
func validateProxyURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid proxy_url: %w", err)
	}
	switch parsed.Scheme {
	case "http", "https", "socks5":
		// 合法
	default:
		return fmt.Errorf("proxy_url must use http, https, or socks5 scheme")
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("proxy_url must include a hostname")
	}
	return nil
}

// sanitizeProxyForLog 抹除 proxy URL 中的用户凭据，防止密码写入日志。
func sanitizeProxyForLog(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "<invalid>"
	}
	parsed.User = nil
	return parsed.String()
}
