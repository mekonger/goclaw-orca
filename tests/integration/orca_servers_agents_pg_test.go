//go:build integration

package integration

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestOrcaServers_AgentsAndHealth(t *testing.T) {
	f, s := newOrcaFixture(t)
	ctx := f.ctx()
	other := store.WithTenantID(ctx, uuid.New())
	srv := createOrca(t, s, f, "agents-vm")

	got, err := s.Get(ctx, srv.ID)
	mustNoErr(t, err, "Get")
	if got.Agents == nil || len(got.Agents) != 0 || len(got.AgentHealth) != 0 {
		t.Fatalf("new server agents=%v health=%v", got.Agents, got.AgentHealth)
	}

	agents := []store.OrcaAgentConfig{
		{Key: "codex-fast", Agent: "codex", Model: "gpt-5", Tier: store.OrcaAgentTierRoutine, Enabled: true, Fallback: true},
		{Key: "claude-hard", Agent: "claude", Model: "sonnet", Tier: store.OrcaAgentTierHard, Enabled: true},
	}
	mustNoErr(t, s.UpdateAgents(ctx, srv.ID, agents), "UpdateAgents")
	if err := s.UpdateAgents(other, srv.ID, nil); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("cross-tenant UpdateAgents: %v", err)
	}

	h := store.OrcaAgentHealth{Status: store.OrcaAgentStatusHealthy, CheckedAt: time.Now().UTC()}
	mustNoErr(t, s.UpdateAgentHealth(ctx, srv.ID, agents[0], h), "health codex")
	mustNoErr(t, s.UpdateAgentHealth(ctx, srv.ID, agents[1], h), "health claude")
	if err := s.UpdateAgentHealth(ctx, srv.ID, store.OrcaAgentConfig{Key: "ghost", Agent: "codex"}, h); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("ghost key: %v", err)
	}
	if err := s.UpdateAgentHealth(other, srv.ID, agents[0], h); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("cross-tenant health: %v", err)
	}
	got, err = s.Get(ctx, srv.ID)
	mustNoErr(t, err, "Get after health")
	if len(got.Agents) != 2 || got.Agents[0] != agents[0] || len(got.AgentHealth) != 2 {
		t.Fatalf("health write changed agents or missed: agents=%v health=%v", got.Agents, got.AgentHealth)
	}

	stale := agents[1]
	agents[1].Model = "opus"
	mustNoErr(t, s.UpdateAgents(ctx, srv.ID, agents), "UpdateAgents model change")
	if err := s.UpdateAgentHealth(ctx, srv.ID, stale, h); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("stale model health write: %v", err)
	}
	got, _ = s.Get(ctx, srv.ID)
	if _, ok := got.AgentHealth["claude-hard"]; ok || len(got.AgentHealth) != 1 {
		t.Fatalf("model change kept health: %v", got.AgentHealth)
	}

	mustNoErr(t, s.UpdateAgents(ctx, srv.ID, []store.OrcaAgentConfig{}), "UpdateAgents empty")
	got, _ = s.Get(ctx, srv.ID)
	if len(got.Agents) != 0 || len(got.AgentHealth) != 0 {
		t.Fatalf("clear: agents=%v health=%v", got.Agents, got.AgentHealth)
	}
}
