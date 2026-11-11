package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/orcaserver"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const orcaServerBodyLimit = 16 << 10

type OrcaServersHandler struct {
	servers     store.OrcaServerStore
	tenantStore store.TenantStore
	checker     *orcaserver.Checker
}

func NewOrcaServersHandler(servers store.OrcaServerStore, tenants store.TenantStore, checker *orcaserver.Checker) *OrcaServersHandler {
	return &OrcaServersHandler{servers: servers, tenantStore: tenants, checker: checker}
}

func (h *OrcaServersHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/orca-servers", h.auth(h.handleList))
	mux.HandleFunc("POST /v1/orca-servers", h.auth(h.handleCreate))
	mux.HandleFunc("GET /v1/orca-servers/{id}", h.auth(h.handleGet))
	mux.HandleFunc("PUT /v1/orca-servers/{id}", h.auth(h.handleUpdate))
	mux.HandleFunc("DELETE /v1/orca-servers/{id}", h.auth(h.handleDelete))
	mux.HandleFunc("POST /v1/orca-servers/{id}/check", h.auth(h.handleCheck))
	mux.HandleFunc("PUT /v1/orca-servers/{id}/agents", h.auth(h.handleUpdateAgents))
}

func (h *OrcaServersHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		if !requireTenantAdmin(w, r, h.tenantStore) {
			return
		}
		next(w, r)
	})
}

func (h *OrcaServersHandler) parseID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(store.LocaleFromContext(r.Context()), i18n.MsgInvalidID, "orca server"))
		return uuid.Nil, false
	}
	return id, true
}

func (h *OrcaServersHandler) writeStoreError(w http.ResponseWriter, r *http.Request, err error, id string) {
	locale := store.LocaleFromContext(r.Context())
	switch {
	case errors.Is(err, store.ErrOrcaServerNotFound):
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "orca server", id))
	case errors.Is(err, store.ErrOrcaServerLabelTaken):
		writeError(w, http.StatusConflict, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgAlreadyExists, "orca server", id))
	case errors.Is(err, store.ErrOrcaServerLimit):
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgSecureCLILabelLimit, store.OrcaServerMaxPerTenant))
	default:
		slog.Error("orca_servers: store error", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "orca server"))
	}
}

func (h *OrcaServersHandler) handleList(w http.ResponseWriter, r *http.Request) {
	items, err := h.servers.List(r.Context())
	if err != nil {
		h.writeStoreError(w, r, err, "")
		return
	}
	if items == nil {
		items = []store.OrcaServer{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *OrcaServersHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	id, ok := h.parseID(w, r)
	if !ok {
		return
	}
	srv, err := h.servers.Get(r.Context(), id)
	if err != nil {
		h.writeStoreError(w, r, err, id.String())
		return
	}
	writeJSON(w, http.StatusOK, srv)
}

func (h *OrcaServersHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	var req struct {
		Label       string `json:"label"`
		Description string `json:"description"`
		PairingCode string `json:"pairing_code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, orcaServerBodyLimit)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error()))
		return
	}
	label, err := store.PrepareSecureCLICredentialLabel(req.Label)
	if err != nil || label == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgSecureCLILabelInvalid, truncateRunes(strings.TrimSpace(req.Label), maxReflectedLabelRunes)))
		return
	}
	desc, ok := validateCredentialDescription(w, locale, &req.Description)
	if !ok {
		return
	}
	if err := store.ValidateOrcaPairingCode(req.PairingCode); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error()))
		return
	}
	srv := &store.OrcaServer{Label: label, Description: *desc, PairingCode: req.PairingCode, CreatedBy: store.UserIDFromContext(ctx)}
	if err := h.servers.Create(ctx, srv); err != nil {
		h.writeStoreError(w, r, err, label)
		return
	}
	writeJSON(w, http.StatusCreated, srv)
}

func (h *OrcaServersHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	id, ok := h.parseID(w, r)
	if !ok {
		return
	}
	var req struct {
		Description *string `json:"description"`
		PairingCode *string `json:"pairing_code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, orcaServerBodyLimit)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error()))
		return
	}
	desc, ok := validateCredentialDescription(w, locale, req.Description)
	if !ok {
		return
	}
	if req.PairingCode != nil && strings.TrimSpace(*req.PairingCode) == "" {
		req.PairingCode = nil
	}
	if req.PairingCode != nil {
		if err := store.ValidateOrcaPairingCode(*req.PairingCode); err != nil {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error()))
			return
		}
	}
	if err := h.servers.Update(ctx, id, desc, req.PairingCode); err != nil {
		h.writeStoreError(w, r, err, id.String())
		return
	}
	srv, err := h.servers.Get(ctx, id)
	if err != nil {
		h.writeStoreError(w, r, err, id.String())
		return
	}
	writeJSON(w, http.StatusOK, srv)
}

func (h *OrcaServersHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := h.parseID(w, r)
	if !ok {
		return
	}
	if err := h.servers.Delete(r.Context(), id); err != nil {
		h.writeStoreError(w, r, err, id.String())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
