package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const orcaAgentsBodyLimit = 64 << 10

var orcaAgentsErrorKeys = map[store.OrcaAgentsErrorCode]string{
	store.OrcaAgentsErrTooMany:         i18n.MsgOrcaAgentsTooMany,
	store.OrcaAgentsErrInvalidField:    i18n.MsgOrcaAgentInvalidField,
	store.OrcaAgentsErrDuplicateKey:    i18n.MsgOrcaAgentDuplicateKey,
	store.OrcaAgentsErrInvalidTier:     i18n.MsgOrcaAgentInvalidTier,
	store.OrcaAgentsErrFallback:        i18n.MsgOrcaAgentFallbackRule,
	store.OrcaAgentsErrPlanningLimit:   i18n.MsgOrcaAgentPlanningLimit,
	store.OrcaAgentsErrDescriptionLong: i18n.MsgSecureCLIDescriptionTooLong,
}

func (h *OrcaServersHandler) handleUpdateAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	id, ok := h.parseID(w, r)
	if !ok {
		return
	}
	var req struct {
		Agents []store.OrcaAgentConfig `json:"agents"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, orcaAgentsBodyLimit)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error()))
		return
	}
	if req.Agents == nil {
		req.Agents = []store.OrcaAgentConfig{}
	}
	if err := store.ValidateOrcaAgents(req.Agents); err != nil {
		writeOrcaAgentsError(w, locale, err)
		return
	}
	if err := h.servers.UpdateAgents(ctx, id, req.Agents); err != nil {
		var ve *store.OrcaAgentsError
		if errors.As(err, &ve) {
			writeOrcaAgentsError(w, locale, err)
			return
		}
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

func writeOrcaAgentsError(w http.ResponseWriter, locale string, err error) {
	var ve *store.OrcaAgentsError
	if !errors.As(err, &ve) {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error()))
		return
	}
	writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, orcaAgentsErrorKeys[ve.Code], ve.Args...))
}
