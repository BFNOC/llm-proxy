package middleware

import (
	"net/http"
	"strings"
)

// StreamingMiddleware ensures proper handling of streaming responses.
// It detects streaming by checking known streaming endpoints and wraps the
// ResponseWriter with a flushing writer.
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

// isStreamingEndpoint returns true for endpoints known to support streaming.
func isStreamingEndpoint(r *http.Request) bool {
	path := r.URL.Path
	return strings.HasSuffix(path, "/chat/completions") ||
		strings.HasSuffix(path, "/messages") ||
		strings.HasSuffix(path, "/completions")
}

// streamingResponseWriter wraps http.ResponseWriter to ensure proper flushing.
type streamingResponseWriter struct {
	http.ResponseWriter
	flusher http.Flusher
}

func (sw *streamingResponseWriter) Write(b []byte) (int, error) {
	n, err := sw.ResponseWriter.Write(b)
	sw.flusher.Flush()
	return n, err
}
