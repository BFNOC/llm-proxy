package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Instawork/llm-proxy/internal/admin"
	"github.com/Instawork/llm-proxy/internal/config"
	"github.com/Instawork/llm-proxy/internal/geoip"
	"github.com/Instawork/llm-proxy/internal/middleware"
	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/gorilla/mux"
)

// CustomPrettyHandler 实现了一个自定义的 slog.Handler，用于本地美化输出。
type CustomPrettyHandler struct {
	level slog.Level
	w     io.Writer
}

func NewCustomPrettyHandler(w io.Writer, level slog.Level) *CustomPrettyHandler {
	return &CustomPrettyHandler{level: level, w: w}
}

func (h *CustomPrettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// cst 是中国标准时间时区（UTC+8）。
var cst = time.FixedZone("CST", 8*60*60)

func (h *CustomPrettyHandler) Handle(_ context.Context, r slog.Record) error {
	timeStr := r.Time.In(cst).Format("2006-01-02 15:04:05")
	message := r.Message
	var allAttrs []string
	r.Attrs(func(a slog.Attr) bool {
		allAttrs = append(allAttrs, fmt.Sprintf("%s=%v", a.Key, a.Value))
		return true
	})
	if len(allAttrs) > 0 {
		message = fmt.Sprintf("%s; %s", message, strings.Join(allAttrs, ", "))
	}
	_, err := fmt.Fprintf(h.w, "%s [%s] %s\n", r.Level.String(), timeStr, message)
	return err
}

func (h *CustomPrettyHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *CustomPrettyHandler) WithGroup(_ string) slog.Handler      { return h }

const (
	version     = "2.8.0"
	defaultPort = "9002"
)

func initLogger() {
	logLevel := os.Getenv("LOG_LEVEL")
	var level slog.Level
	switch strings.ToLower(logLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logFormat := os.Getenv("LOG_FORMAT")
	var handler slog.Handler
	if logFormat == "json" {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: level,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if a.Key == slog.TimeKey {
					a.Value = slog.StringValue(a.Value.Time().In(cst).Format("2006-01-02 15:04:05"))
				}
				return a
			},
		})
	} else {
		handler = NewCustomPrettyHandler(os.Stderr, level)
	}

	slog.SetDefault(slog.New(handler))
}

