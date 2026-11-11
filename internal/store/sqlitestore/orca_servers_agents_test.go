//go:build sqlite || sqliteonly

package sqlitestore

import (
	"errors"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestOrcaServerAgents(t *testing.T) {
	s, ctx, other := newOrcaTestStore(t, orcaTestKey)
	srv := &store.OrcaServer{Label: "prod", PairingCode: "orca://h#secret"}
	if err := s.Create(ctx, srv); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, srv.ID)
	if got.Agents == nil || len(got.Agents) != 0 || got.AgentHealth == nil {
		t.Fatalf("new server agents=%v health=%v", got.Agents, got.AgentHealth)
	}
	agents := []store.OrcaAgentConfig{
		{Key: "codex-fast", Agent: "codex", Model: "gpt-5", Tier: store.OrcaAgentTierRoutine, Enabled: true, Fallback: true},
		{Key: "claude-hard", Agent: "claude", Model: "sonnet", Tier: store.OrcaAgentTierHard, Enabled: true},
	}
	if err := s.UpdateAgents(ctx, srv.ID, agents); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgents(other, srv.ID, nil); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("cross-tenant UpdateAgents: %v", err)
	}
	h := store.OrcaAgentHealth{Status: store.OrcaAgentStatusHealthy, CheckedAt: time.Now().UTC()}
	for _, a := range agents {
		if err := s.UpdateAgentHealth(ctx, srv.ID, a, h); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UpdateAgentHealth(ctx, srv.ID, store.OrcaAgentConfig{Key: "ghost", Agent: "codex"}, h); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("ghost key: %v", err)
	}
	if err := s.UpdateAgentHealth(other, srv.ID, agents[0], h); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("cross-tenant health: %v", err)
	}
	got, _ = s.Get(ctx, srv.ID)
	if len(got.Agents) != 2 || got.AgentHealth["codex-fast"].Status != store.OrcaAgentStatusHealthy || len(got.AgentHealth) != 2 {
		t.Fatalf("after health: agents=%v health=%v", got.Agents, got.AgentHealth)
	}
	stale := agents[1]
	agents[1].Model = "opus"
	if err := s.UpdateAgents(ctx, srv.ID, agents[:2]); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentHealth(ctx, srv.ID, stale, h); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("stale model health write: %v", err)
	}
	noModel := store.OrcaAgentConfig{Key: "plain", Agent: "codex", Tier: store.OrcaAgentTierRoutine}
	if err := s.UpdateAgents(ctx, srv.ID, append(agents[:2:2], noModel)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentHealth(ctx, srv.ID, noModel, h); err != nil {
		t.Fatalf("empty-model health write: %v", err)
	}
	got, _ = s.Get(ctx, srv.ID)
	if _, ok := got.AgentHealth["claude-hard"]; ok || len(got.AgentHealth) != 2 {
		t.Fatalf("model change kept health: %v", got.AgentHealth)
	}
	if err := s.UpdateAgents(ctx, srv.ID, []store.OrcaAgentConfig{}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(ctx, srv.ID)
	if len(got.Agents) != 0 || len(got.AgentHealth) != 0 {
		t.Fatalf("clear: agents=%v health=%v", got.Agents, got.AgentHealth)
	}
}

func TestOrcaServerUpgradeFromSchema62KeepsLegacyRows(t *testing.T) {
	s, ctx, _ := newOrcaTestStore(t, orcaTestKey)
	srv := &store.OrcaServer{Label: "legacy", PairingCode: "orca://h#c"}
	if err := s.Create(ctx, srv); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`ALTER TABLE orca_servers DROP COLUMN agents`,
		`ALTER TABLE orca_servers DROP COLUMN agent_health`,
		`UPDATE schema_version SET version = 62`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := EnsureSchema(s.db); err != nil {
		t.Fatalf("upgrade from 62: %v", err)
	}
	got, err := s.Get(ctx, srv.ID)
	if err != nil || got.Agents == nil || len(got.Agents) != 0 || got.AgentHealth == nil || len(got.AgentHealth) != 0 {
		t.Fatalf("legacy row after upgrade: %+v, %v", got, err)
	}
}
