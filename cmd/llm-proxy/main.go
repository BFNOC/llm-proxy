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
	"strings"
	"syscall"
	"time"

	"github.com/Instawork/llm-proxy/internal/admin"
	"github.com/Instawork/llm-proxy/internal/config"
	"github.com/Instawork/llm-proxy/internal/middleware"
	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/gorilla/mux"
)

// CustomPrettyHandler implements a custom slog.Handler for pretty local output.
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

// cst is the China Standard Time timezone (UTC+8).
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
	version     = "2.0.0"
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

	// Override from env vars
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		yamlConfig.Logging.Level = v
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		yamlConfig.Logging.Format = v
	}
	if v := os.Getenv("PORT"); v != "" {
		// Override port from env
		fmt.Sscanf(v, "%d", &yamlConfig.Server.Port)
	}

	// Re-initialize logger with final config values (YAML + env overrides).
	os.Setenv("LOG_LEVEL", yamlConfig.Logging.Level)
	os.Setenv("LOG_FORMAT", yamlConfig.Logging.Format)
	initLogger()

	yamlConfig.LogConfiguration(slog.Default())

	// Validate required env vars
	encryptionKeyHex := os.Getenv("ENCRYPTION_KEY")
	if encryptionKeyHex == "" {
		slog.Error("ENCRYPTION_KEY environment variable is required (32 bytes, hex or raw)")
		os.Exit(1)
	}

	// Support both raw 32-byte string and hex-encoded
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

	// Ensure data directory exists
	dataDir := yamlConfig.Storage.SQLitePath
	if idx := strings.LastIndex(dataDir, "/"); idx > 0 {
		if err := os.MkdirAll(dataDir[:idx], 0o755); err != nil {
			slog.Error("Failed to create data directory", "error", err)
			os.Exit(1)
		}
	}

	// Open SQLite store
	db, err := store.NewStore(yamlConfig.Storage.SQLitePath, encryptionKey)
	if err != nil {
		slog.Error("Failed to open SQLite store", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("SQLite store opened", "path", yamlConfig.Storage.SQLitePath)

	// Create key cache and load snapshot
	keyCache := middleware.NewKeyCache()
	if err := keyCache.Reload(db); err != nil {
		slog.Error("Failed to load key cache", "error", err)
		os.Exit(1)
	}
	slog.Info("Key cache loaded")

	// Create per-key RPM limiter
	rateLimiter := middleware.NewPerKeyRPMLimiter()

	// Create dynamic proxy
	dynamicProxy := proxy.NewDynamicProxy()

	// Create upstream prober and start background goroutine
	probeInterval := time.Duration(yamlConfig.Upstream.ProbeIntervalSeconds) * time.Second
	probeTimeout := time.Duration(yamlConfig.Upstream.ProbeTimeoutSeconds) * time.Second
	prober := proxy.NewUpstreamProber(db, dynamicProxy, probeInterval, probeTimeout)

	proberCtx, proberCancel := context.WithCancel(context.Background())
	go prober.Start(proberCtx)
	slog.Info("Upstream prober started", "interval", probeInterval)

	// Create audit logger
	var auditLogger *middleware.AuditLogger
	if yamlConfig.Audit.Enabled {
		flushInterval := time.Duration(yamlConfig.Audit.FlushInterval) * time.Millisecond
		auditLogger = middleware.NewAuditLogger(db, yamlConfig.Audit.ChannelBuffer, yamlConfig.Audit.BatchSize, flushInterval)
		slog.Info("Audit logger started", "buffer", yamlConfig.Audit.ChannelBuffer, "batch_size", yamlConfig.Audit.BatchSize)
	}

	// Build router
	r := mux.NewRouter()

	// Health endpoints (no auth)
	r.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	}).Methods("GET", "HEAD")

	r.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		status := map[string]interface{}{
			"status":            "ok",
			"active_upstream":   dynamicProxy.GetActiveUpstream() != nil,
			"audit_dropped":     int64(0),
		}
		if auditLogger != nil {
			status["audit_dropped"] = auditLogger.DroppedCount()
		}
		if dynamicProxy.GetActiveUpstream() == nil {
			status["status"] = "not_ready"
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(status) //nolint:errcheck
	}).Methods("GET", "HEAD")

	// Admin routes (separate subrouter, no CORS)
	if yamlConfig.Admin.Enabled {
		adminHandler := admin.NewAdminHandler(db, keyCache, rateLimiter, prober, auditLogger, adminToken)
		adminHandler.RegisterRoutes(r)
		slog.Info("Admin interface enabled", "dashboard", "/admin/", "api", "/admin/api/")
	}

	// Proxy middleware chain for /v1/
	proxyChain := http.Handler(dynamicProxy)
	proxyChain = middleware.StreamingMiddleware()(proxyChain)
	if auditLogger != nil {
		proxyChain = middleware.AuditLogMiddleware(auditLogger)(proxyChain)
	}
	proxyChain = middleware.RateLimitMiddleware(rateLimiter)(proxyChain)
	proxyChain = middleware.KeyResolverMiddleware(keyCache)(proxyChain)
	proxyChain = middleware.RequestClassifierMiddleware()(proxyChain)
	proxyChain = middleware.CORSMiddleware()(proxyChain)

	r.PathPrefix("/v1/").Handler(proxyChain)

	// Root endpoint — return API info instead of 404.
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

	// Start server
	bindAddr := "127.0.0.1"
	if v := os.Getenv("BIND_ADDR"); v != "" {
		bindAddr = v
	}
	port := fmt.Sprintf("%d", yamlConfig.Server.Port)
	server := &http.Server{
		Addr:    bindAddr + ":" + port,
		Handler: r,
	}

	go func() {
		slog.Info("Starting server", "address", server.Addr, "version", version)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown
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

	// Stop prober
	proberCancel()
	slog.Info("Upstream prober stopped")

	// Drain audit logger
	if auditLogger != nil {
		slog.Info("Draining audit logger...")
		auditLogger.Stop()
		slog.Info("Audit logger drained")
	}

	slog.Info("Server shutdown complete")
}
