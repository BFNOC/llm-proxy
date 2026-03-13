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
	"time"

	"github.com/Instawork/llm-proxy/internal/middleware"
	"github.com/Instawork/llm-proxy/internal/proxy"
	"github.com/Instawork/llm-proxy/internal/store"
	"github.com/gorilla/mux"
)

type AdminHandler struct {
	store       *store.Store
	keyCache    *middleware.KeyCache
	rateLimiter *middleware.PerKeyRPMLimiter
	prober      *proxy.UpstreamProber
	auditLogger *middleware.AuditLogger
	adminToken  string
}

func NewAdminHandler(
	s *store.Store,
	kc *middleware.KeyCache,
	rl *middleware.PerKeyRPMLimiter,
	prober *proxy.UpstreamProber,
	al *middleware.AuditLogger,
	adminToken string,
) *AdminHandler {
	return &AdminHandler{
		store:       s,
		keyCache:    kc,
		rateLimiter: rl,
		prober:      prober,
		auditLogger: al,
		adminToken:  adminToken,
	}
}

// RegisterRoutes registers admin API routes on the given subrouter.
func (h *AdminHandler) RegisterRoutes(r *mux.Router) {
	// All admin routes require admin auth
	api := r.PathPrefix("/admin/api").Subrouter()
	api.Use(h.authMiddleware)

	// Upstreams
	api.HandleFunc("/upstreams", h.listUpstreams).Methods("GET")
	api.HandleFunc("/upstreams", h.createUpstream).Methods("POST")
	api.HandleFunc("/upstreams/{id}", h.updateUpstream).Methods("PUT")
	api.HandleFunc("/upstreams/{id}", h.deleteUpstream).Methods("DELETE")

	// Keys
	api.HandleFunc("/keys", h.listKeys).Methods("GET")
	api.HandleFunc("/keys", h.createKey).Methods("POST")
	api.HandleFunc("/keys/{id}", h.updateKey).Methods("PUT")
	api.HandleFunc("/keys/{id}", h.deleteKey).Methods("DELETE")

	// Logs
	api.HandleFunc("/logs", h.queryLogs).Methods("GET")

	// Status
	api.HandleFunc("/status", h.getStatus).Methods("GET")

	// Dashboard (serve embedded HTML)
	r.PathPrefix("/admin/").HandlerFunc(h.serveDashboard)
}

