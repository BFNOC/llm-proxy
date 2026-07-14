package middleware

import (
	"net/http"
	"strings"
)

// StreamingMiddleware 确保流式响应被正确处理。
// 它通过检查已知的流式端点来判断是否为流式请求，并用一个可 flush 的
// writer 包装 ResponseWriter。
func StreamingMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isStreamingEndpoint(r) {
				if flusher, ok := w.(http.Flusher); ok {
					sw := &streamingResponseWriter{
						ResponseWriter: w,
						flusher:        flusher,
					}
					next.ServeHTTP(sw, r)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isStreamingEndpoint 对已知支持流式响应的端点返回 true。
func isStreamingEndpoint(r *http.Request) bool {
	path := r.URL.Path
	return strings.HasSuffix(path, "/chat/completions") ||
		strings.HasSuffix(path, "/messages") ||
		strings.HasSuffix(path, "/completions") ||
		strings.HasSuffix(path, "/responses")
}

// streamingResponseWriter 包装 http.ResponseWriter 以确保正确 flush。
// 它实现了 http.Flusher，使下游代码（DynamicProxy）能够穿过这层中间件
// 对 Flusher 做类型断言。
type streamingResponseWriter struct {
	http.ResponseWriter
	flusher http.Flusher
}

func (sw *streamingResponseWriter) Write(b []byte) (int, error) {
	n, err := sw.ResponseWriter.Write(b)
	sw.flusher.Flush()
	return n, err
}

// Flush 实现 http.Flusher。
func (sw *streamingResponseWriter) Flush() {
	sw.flusher.Flush()
}
