package orcaserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	UnhealthyResetAfter = 30 * time.Minute
	notSignedInError    = "not signed in"
)

type agentAuth int

const (
	authUnknown agentAuth = iota
	authSignedIn
	authSignedOut
)

type accountEntry struct {
	Accounts      *[]json.RawMessage `json:"accounts"`
	SystemDefault *struct {
		HasAuth *bool `json:"hasAuth"`
	} `json:"systemDefault"`
}

func parseAccountList(raw []byte) (map[string]agentAuth, bool) {
	var out struct {
		OK     *bool                      `json:"ok"`
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.OK == nil || !*out.OK || out.Result == nil {
		return nil, false
	}
	auth := map[string]agentAuth{}
	for agent, v := range out.Result {
		var e accountEntry
		if json.Unmarshal(v, &e) != nil || e.Accounts == nil {
			continue
		}
		switch {
		case len(*e.Accounts) > 0:
			auth[agent] = authSignedIn
		case e.SystemDefault != nil && e.SystemDefault.HasAuth != nil && *e.SystemDefault.HasAuth:
			auth[agent] = authSignedIn
		case e.SystemDefault != nil && e.SystemDefault.HasAuth != nil:
			auth[agent] = authSignedOut
		default:
			auth[agent] = authUnknown
		}
	}
	return auth, true
}

func (c *Checker) accountAuth(ctx context.Context, code string) (map[string]agentAuth, bool) {
	res, err := c.run(ctx, code, "account", "list", "--json")
	if err != nil || res.timedOut {
		return nil, false
	}
	return parseAccountList(res.stdout)
}

func nextAgentHealth(cur store.OrcaAgentHealth, a store.OrcaAgentConfig, serverHealthy, reset bool,
	auth map[string]agentAuth, now time.Time) (store.OrcaAgentHealth, bool) {

	status := cur.Status
	if status == "" {
		status = store.OrcaAgentStatusUnknown
	}
	next := cur
	next.Status = status
	if !serverHealthy {
		next = store.OrcaAgentHealth{Status: store.OrcaAgentStatusUnknown, CheckedAt: now, LastOKAt: cur.LastOKAt}
		return next, status != store.OrcaAgentStatusUnknown
	}
	if status == store.OrcaAgentStatusUnhealthy && (reset || now.Sub(cur.CheckedAt) >= UnhealthyResetAfter) {
		next = store.OrcaAgentHealth{Status: store.OrcaAgentStatusUnknown, CheckedAt: now, LastOKAt: cur.LastOKAt}
	}
	if a.Enabled {
		switch auth[a.Agent] {
		case authSignedIn:
			if next.Status == store.OrcaAgentStatusUnhealthy && cur.Error != notSignedInError {
				return next, next.Status != status
			}
			ok := now
			next = store.OrcaAgentHealth{Status: store.OrcaAgentStatusHealthy, CheckedAt: now, LastOKAt: &ok}
			return next, true
		case authSignedOut:
			next = store.OrcaAgentHealth{Status: store.OrcaAgentStatusUnhealthy, Error: notSignedInError, CheckedAt: now, LastOKAt: cur.LastOKAt}
			return next, status != store.OrcaAgentStatusUnhealthy || cur.Error != notSignedInError
		}
	}
	return next, next.Status != status
}

func CheckAgents(ctx context.Context, s store.OrcaServerStore, c *Checker, srv *store.OrcaServer, serverHealthy, reset bool) {
	if len(srv.Agents) == 0 {
		return
	}
	ctx = store.WithTenantID(ctx, srv.TenantID)
	var auth map[string]agentAuth
	if serverHealthy {
		if a, ok := c.accountAuth(ctx, srv.PairingCode); ok {
			auth = a
		} else {
			slog.Warn("orca_server.account_list_failed", "id", srv.ID, "label", srv.Label)
		}
	}
	now := time.Now().UTC()
	if srv.AgentHealth == nil {
		srv.AgentHealth = map[string]store.OrcaAgentHealth{}
	}
	for _, a := range srv.Agents {
		next, changed := nextAgentHealth(srv.AgentHealth[a.Key], a, serverHealthy, reset, auth, now)
		if !changed {
			continue
		}
		if err := s.UpdateAgentHealth(ctx, srv.ID, a, next); err != nil {
			slog.Warn("orca_server.agent_health_write_failed", "id", srv.ID, "key", a.Key, "error", err)
			continue
		}
		srv.AgentHealth[a.Key] = next
	}
}