func main() {
	initLogger()

	yamlConfig, err := config.LoadEnvironmentConfig()
	if err != nil {
		slog.Warn("Failed to load config, using defaults", "error", err)
		yamlConfig = config.GetDefaultYAMLConfig()
	}

	// 用环境变量覆盖配置
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		yamlConfig.Logging.Level = v
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		yamlConfig.Logging.Format = v
	}
	if v := os.Getenv("PORT"); v != "" {
		// 用环境变量覆盖端口
		fmt.Sscanf(v, "%d", &yamlConfig.Server.Port)
	}

	// 用最终配置值（YAML + 环境变量覆盖）重新初始化日志器。
	os.Setenv("LOG_LEVEL", yamlConfig.Logging.Level)
	os.Setenv("LOG_FORMAT", yamlConfig.Logging.Format)
	initLogger()

	yamlConfig.LogConfiguration(slog.Default())

	// 校验必需的环境变量
	encryptionKeyHex := os.Getenv("ENCRYPTION_KEY")
	if encryptionKeyHex == "" {
		slog.Error("ENCRYPTION_KEY environment variable is required (32 bytes, hex or raw)")
		os.Exit(1)
	}

	// 同时支持 32 字节原始字符串和十六进制编码
	var encryptionKey []byte
	if len(encryptionKeyHex) == 64 {
		encryptionKey, err = hex.DecodeString(encryptionKeyHex)
		if err != nil {
			slog.Error("ENCRYPTION_KEY is 64 chars but not valid hex", "error", err)
			os.Exit(1)
		}
	} else if len(encryptionKeyHex) == 32 {
		encryptionKey = []byte(encryptionKeyHex)
	} else {
		slog.Error("ENCRYPTION_KEY must be exactly 32 bytes (or 64 hex chars)")
		os.Exit(1)
	}

	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" && yamlConfig.Admin.Enabled {
		slog.Error("ADMIN_TOKEN environment variable is required when admin is enabled")
		os.Exit(1)
	}

	// 确保数据目录存在
	dataDir := yamlConfig.Storage.SQLitePath
	if idx := strings.LastIndex(dataDir, "/"); idx > 0 {
		if err := os.MkdirAll(dataDir[:idx], 0o755); err != nil {
			slog.Error("Failed to create data directory", "error", err)
			os.Exit(1)
		}
	}

	// 打开 SQLite store
	db, err := store.NewStore(yamlConfig.Storage.SQLitePath, encryptionKey)
	if err != nil {
		slog.Error("Failed to open SQLite store", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("SQLite store opened", "path", yamlConfig.Storage.SQLitePath)

	// 创建 Key 缓存并加载快照
	keyCache := middleware.NewKeyCache()
	if err := keyCache.Reload(db); err != nil {
		slog.Error("Failed to load key cache", "error", err)
		os.Exit(1)
	}
	slog.Info("Key cache loaded")

	// 创建 per-key RPM 限流器
	rateLimiter := middleware.NewPerKeyRPMLimiter()

	// 创建动态代理
	dynamicProxy := proxy.NewDynamicProxy()

	// 创建模型覆盖缓存（per-key 模型路由覆盖）
	overrideCache := middleware.NewModelOverrideCache(db)

	// 创建上游探活器并启动后台 goroutine
	probeInterval := time.Duration(yamlConfig.Upstream.ProbeIntervalSeconds) * time.Second
	probeTimeout := time.Duration(yamlConfig.Upstream.ProbeTimeoutSeconds) * time.Second

	// 从 DB 加载自动禁用阈值（YAML 为默认值），存入 DynamicProxy 的 atomic 字段支持运行时修改
	thresholdStr, _ := db.GetSetting("auto_disable_threshold", fmt.Sprintf("%d", yamlConfig.Upstream.AutoDisableThreshold))
	if t, err := strconv.Atoi(thresholdStr); err == nil && t >= 0 {
		dynamicProxy.AutoDisableThreshold.Store(int64(t))
	} else {
		dynamicProxy.AutoDisableThreshold.Store(int64(yamlConfig.Upstream.AutoDisableThreshold))
	}

	prober := proxy.NewUpstreamProber(db, dynamicProxy, probeInterval, probeTimeout)

	proberCtx, proberCancel := context.WithCancel(context.Background())
	go prober.Start(proberCtx)
	slog.Info("Upstream prober started", "interval", probeInterval)

	// 后台日志清理：删除早于 log_retention_days（默认 15 天）的 request_logs。
	logCleanupCtx, logCleanupCancel := context.WithCancel(context.Background())
	go func() {
		const cleanupInterval = 6 * time.Hour
		const defaultRetentionDays = 15
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				daysStr, _ := db.GetSetting("log_retention_days", strconv.Itoa(defaultRetentionDays))
				days, err := strconv.Atoi(daysStr)
				if err != nil || days <= 0 {
					days = defaultRetentionDays
				}
				retention := time.Duration(days) * 24 * time.Hour
				if err := db.DeleteLogsOlderThan(retention); err != nil {
					slog.Error("log cleanup failed", "error", err)
				} else {
					slog.Info("log cleanup completed", "retention_days", days)
				}
			case <-logCleanupCtx.Done():
				return
			}
		}
	}()
	slog.Info("Log cleanup goroutine started", "interval", "6h", "default_retention_days", 15)

	// 创建审计日志器
	var auditLogger *middleware.AuditLogger
	if yamlConfig.Audit.Enabled {
		// 初始化 GeoIP 用于 IP 归属地查询（仅审计日志开启时需要）。
		// 优雅降级：若 mmdb 缺失，geo 为 nil，查询将被跳过。
		geoIPDBPath := "data/GeoLite2-City.mmdb"
		if v := os.Getenv("GEOIP_DB_PATH"); v != "" {
			geoIPDBPath = v
		}
		geo := geoip.New(geoIPDBPath)
		if geo != nil {
			defer geo.Close()
		}

		flushInterval := time.Duration(yamlConfig.Audit.FlushInterval) * time.Millisecond
		auditLogger = middleware.NewAuditLogger(db, geo, yamlConfig.Audit.ChannelBuffer, yamlConfig.Audit.BatchSize, flushInterval)
		slog.Info("Audit logger started", "buffer", yamlConfig.Audit.ChannelBuffer, "batch_size", yamlConfig.Audit.BatchSize)
	}

	// 构建路由器
	r := mux.NewRouter()

	// 健康检查端点（无需鉴权）
	r.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	}).Methods("GET", "HEAD")

	// readyz 只回答“当前是否至少存在一个可转发的健康上游”，
	// 让探针语义直接对应真实转发能力，不暴露额外内部状态。
	r.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		status := map[string]interface{}{
			"status": "ok",
		}
		if dynamicProxy.GetActiveUpstream() == nil {
			status["status"] = "not_ready"
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(status) //nolint:errcheck
	}).Methods("GET", "HEAD")

	// Admin 路由（独立子路由，不走 CORS）
	// 模型白名单过滤器
	modelFilter := middleware.NewModelFilter(db)
	// 无条件注入白名单匹配器（即使 admin 被禁用也能生效）
	dynamicProxy.WhitelistMatcher = modelFilter.MatchModel
	// 连续失败 Key 自动禁用回调（达到阈值立即禁用，不等 prober）
	dynamicProxy.KeyFailCallback = func(upstreamID, keyRowID int64) {
		threshold := int(dynamicProxy.AutoDisableThreshold.Load())
		if threshold <= 0 {
			return
		}
		count, err := db.IncrKeyFailures(upstreamID, keyRowID, threshold)
		if err != nil {
			slog.Error("failed to record key failure", "error", err)
			return
		}
		if count >= threshold {
			slog.Warn("key auto-disabled due to consecutive failures",
				"upstream_id", upstreamID, "key_row_id", keyRowID, "failures", count)
		}
	}
	dynamicProxy.KeySuccessCallback = func(upstreamID, keyRowID int64) {
		_ = db.ResetKeyFailures(upstreamID, keyRowID)
	}

	// 创建统计计数器（纯内存，用于 Dashboard 实时统计）
	globalCounter := middleware.NewGlobalRequestCounter()
	perKeyStats := middleware.NewPerKeyStatsCollector()

	// 内存中抓取入站 /v1 客户端 Header（用于 Claude Code 指纹调试）。
	headerCapture := middleware.NewHeaderCapture(20)

	bindingCache := middleware.NewBindingCache(db)

	if yamlConfig.Admin.Enabled {
		adminHandler := admin.NewAdminHandler(db, keyCache, rateLimiter, prober, dynamicProxy, auditLogger, modelFilter, globalCounter, perKeyStats, overrideCache, bindingCache, headerCapture, adminToken, version)
		adminHandler.RegisterRoutes(r)
		slog.Info("Admin interface enabled", "dashboard", "/admin/", "api", "/admin/api/")
	}

	// 代理中间件链顺序不能随意调整：
	// RequestClassifier 先识别 provider 风格和下游 key，
	// KeyResolver 再解析出数据库中的 Key，
	// UpstreamBinding 随后基于 keyID 计算允许访问的上游集合，
	// DynamicProxy 最后依据该集合做真正的上游选择。
	// 这样可以保证未授权上游在任何网络 I/O 发生前就被排除。
	// 中间件装配顺序（后 Wrap 的在更外层）：
	// CORS → HeaderCapture → Stats → Classifier → KeyResolver → Binding →
	// PerKeyStats → Audit → RateLimit → Streaming → ModelFilter → Proxy
	// Audit 必须在 RateLimit 外侧，这样 429 限流也会写入审计日志。
	proxyChain := http.Handler(dynamicProxy)
	proxyChain = middleware.ModelFilterMiddleware(modelFilter)(proxyChain)
	proxyChain = middleware.StreamingMiddleware()(proxyChain)
	proxyChain = middleware.RateLimitMiddleware(rateLimiter)(proxyChain)
	if auditLogger != nil {
		proxyChain = middleware.AuditLogMiddleware(auditLogger)(proxyChain)
	}
	// per-key 统计放在 KeyResolver 之后，确保只记录已通过鉴权的请求
	proxyChain = middleware.PerKeyStatsMiddleware(perKeyStats)(proxyChain)
	// 先做绑定查询再进入后续转发流程，避免存储异常时请求绕过授权边界。
	proxyChain = middleware.UpstreamBindingMiddleware(bindingCache, overrideCache)(proxyChain)
	proxyChain = middleware.KeyResolverMiddleware(keyCache)(proxyChain)
	proxyChain = middleware.RequestClassifierMiddleware()(proxyChain)
	// 全局 RPM/RPS 统计放在最外层，统计所有到达代理的请求
	proxyChain = middleware.StatsMiddleware(globalCounter)(proxyChain)
	// Header 抓取放在鉴权之外，这样即使请求后续鉴权失败，
	// 也能记录原始 Claude Code 客户端 Header（仍然只在开启抓取时生效）。
	proxyChain = headerCapture.Middleware(proxyChain)
	proxyChain = middleware.CORSMiddleware()(proxyChain)

	r.PathPrefix("/v1/").Handler(proxyChain)

	// 根路径端点——返回 API 信息，而不是 404。
	r.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "AI API Server",
			"endpoints": []string{
				"POST /v1/chat/completions",
				"POST /v1/completions",
				"POST /v1/embeddings",
				"GET  /v1/models",
				"POST /v1/messages",
			},
		}) //nolint:errcheck
	})

	// 启动服务器
	bindAddr := "127.0.0.1"
	if v := os.Getenv("BIND_ADDR"); v != "" {
		bindAddr = v
	}
	port := fmt.Sprintf("%d", yamlConfig.Server.Port)
	// 超时设置：与长 SSE 连接及常见 LB/Cloudflare 300s 空闲超时对齐。
	// WriteTimeout 必须保持为 0，避免流式响应在传输中被截断。
	server := &http.Server{
		Addr:              bindAddr + ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       300 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB（1 兆字节）
	}

	go func() {
		slog.Info("Starting server", "address", server.Addr, "version", version)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	// 优雅停机
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	slog.Info("Received shutdown signal", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slog.Info("Shutting down HTTP server...")
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("HTTP server shutdown failed", "error", err)
	} else {
		slog.Info("HTTP server shut down successfully")
	}

	// 停止探活器和日志清理
	proberCancel()
	logCleanupCancel()
	slog.Info("Upstream prober stopped")

	// 排空审计日志器
	if auditLogger != nil {
		slog.Info("Draining audit logger...")
		auditLogger.Stop()
		slog.Info("Audit logger drained")
	}

	slog.Info("Server shutdown complete")
}
