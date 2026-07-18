package admin

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/Instawork/llm-proxy/internal/middleware"
	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/gorilla/mux"
)

var startTime = time.Now()

type AdminHandler struct {
	store          *store.Store
	keyCache       *middleware.KeyCache
	rateLimiter    *middleware.PerKeyRPMLimiter
	prober         *proxy.UpstreamProber
	dynamicProxy   *proxy.DynamicProxy
	auditLogger    *middleware.AuditLogger
	fullRecording  *middleware.FullRecordingPolicy
	modelFilter    *middleware.ModelFilter
	requestCounter *middleware.GlobalRequestCounter
	perKeyStats    *middleware.PerKeyStatsCollector
	overrideCache  *middleware.ModelOverrideCache
	bindingCache   *middleware.BindingCache
	headerCapture  *middleware.HeaderCapture
	circuitBreaker *middleware.CircuitBreaker
	adminToken     string
	version        string
}

func NewAdminHandler(
	s *store.Store,
	kc *middleware.KeyCache,
	rl *middleware.PerKeyRPMLimiter,
	prober *proxy.UpstreamProber,
	dp *proxy.DynamicProxy,
	al *middleware.AuditLogger,
	mf *middleware.ModelFilter,
	rc *middleware.GlobalRequestCounter,
	pks *middleware.PerKeyStatsCollector,
	oc *middleware.ModelOverrideCache,
	bc *middleware.BindingCache,
	hc *middleware.HeaderCapture,
	cb *middleware.CircuitBreaker,
	adminToken string,
	version string,
	fullRecordingPolicies ...*middleware.FullRecordingPolicy,
) *AdminHandler {
	var fullRecording *middleware.FullRecordingPolicy
	if len(fullRecordingPolicies) > 0 {
		fullRecording = fullRecordingPolicies[0]
	}
	if fullRecording == nil {
		config, err := s.GetFullRecordingConfig()
		if err != nil {
			config = store.FullRecordingConfig{}
		}
		fullRecording = middleware.NewFullRecordingPolicy(config)
	}
	return &AdminHandler{
		store:          s,
		keyCache:       kc,
		rateLimiter:    rl,
		prober:         prober,
		dynamicProxy:   dp,
		auditLogger:    al,
		fullRecording:  fullRecording,
		modelFilter:    mf,
		requestCounter: rc,
		perKeyStats:    pks,
		overrideCache:  oc,
		bindingCache:   bc,
		headerCapture:  hc,
		circuitBreaker: cb,
		adminToken:     adminToken,
		version:        version,
	}
}

