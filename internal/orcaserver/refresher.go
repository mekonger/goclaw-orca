package orcaserver

import (
	"context"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	RefreshInterval = 5 * time.Minute
	initialDelay    = 30 * time.Second
	dueBatchSize    = 50
)

type Refresher struct {
	store   store.OrcaServerStore
	checker *Checker
}

func NewRefresher(s store.OrcaServerStore, c *Checker) *Refresher {
	return &Refresher{store: s, checker: c}
}

func (r *Refresher) Start(ctx context.Context) {
	go func() {
		select {
		case <-time.After(initialDelay):
		case <-ctx.Done():
			return
		}
		r.RunOnce(ctx)
		ticker := time.NewTicker(RefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.RunOnce(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (r *Refresher) RunOnce(ctx context.Context) {
	due, err := r.store.ListDueForCheck(ctx, time.Now().Add(-RefreshInterval), dueBatchSize)
	if err != nil {
		slog.Warn("orca_server.refresh_list_failed", "error", err)
		return
	}
	for i := range due {
		if ctx.Err() != nil {
			return
		}
		srv := &due[i]
		if err := CheckAndStore(ctx, r.store, r.checker, srv, false); err != nil {
			slog.Warn("orca_server.refresh_update_failed", "id", srv.ID, "error", err)
		}
	}
}

func CheckAndStore(ctx context.Context, s store.OrcaServerStore, c *Checker, srv *store.OrcaServer, resetAgents bool) error {
	start := time.Now()
	h := c.Check(ctx, srv.PairingCode)
	slog.Info("orca_server.check", "id", srv.ID, "tenant_id", srv.TenantID, "label", srv.Label,
		"status", h.Status, "duration", time.Since(start))
	srv.LastStatus = h.Status
	srv.LastRuntimeState = h.RuntimeState
	srv.LastError = h.Error
	checked := h.CheckedAt
	srv.LastCheckedAt = &checked
	if h.Healthy {
		srv.LastHealthyAt = &checked
	}
	if err := s.UpdateHealth(ctx, srv.ID, h); err != nil {
		return err
	}
	CheckAgents(ctx, s, c, srv, h.Healthy, resetAgents)
	return nil
}