func (h *AdminHandler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if !strings.HasPrefix(token, "Bearer ") || strings.TrimPrefix(token, "Bearer ") != h.adminToken {
			jsonError(w, http.StatusUnauthorized, "invalid admin token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Upstreams ---

func (h *AdminHandler) listUpstreams(w http.ResponseWriter, r *http.Request) {
	upstreams, err := h.store.ListUpstreams()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Redact API keys in listing
	type upstreamResponse struct {
		ID        int64     `json:"id"`
		Name      string    `json:"name"`
		BaseURL   string    `json:"base_url"`
		Priority  int       `json:"priority"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	result := make([]upstreamResponse, len(upstreams))
	for i, u := range upstreams {
		result[i] = upstreamResponse{
			ID: u.ID, Name: u.Name, BaseURL: u.BaseURL,
			Priority: u.Priority, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
		}
	}
	jsonOK(w, result)
}

func (h *AdminHandler) createUpstream(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		BaseURL  string `json:"base_url"`
		APIKey   string `json:"api_key"`
		Priority int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" || req.BaseURL == "" || req.APIKey == "" {
		jsonError(w, http.StatusBadRequest, "name, base_url, and api_key are required")
		return
	}

	// SSRF validation
	if err := validateBaseURL(req.BaseURL); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	upstream, err := h.store.CreateUpstream(req.Name, req.BaseURL, req.APIKey, req.Priority)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	slog.Info("admin: created upstream", "id", upstream.ID, "name", upstream.Name)
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, map[string]interface{}{"id": upstream.ID, "name": upstream.Name, "base_url": upstream.BaseURL, "priority": upstream.Priority})
}

func (h *AdminHandler) updateUpstream(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req struct {
		Name     string `json:"name"`
		BaseURL  string `json:"base_url"`
		APIKey   string `json:"api_key"`
		Priority int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.BaseURL != "" {
		if err := validateBaseURL(req.BaseURL); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	upstream, err := h.store.UpdateUpstream(id, req.Name, req.BaseURL, req.APIKey, req.Priority)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	slog.Info("admin: updated upstream", "id", upstream.ID)
	jsonOK(w, map[string]interface{}{"id": upstream.ID, "name": upstream.Name})
}

func (h *AdminHandler) deleteUpstream(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.DeleteUpstream(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	slog.Info("admin: deleted upstream", "id", id)
	jsonOK(w, map[string]string{"status": "deleted"})
}

// --- Keys ---

func (h *AdminHandler) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.store.ListKeys()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type keyResponse struct {
		ID        int64     `json:"id"`
		KeyPrefix string    `json:"key_prefix"`
		Name      string    `json:"name"`
		RPMLimit  int       `json:"rpm_limit"`
		Enabled   bool      `json:"enabled"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	result := make([]keyResponse, len(keys))
	for i, k := range keys {
		result[i] = keyResponse{
			ID: k.ID, KeyPrefix: k.KeyPrefix, Name: k.Name,
			RPMLimit: k.RPMLimit, Enabled: k.Enabled,
			CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt,
		}
	}
	jsonOK(w, result)
}

func (h *AdminHandler) createKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		RPMLimit int    `json:"rpm_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" {
		jsonError(w, http.StatusBadRequest, "name is required")
		return
	}

	plaintext, key, err := h.store.CreateKey(req.Name, req.RPMLimit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Reload key cache
	if err := h.keyCache.Reload(h.store); err != nil {
		slog.Error("admin: failed to reload key cache", "error", err)
	}

	slog.Info("admin: created key", "id", key.ID, "name", key.Name)
	w.WriteHeader(http.StatusCreated)
	// Return plaintext ONCE
	jsonOK(w, map[string]interface{}{
		"id":        key.ID,
		"key":       plaintext,
		"name":      key.Name,
		"rpm_limit": key.RPMLimit,
	})
}

func (h *AdminHandler) updateKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req struct {
		Name     string `json:"name"`
		RPMLimit int    `json:"rpm_limit"`
		Enabled  *bool  `json:"enabled"` // pointer to distinguish absent from false
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	key, err := h.store.UpdateKey(id, req.Name, req.RPMLimit, enabled)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Reload key cache
	if err := h.keyCache.Reload(h.store); err != nil {
		slog.Error("admin: failed to reload key cache", "error", err)
	}

	slog.Info("admin: updated key", "id", key.ID)
	jsonOK(w, map[string]interface{}{"id": key.ID, "name": key.Name, "rpm_limit": key.RPMLimit, "enabled": key.Enabled})
}

func (h *AdminHandler) deleteKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.DeleteKey(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Reload key cache + clean rate limiter
	if err := h.keyCache.Reload(h.store); err != nil {
		slog.Error("admin: failed to reload key cache", "error", err)
	}
	h.rateLimiter.RemoveKey(id)

	slog.Info("admin: deleted key", "id", id)
	jsonOK(w, map[string]string{"status": "deleted"})
}

// --- Logs ---

func (h *AdminHandler) queryLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var keyID int64
	if v := q.Get("key_id"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid key_id")
			return
		}
		keyID = parsed
	}

	from := time.Now().UTC().Add(-24 * time.Hour)
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid from date (use RFC3339)")
			return
		}
		from = t
	}

	to := time.Now().UTC()
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid to date (use RFC3339)")
			return
		}
		to = t
	}

	limit := 100
	if v := q.Get("limit"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}

	logs, err := h.store.QueryLogs(keyID, from, to, limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonOK(w, logs)
}

// --- Status ---

func (h *AdminHandler) getStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"active_upstream_id": h.prober.GetCurrentID(),
		"audit_dropped":      h.auditLogger.DroppedCount(),
		"timestamp":          time.Now().UTC(),
	}
	jsonOK(w, status)
}

// --- Dashboard ---

func (h *AdminHandler) serveDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(dashboardHTML)
}

// --- Helpers ---

func parseID(r *http.Request) (int64, error) {
	vars := mux.Vars(r)
	return strconv.ParseInt(vars["id"], 10, 64)
}

func jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// validateBaseURL enforces https and rejects private/loopback/link-local IPs.
func validateBaseURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("base_url must use https scheme")
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
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("base_url resolves to private/loopback IP %s", ipStr)
		}
	}

	return nil
}
