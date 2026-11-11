package http

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestOrcaServersCheckEndpointResetsFreshUnhealthyAgents(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1 $2\" in\n" +
		"\"status --json\") echo '{\"result\":{\"runtime\":{\"state\":\"ready\"}}}' ;;\n" +
		"\"account list\") echo '{\"ok\":true,\"something\":\"else\"}' ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "orca"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")

	mux, ms := newOrcaTestMux(t, oauthAdminTS{})
	id := uuid.MustParse(orcaCreate(t, mux, "prod"))
	agents := []store.OrcaAgentConfig{
		{Key: "codex-fast", Agent: "codex", Model: "gpt-5", Tier: store.OrcaAgentTierRoutine, Enabled: true, Fallback: true},
	}
	tctx := store.WithTenantID(context.Background(), ms.rows[id].TenantID)
	if err := ms.UpdateAgents(tctx, id, agents); err != nil {
		t.Fatal(err)
	}
	fresh := store.OrcaAgentHealth{Status: store.OrcaAgentStatusUnhealthy, Error: "launch failed", CheckedAt: time.Now().UTC()}
	if err := ms.UpdateAgentHealth(tctx, id, agents[0], fresh); err != nil {
		t.Fatal(err)
	}

	rec := orcaDo(mux, http.MethodPost, "/v1/orca-servers/"+id.String()+"/check", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("check: %d %s", rec.Code, rec.Body)
	}
	if got := ms.rows[id].AgentHealth["codex-fast"].Status; got != store.OrcaAgentStatusUnknown {
		t.Fatalf("check endpoint must reset fresh unhealthy agent to unknown, got %q", got)
	}
}
