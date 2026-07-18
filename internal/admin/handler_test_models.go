package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/Instawork/llm-proxy/internal/store"
)

// --- 测试模型 ---

func (h *AdminHandler) listTestModels(w http.ResponseWriter, r *http.Request) {
	protocol := r.URL.Query().Get("protocol")
	models, err := h.store.ListTestModels(protocol)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if models == nil {
		models = []store.TestModel{}
	}
	jsonOK(w, models)
}

func (h *AdminHandler) createTestModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" {
		jsonError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Protocol == "" {
		req.Protocol = "openai"
	}
	m, err := h.store.CreateTestModel(req.Name, req.Protocol)
	if err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, m)
}

func (h *AdminHandler) updateTestModel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" {
		jsonError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := h.store.UpdateTestModel(id, req.Name, req.Protocol); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	jsonOK(w, map[string]interface{}{"id": id, "name": req.Name, "protocol": req.Protocol})
}

func (h *AdminHandler) deleteTestModel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.DeleteTestModel(id); err != nil {
		slog.Error("admin: store error", "error", err)
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	jsonOK(w, map[string]string{"status": "deleted"})
}
