//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/orcaserver"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	orcaRefresherStatusJSON   = `{"ok":true,"result":{"runtime":{"state":"ready"}}}`
	orcaRefresherAccountsJSON = `{"ok":true,"result":{"codex":{"accounts":[],"systemDefault":{"hasAuth":true}}}}`
)

func installFakeOrcaCLI(t *testing.T, statusJSON, accountsJSON string) {
	t.Helper()
	dir := t.TempDir()
	accounts := filepath.Join(dir, "accounts.json")
	if err := os.WriteFile(accounts, []byte(accountsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \"$1 $2\" in\n" +
		"\"status --json\") echo '" + statusJSON + "' ;;\n" +
		"\"account list\") cat " + accounts + " ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "orca"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
}

func TestOrcaServers_RefresherWritesAgentHealthToPG(t *testing.T) {
	f, s := newOrcaFixture(t)
	ctx := f.ctx()
	srv := createOrca(t, s, f, "refresh-vm")

	agents := []store.OrcaAgentConfig{
		{Key: "codex-fast", Agent: "codex", Model: "gpt-5", Tier: store.OrcaAgentTierRoutine, Enabled: true, Fallback: true},
		{Key: "claude-hard", Agent: "claude", Model: "sonnet", Tier: store.OrcaAgentTierHard, Enabled: true},
	}
	mustNoErr(t, s.UpdateAgents(ctx, srv.ID, agents), "UpdateAgents")
	installFakeOrcaCLI(t, orcaRefresherStatusJSON, orcaRefresherAccountsJSON)

	orcaserver.NewRefresher(s, orcaserver.NewChecker()).RunOnce(context.Background())

	got, err := s.Get(ctx, srv.ID)
	mustNoErr(t, err, "Get after refresh")
	if got.LastStatus != store.OrcaServerStatusHealthy {
		t.Fatalf("server last_status = %q, want healthy", got.LastStatus)
	}
	h, ok := got.AgentHealth["codex-fast"]
	if !ok || h.Status != store.OrcaAgentStatusHealthy || h.LastOKAt == nil {
		t.Fatalf("codex-fast health not persisted by refresher: ok=%v health=%+v", ok, h)
	}
	if _, ok := got.AgentHealth["claude-hard"]; ok {
		t.Fatalf("claude without systemDefault must not get health: %+v", got.AgentHealth["claude-hard"])
	}
}