// RegisterRoutes 在给定的子路由器上注册 admin API 路由。
func (h *AdminHandler) RegisterRoutes(r *mux.Router) {
	// 所有 admin 路由都要求 admin 鉴权
	api := r.PathPrefix("/admin/api").Subrouter()
	api.Use(h.authMiddleware)

	// 上游（批量路由放在 /{id} 之前，避免路径冲突）
	api.HandleFunc("/upstreams", h.listUpstreams).Methods("GET")
	api.HandleFunc("/upstreams", h.createUpstream).Methods("POST")
	api.HandleFunc("/upstreams/batch/enabled", h.batchSetUpstreamEnabled).Methods("PUT")
	api.HandleFunc("/upstreams/batch", h.batchDeleteUpstreams).Methods("DELETE")
	api.HandleFunc("/upstreams/models", h.getAllUpstreamModelPatterns).Methods("GET")
	api.HandleFunc("/upstreams/declared-models", h.getAllUpstreamDeclaredModels).Methods("GET")
	api.HandleFunc("/upstreams/reorder", h.reorderUpstreams).Methods("PUT")
	api.HandleFunc("/upstreams/{id}", h.updateUpstream).Methods("PUT")
	api.HandleFunc("/upstreams/{id}", h.deleteUpstream).Methods("DELETE")
	api.HandleFunc("/upstreams/{id}/auto-discover", h.setAutoDiscoverModels).Methods("PUT")
	api.HandleFunc("/upstreams/{id}/test-proxy", h.testUpstreamProxy).Methods("POST")
	api.HandleFunc("/upstreams/{id}/test-websocket", h.testUpstreamWebSocket).Methods("POST")
	api.HandleFunc("/upstreams/{id}/check-quota", h.checkUpstreamQuota).Methods("POST")
	api.HandleFunc("/upstreams/{id}/models", h.getUpstreamModelPatterns).Methods("GET")
	api.HandleFunc("/upstreams/{id}/models", h.setUpstreamModelPatterns).Methods("PUT")
	api.HandleFunc("/upstreams/{id}/declared-models", h.getUpstreamDeclaredModels).Methods("GET")
	api.HandleFunc("/upstreams/{id}/declared-models", h.setUpstreamDeclaredModels).Methods("PUT")
	// 按上游管理各自的 API Key
	api.HandleFunc("/upstreams/{id}/apikeys", h.listUpstreamAPIKeys).Methods("GET")
	api.HandleFunc("/upstreams/{id}/apikeys", h.addUpstreamAPIKeys).Methods("POST")
	api.HandleFunc("/upstreams/{id}/apikeys/{key_id}", h.deleteUpstreamAPIKey).Methods("DELETE")
	api.HandleFunc("/upstreams/{id}/apikeys/{key_id}/enabled", h.setAPIKeyEnabled).Methods("PUT")
	api.HandleFunc("/upstreams/{id}/apikeys/{key_id}/test", h.testUpstreamAPIKey).Methods("POST")

	// 上游模板
	api.HandleFunc("/upstream-templates", h.listUpstreamTemplates).Methods("GET")
	// 上游速率信息与熔断状态
	api.HandleFunc("/upstreams/rate-info", h.getUpstreamRateInfo).Methods("GET")
	api.HandleFunc("/upstreams/circuit-status", h.getCircuitStatus).Methods("GET")
	api.HandleFunc("/upstreams/deleted", h.listDeletedUpstreams).Methods("GET")
	api.HandleFunc("/upstreams/{id}/rpm-limit", h.setUpstreamRPMLimit).Methods("PUT")
	api.HandleFunc("/upstreams/{id}/circuit-breaker", h.setCircuitBreakerConfig).Methods("PUT")
	api.HandleFunc("/upstreams/{id}/undo", h.undoDeleteUpstream).Methods("POST")

	// Key
	api.HandleFunc("/keys", h.listKeys).Methods("GET")
	api.HandleFunc("/keys", h.createKey).Methods("POST")
	api.HandleFunc("/keys/{id}", h.updateKey).Methods("PUT")
	api.HandleFunc("/keys/{id}", h.deleteKey).Methods("DELETE")
	api.HandleFunc("/keys/{id}/reveal", h.revealKey).Methods("GET")

	// 日志
	api.HandleFunc("/logs", h.queryLogs).Methods("GET")
	api.HandleFunc("/logs/key-stats", h.getKeyUsageStats).Methods("GET")
	api.HandleFunc("/logs/export", h.exportLogs).Methods("GET")
	api.HandleFunc("/logs/sessions", h.queryLogSessions).Methods("GET")
	api.HandleFunc("/logs/session", h.getLogSession).Methods("GET")
	api.HandleFunc("/logs/{id}", h.getLogDetail).Methods("GET")
	api.HandleFunc("/logs/{id}/replay", h.replayRequest).Methods("POST")

	// 模型白名单
	api.HandleFunc("/models/whitelist", h.listModelWhitelist).Methods("GET")
	api.HandleFunc("/models/whitelist", h.addModelWhitelist).Methods("POST")
	api.HandleFunc("/models/whitelist/batch", h.batchDeleteModelWhitelist).Methods("DELETE")
	api.HandleFunc("/models/whitelist/{id}", h.deleteModelWhitelist).Methods("DELETE")

	// 绑定接口拆成“全量查看”“单 Key 查询”“全量覆盖更新”三类，
	// 让管理端既能一次加载总览，也能按 Key 精确编辑。
	api.HandleFunc("/keys/bindings", h.getAllKeyBindings).Methods("GET")
	api.HandleFunc("/keys/{id}/upstreams", h.getKeyUpstreams).Methods("GET")
	api.HandleFunc("/keys/{id}/upstreams", h.setKeyUpstreams).Methods("PUT")

	// Key 模型路由覆盖
	api.HandleFunc("/keys/model-overrides", h.getAllKeyModelOverrides).Methods("GET")
	api.HandleFunc("/keys/{id}/model-overrides", h.getKeyModelOverrides).Methods("GET")
	api.HandleFunc("/keys/{id}/model-overrides", h.setKeyModelOverrides).Methods("PUT")

	// 状态
	api.HandleFunc("/status", h.getStatus).Methods("GET")
	api.HandleFunc("/key-rpm", h.getKeyRPM).Methods("GET")

	// 测试模型
	api.HandleFunc("/test-models", h.listTestModels).Methods("GET")
	api.HandleFunc("/test-models", h.createTestModel).Methods("POST")
	api.HandleFunc("/test-models/{id}", h.updateTestModel).Methods("PUT")
	api.HandleFunc("/test-models/{id}", h.deleteTestModel).Methods("DELETE")

	api.HandleFunc("/settings", h.getSettings).Methods("GET")
	api.HandleFunc("/settings", h.updateSettings).Methods("PUT")

	// 延迟统计与健康历史
	api.HandleFunc("/stats/latency", h.getLatencyStats).Methods("GET")
	api.HandleFunc("/upstreams/{id}/health-history", h.getHealthHistory).Methods("GET")

	// 快捷操作
	api.HandleFunc("/actions/pause-all", h.pauseAllUpstreams).Methods("POST")
	api.HandleFunc("/actions/resume-all", h.resumeAllUpstreams).Methods("POST")
	api.HandleFunc("/actions/refresh-caches", h.refreshAllCaches).Methods("POST")

	// SSE 实时事件推送
	api.HandleFunc("/events", h.sseEvents).Methods("GET")

	// 配置导入导出
	api.HandleFunc("/config/export", h.exportConfig).Methods("GET")
	api.HandleFunc("/config/import", h.importConfig).Methods("POST")

	// Header 抓取（Claude Code / 客户端指纹调试）
	api.HandleFunc("/header-capture", h.getHeaderCapture).Methods("GET")
	api.HandleFunc("/header-capture", h.updateHeaderCapture).Methods("PUT")
	api.HandleFunc("/header-capture", h.clearHeaderCapture).Methods("DELETE")

	// Dashboard 壳层 + 静态 CSS/JS（embed）。静态资源必须在兜底路由之前注册。
	r.PathPrefix("/admin/assets/").Handler(assetsHandler())
	r.PathPrefix("/admin/").HandlerFunc(h.serveDashboard)
}

func (h *AdminHandler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if strings.HasPrefix(token, "Bearer ") {
			if subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(token, "Bearer ")), []byte(h.adminToken)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}
		// 仅 SSE 端点允许 ?token= 查询参数（EventSource 无法设置自定义 Header），
		// 避免其他端点在 URL 中暴露 admin token（日志、浏览器历史、Referer 等）。
		if strings.HasSuffix(r.URL.Path, "/events") {
			if qToken := r.URL.Query().Get("token"); qToken != "" {
				if subtle.ConstantTimeCompare([]byte(qToken), []byte(h.adminToken)) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}
		}
		jsonError(w, http.StatusUnauthorized, "invalid admin token")
	})
}

// --- Dashboard 页面 ---

func (h *AdminHandler) serveDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(dashboardHTML)
}
