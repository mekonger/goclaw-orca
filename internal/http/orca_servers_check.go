package http

import (
	"context"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/orcaserver"
)

func (h *OrcaServersHandler) handleCheck(w http.ResponseWriter, r *http.Request) {
	ctx := context.WithoutCancel(r.Context())
	id, ok := h.parseID(w, r)
	if !ok {
		return
	}
	srv, err := h.servers.GetWithSecret(ctx, id)
	if err != nil {
		h.writeStoreError(w, r, err, id.String())
		return
	}
	if err := orcaserver.CheckAndStore(ctx, h.servers, h.checker, srv, true); err != nil {
		h.writeStoreError(w, r, err, id.String())
		return
	}
	srv.PairingCode = ""
	srv.HasPairingCode = true
	writeJSON(w, http.StatusOK, srv)
}
