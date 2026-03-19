package middleware

import "net/http"

// StatsMiddleware 在代理链入口处记录请求，用于 RPM/RPS 统计。
// 放在中间件链最外层（CORS 之后），确保所有到达代理的请求都被计数。
func StatsMiddleware(counter *GlobalRequestCounter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			counter.Increment()
			next.ServeHTTP(w, r)
		})
	}
}

// PerKeyStatsMiddleware 在 KeyResolver 之后记录每 Key 请求。
// 须放在 KeyResolverMiddleware 之后，此时 context 中已有 ResolvedKey。
func PerKeyStatsMiddleware(collector *PerKeyStatsCollector) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if resolved := ResolvedKeyFromContext(r.Context()); resolved != nil {
				collector.Record(resolved.ID)
			}
			next.ServeHTTP(w, r)
		})
	}
}
