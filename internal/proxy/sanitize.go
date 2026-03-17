// Package proxy — sanitize.go 对上游错误响应体做纯文本脱敏。
//
// 当前覆盖范围（白名单式扩展点）：
//   - 方括号包裹的 API key 标识，如 [sk-xxx]、[ak-xxx]
//   - (request id: xxx) 样式的请求追踪 ID
//   - RemainQuota / UsedQuota / TotalQuota 等额度数字
//
// 未来遇到新的上游敏感字段，只需在 sanitizeRules 中追加条目即可。
// 注意：本模块是纯文本正则替换，不解析 JSON 结构。
package proxy

import (
	"regexp"
)

// sanitizeRule 定义一条脱敏规则：正则匹配 + 替换模板。
// 规则在代码中集中维护，未来新增上游敏感字段只需追加条目。
type sanitizeRule struct {
	pattern *regexp.Regexp
	repl    string
}

// sanitizeRules 是应用于非 2xx 错误响应体的脱敏规则流水线。
// 按顺序依次执行，每条规则独立匹配替换。
var sanitizeRules = []sanitizeRule{
	// 1. 令牌标识：匹配已知 API key 前缀（sk/ak/rk/fk）被方括号包裹的形式
	{regexp.MustCompile(`\[(?:sk|ak|rk|fk)-[^\]]*\]`), "[***]"},

	// 2. 请求 ID：匹配 (request id: xxx)，不区分大小写
	{regexp.MustCompile(`(?i)\(request\s*id\s*:\s*[^)]*\)`), ""},

	// 3. 额度数字：匹配 RemainQuota / UsedQuota / TotalQuota = 数字
	{regexp.MustCompile(`((?:Remain|Used|Total)Quota)(\s*=\s*)-?\d+`), "${1}${2}***"},
}

// maxErrorBodySize 是脱敏时读取错误响应体的最大字节数。
// 超过此限制的部分将被截断。LLM API 错误响应通常 < 5 KB。
const maxErrorBodySize = 256 << 10 // 256 KB

// SanitizeErrorBody 对错误响应体执行所有脱敏规则，
// 依次替换令牌标识、请求 ID、额度数字等上游敏感信息。
func SanitizeErrorBody(body []byte) []byte {
	result := body
	for _, rule := range sanitizeRules {
		result = rule.pattern.ReplaceAll(result, []byte(rule.repl))
	}
	return result
}
