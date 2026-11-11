package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const orcaAgentsBody = `{"agents":[
 {"key":"codex-fast","agent":"codex","model":"gpt-5","tier":"routine","description":"quick","enabled":true,"fallback":true},
 {"key":"claude-hard","agent":"claude","model":"sonnet","tier":"hard","description":"big","enabled":true}]}`

func TestOrcaServersUpdateAgents(t *testing.T) {
	mux, ms := newOrcaTestMux(t, oauthAdminTS{})
	id := orcaCreate(t, mux, "prod")

	rec := orcaDo(mux, "GET", "/v1/orca-servers/"+id, "", nil)
	if !strings.Contains(rec.Body.String(), `"agents"`) {
		t.Fatalf("get missing agents field: %s", rec.Body)
	}

	rec = orcaDo(mux, "PUT", "/v1/orca-servers/"+id+"/agents", orcaAgentsBody, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("put agents: %d %s", rec.Code, rec.Body)
	}
	var srv store.OrcaServer
	if err := json.Unmarshal(rec.Body.Bytes(), &srv); err != nil {
		t.Fatal(err)
	}
	if len(srv.Agents) != 2 || srv.Agents[0].Key != "codex-fast" || !srv.Agents[0].Fallback {
		t.Fatalf("agents = %+v", srv.Agents)
	}
	if strings.Contains(rec.Body.String(), "very-secret-payload") {
		t.Fatal("response leaked pairing code")
	}

	uid := uuid.MustParse(id)
	ctx := store.WithTenantID(t.Context(), ms.rows[uid].TenantID)
	if err := ms.UpdateAgentHealth(ctx, uid, srv.Agents[1], store.OrcaAgentHealth{Status: store.OrcaAgentStatusHealthy, CheckedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	rec = orcaDo(mux, "PUT", "/v1/orca-servers/"+id+"/agents", `{"agents":[]}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear agents: %d %s", rec.Code, rec.Body)
	}
	if r := ms.rows[uid]; len(r.Agents) != 0 || len(r.AgentHealth) != 0 {
		t.Fatalf("clear left agents=%v health=%v", r.Agents, r.AgentHealth)
	}
}

func TestOrcaServersUpdateAgentsValidation(t *testing.T) {
	mux, _ := newOrcaTestMux(t, oauthAdminTS{})
	id := orcaCreate(t, mux, "prod")
	cases := []struct {
		name, body, want string
	}{
		{"bad agent", `{"agents":[{"key":"a","agent":"-x","tier":"routine","enabled":true}]}`, "invalid agent"},
		{"bad tier", `{"agents":[{"key":"a","agent":"codex","tier":"expert","enabled":true}]}`, "invalid tier"},
		{"duplicate", `{"agents":[{"key":"a","agent":"codex","tier":"routine"},{"key":"a","agent":"claude","tier":"hard"}]}`, "duplicate"},
		{"fallback disabled", `{"agents":[{"key":"a","agent":"codex","tier":"routine","fallback":true}]}`, "fallback"},
		{"malformed", `{`, ""},
	}
	for _, tc := range cases {
		rec := orcaDo(mux, "PUT", "/v1/orca-servers/"+id+"/agents", tc.body, nil)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s: %d %s", tc.name, rec.Code, rec.Body)
		}
	}
	if rec := orcaDo(mux, "PUT", "/v1/orca-servers/"+uuid.NewString()+"/agents", `{"agents":[]}`, nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: %d", rec.Code)
	}
}
