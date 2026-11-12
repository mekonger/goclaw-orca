package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type routeOrcaStore struct {
	store.OrcaServerStore
	mu      sync.Mutex
	srv     store.OrcaServer
	healthW []store.OrcaAgentHealth
	gets    int
}

func (s *routeOrcaStore) Get(_ context.Context, id uuid.UUID) (*store.OrcaServer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets++
	if id != s.srv.ID {
		return nil, store.ErrOrcaServerNotFound
	}
	c := s.srv
	return &c, nil
}

func (s *routeOrcaStore) UpdateAgentHealth(_ context.Context, id uuid.UUID, a store.OrcaAgentConfig, h store.OrcaAgentHealth) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healthW = append(s.healthW, h)
	if s.srv.AgentHealth == nil {
		s.srv.AgentHealth = map[string]store.OrcaAgentHealth{}
	}
	s.srv.AgentHealth[a.Key] = h
	return nil
}

type noProviders struct{ calls int }

func (p *noProviders) ListProviders(context.Context) ([]store.LLMProviderData, error) {
	p.calls++
	return nil, nil
}

func orcaRouteTool(t *testing.T, agents []store.OrcaAgentConfig) (*ExecTool, *routeOrcaStore, *labeledStubStore, context.Context) {
	t.Helper()
	tool, ls, ctx := orcaScriptTool(t, []store.SecureCLICredentialLabel{{Label: "build-vm", Source: "orca_server"}}, false)
	srvID := uuid.New()
	ls.resolved["build-vm"] = &store.SecureCLIResolvedCredential{Label: "build-vm", Source: store.SecureCLISourceOrcaServer,
		SubjectID: srvID.String(), Env: []byte(`{"ORCA_PAIRING_CODE":"server-code"}`)}
	rs := &routeOrcaStore{srv: store.OrcaServer{ID: srvID, Label: "build-vm", Agents: agents}}
	tool.orcaServers = rs
	tool.orcaProviders = &noProviders{}
	return tool, rs, ls, ctx
}

func setOrcaScript(t *testing.T, ls *labeledStubStore, script string) {
	t.Helper()
	if err := os.WriteFile(*ls.byName["orca"].BinaryPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFindFlagValueAndRewrite(t *testing.T) {
	args := []string{"agent", "launch", "--agent", "auto", "--prompt", "hi"}
	v, idx, inline := findFlagValue(args, "--agent")
	if v != "auto" || idx != 2 || inline {
		t.Fatalf("got %q %d %v", v, idx, inline)
	}
	got := strings.Join(rewriteOrcaAgentArgs(args, idx, inline, "claude", "sonnet"), " ")
	if got != "agent launch --agent claude --model sonnet --prompt hi" {
		t.Fatalf("rewrite = %s", got)
	}
	args = []string{"worktree", "create", "--agent=auto", "--name", "x"}
	v, idx, inline = findFlagValue(args, "--agent")
	got = strings.Join(rewriteOrcaAgentArgs(args, idx, inline, "codex", ""), " ")
	if v != "auto" || !inline || got != "worktree create --agent=codex --name x" {
		t.Fatalf("inline rewrite = %s", got)
	}
	if _, idx, _ := findFlagValue([]string{"agent", "launch", "--", "--agent", "auto"}, "--agent"); idx != -1 {
		t.Fatal("flag after -- must be ignored")
	}
	if got := strings.Join(rewriteOrcaAgentArgs([]string{"agent", "launch", "--agent"}, 2, false, "codex", ""), " "); got != "agent launch --agent codex" {
		t.Fatalf("trailing flag rewrite = %s", got)
	}
}

func TestOrcaLaunchCommand(t *testing.T) {
	for args, want := range map[string]string{
		"worktree create --agent auto": "worktree create",
		"agent launch --agent auto":    "agent launch",
		"worktree list --agent auto":   "",
		"status":                       "",
	} {
		if got := orcaLaunchCommand("/usr/bin/orca", strings.Fields(args)); got != want {
			t.Errorf("%s: %q", args, got)
		}
	}
	if orcaLaunchCommand("gh", []string{"agent", "launch"}) != "" {
		t.Error("non-orca binary routed")
	}
}

func TestExecOrcaAutoRoutesToFallbackWhenJevOff(t *testing.T) {
	tool, _, _, ctx := orcaRouteTool(t, policyAgents())
	res := tool.Execute(ctx, map[string]any{"command": `orca --server build-vm agent launch --agent auto --prompt "fix the bug" --json`})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.HasPrefix(res.ForLLM, "routed: --agent codex --model gpt-5 (fallback: jev off)\n") {
		t.Fatalf("missing route note: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "ARGS:agent launch --agent codex --model gpt-5 --prompt fix the bug --json") {
		t.Fatalf("args not rewritten: %s", res.ForLLM)
	}
}

func TestExecOrcaAutoErrors(t *testing.T) {
	noFallback := policyAgents()
	noFallback[0].Fallback = false
	cases := []struct {
		name, cmd, want string
		agents          []store.OrcaAgentConfig
	}{
		{"with model", `orca --server build-vm agent launch --agent auto --model x`, "picks the model", policyAgents()},
		{"with effort", `orca --server build-vm agent launch --agent=auto --effort high`, "picks the model", policyAgents()},
		{"no agents", `orca --server build-vm agent launch --agent auto`, "no coding agents are configured on Orca server build-vm", nil},
		{"no fallback", `orca --server build-vm agent launch --agent auto --prompt hi`, "codex-fast (codex gpt-5, routine, unknown)", noFallback},
		{"repeated agent", `orca --server build-vm agent launch --agent auto --prompt hi --agent=auto`, "more than once", policyAgents()},
		{"repeated model explicit", `orca --server build-vm agent launch --agent claude --model a --model=b`, "more than once", policyAgents()},
		{"planning missing", `orca --server build-vm agent launch --agent auto --prompt "/planning x"`, "no usable planning agent", policyAgents()[:3]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool, _, _, ctx := orcaRouteTool(t, tc.agents)
			res := tool.Execute(ctx, map[string]any{"command": tc.cmd})
			if !res.IsError || !strings.Contains(res.ForLLM, tc.want) || strings.Contains(res.ForLLM, "ARGS:") {
				t.Fatalf("got %v %s", res.IsError, res.ForLLM)
			}
		})
	}
}

func TestExecOrcaAutoUnroutableNeverRunsOrca(t *testing.T) {
	tool, _, ctx := orcaScriptTool(t, twoServers, true)
	res := tool.Execute(ctx, map[string]any{"command": `orca agent launch --agent auto --prompt hi`})
	if !res.IsError || !strings.Contains(res.ForLLM, "--server <orca-server-label>") || strings.Contains(res.ForLLM, "ARGS:") {
		t.Fatalf("default credential: %s", res.ForLLM)
	}
	res = tool.Execute(ctx, map[string]any{"command": `orca --server default agent launch --agent auto`})
	if !res.IsError || strings.Contains(res.ForLLM, "ARGS:") {
		t.Fatalf("--server default: %s", res.ForLLM)
	}
	res = tool.Execute(ctx, map[string]any{"command": `orca --json agent launch --agent auto`})
	if !res.IsError || strings.Contains(res.ForLLM, "ARGS:") {
		t.Fatalf("unrecognised command with auto: %s", res.ForLLM)
	}
	tool2, _, _, ctx2 := orcaRouteTool(t, policyAgents())
	tool2.orcaServers = nil
	res = tool2.Execute(ctx2, map[string]any{"command": `orca --server build-vm agent launch --agent auto`})
	if !res.IsError || strings.Contains(res.ForLLM, "ARGS:") {
		t.Fatalf("nil stores: %s", res.ForLLM)
	}
}

func TestExecOrcaUntouchedWithoutAuto(t *testing.T) {
	tool, rs, _, ctx := orcaRouteTool(t, policyAgents())
	res := tool.Execute(ctx, map[string]any{"command": `orca --server build-vm worktree list --json`})
	if res.IsError || strings.HasPrefix(res.ForLLM, "routed") || !strings.Contains(res.ForLLM, "ARGS:worktree list --json") || rs.gets != 0 {
		t.Fatalf("untouched command changed: gets=%d %s", rs.gets, res.ForLLM)
	}
	res = tool.Execute(ctx, map[string]any{"command": `orca --server build-vm agent launch --agent claude --model sonnet`})
	if res.IsError || strings.Contains(res.ForLLM, "warning") || !strings.Contains(res.ForLLM, "ARGS:agent launch --agent claude --model sonnet") {
		t.Fatalf("explicit configured agent: %s", res.ForLLM)
	}
}

func TestExecOrcaExplicitUnconfiguredAgentWarns(t *testing.T) {
	tool, _, _, ctx := orcaRouteTool(t, policyAgents())
	res := tool.Execute(ctx, map[string]any{"command": `orca --server build-vm agent launch --agent amp`})
	if res.IsError || !strings.HasPrefix(res.ForLLM, `warning: agent "amp" is not configured on Orca server build-vm`) || !strings.Contains(res.ForLLM, "ARGS:agent launch --agent amp") {
		t.Fatalf("explicit agent: %s", res.ForLLM)
	}
}

func TestExecOrcaDenyRunsOnRewrittenArgs(t *testing.T) {
	tool, _, ls, ctx := orcaRouteTool(t, policyAgents())
	deny, _ := json.Marshal([]string{`--agent codex`})
	ls.byName["orca"].DenyArgs = deny
	res := tool.Execute(ctx, map[string]any{"command": `orca --server build-vm agent launch --agent auto`})
	if !res.IsError || strings.Contains(res.ForLLM, "ARGS:") || !strings.Contains(res.ForLLM, "blocked") {
		t.Fatalf("deny must see rewritten args: %s", res.ForLLM)
	}
}

func TestExecOrcaUnknownAgentMarksUnhealthy(t *testing.T) {
	tool, rs, ls, ctx := orcaRouteTool(t, policyAgents())
	reply := strings.Replace(readOrcaFixture(t, "agent_launch_unknown_agent.json"), "nosuchagent", "codex", 1)
	setOrcaScript(t, ls, "#!/bin/sh\ncat <<'EOF'\n"+reply+"\nEOF\necho 'some stderr noise' >&2\nexit 1\n")
	res := tool.Execute(ctx, map[string]any{"command": `orca --server build-vm agent launch --agent auto --prompt hi`})
	if !res.IsError {
		t.Fatalf("expected failure: %s", res.ForLLM)
	}
	deadline := time.Now().Add(time.Second)
	for {
		rs.mu.Lock()
		h, n := rs.srv.AgentHealth["codex-fast"], len(rs.healthW)
		rs.mu.Unlock()
		if n == 1 && h.Status == store.OrcaAgentStatusUnhealthy && strings.Contains(h.Error, "Unknown TUI agent") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("health not recorded: %+v (%d writes)", h, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestExecOrcaSuccessDoesNotTouchHealth(t *testing.T) {
	tool, rs, ls, ctx := orcaRouteTool(t, policyAgents())
	setOrcaScript(t, ls, "#!/bin/sh\ncat <<'EOF'\n"+readOrcaFixture(t, "agent_launch_ok_chat.json")+"\nEOF\n")
	res := tool.Execute(ctx, map[string]any{"command": `orca --server build-vm agent launch --agent auto --prompt hi`})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if len(rs.healthW) != 0 {
		t.Fatalf("ok:true must not write health: %+v", rs.healthW)
	}
}

func TestInheritOrcaRouting(t *testing.T) {
	parent, _, _, _ := orcaRouteTool(t, policyAgents())
	child := NewExecTool(t.TempDir(), false)
	child.InheritOrcaRouting(parent)
	if child.orcaServers != parent.orcaServers || child.orcaProviders != parent.orcaProviders {
		t.Fatal("routing stores not inherited")
	}
}

func TestExecOrcaJevProvidersResolvedOnlyForMultiCandidate(t *testing.T) {
	unhealthyClaudeHard := map[string]store.OrcaAgentHealth{"claude-hard": status(store.OrcaAgentStatusUnhealthy)}
	cases := []struct {
		name, cmd string
		agents    []store.OrcaAgentConfig
		health    map[string]store.OrcaAgentHealth
		wantCalls int
	}{
		{"planning rule", `orca --server build-vm agent launch --agent auto --prompt "/planning x"`, policyAgents(), nil, 0},
		{"cook rule", `orca --server build-vm agent launch --agent auto --prompt "/cook plan.md"`, policyAgents(), nil, 0},
		{"single candidate", `orca --server build-vm agent launch --agent auto --prompt "fix"`, policyAgents()[:2], unhealthyClaudeHard, 0},
		{"explicit agent", `orca --server build-vm agent launch --agent claude --model sonnet --prompt "fix"`, policyAgents(), nil, 0},
		{"multi candidate", `orca --server build-vm agent launch --agent auto --prompt "fix the bug"`, policyAgents(), nil, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool, rs, _, ctx := orcaRouteTool(t, tc.agents)
			rs.srv.AgentHealth = tc.health
			res := tool.Execute(ctx, map[string]any{"command": tc.cmd})
			if res.IsError {
				t.Fatalf("unexpected error: %s", res.ForLLM)
			}
			if got := tool.orcaProviders.(*noProviders).calls; got != tc.wantCalls {
				t.Fatalf("ListProviders calls = %d, want %d (%s)", got, tc.wantCalls, res.ForLLM)
			}
		})
	}
}

func TestExecOrcaSubagentRoutesLikeParent(t *testing.T) {
	parent, _, ls, ctx := orcaRouteTool(t, policyAgents())
	cmd := map[string]any{"command": `orca --server build-vm agent launch --agent auto --prompt hi`}

	bare := NewExecTool(t.TempDir(), false)
	bare.SetSecureCLIStore(ls)
	if res := bare.Execute(ctx, cmd); !res.IsError || strings.Contains(res.ForLLM, "ARGS:") {
		t.Fatalf("child without inherited routing must refuse: %s", res.ForLLM)
	}

	child := NewExecTool(t.TempDir(), false)
	child.SetSecureCLIStore(ls)
	child.InheritOrcaRouting(parent)
	res := child.Execute(ctx, cmd)
	if res.IsError || !strings.HasPrefix(res.ForLLM, "routed: --agent codex --model gpt-5 (fallback: jev off)\n") ||
		!strings.Contains(res.ForLLM, "ARGS:agent launch --agent codex --model gpt-5 --prompt hi") {
		t.Fatalf("child did not route like parent: %s", res.ForLLM)
	}
}

func TestExecOrcaDenyRunsBeforeJev(t *testing.T) {
	tool, _, ls, ctx := orcaRouteTool(t, policyAgents())
	deny, _ := json.Marshal([]string{`secret-repo`})
	ls.byName["orca"].DenyArgs = deny
	providers := &noProviders{}
	tool.orcaProviders = providers
	res := tool.Execute(ctx, map[string]any{"command": `orca --server build-vm agent launch --agent auto --worktree path:/secret-repo --prompt hi`})
	if !res.IsError || providers.calls != 0 {
		t.Fatalf("deny must short-circuit before Jev: calls=%d %s", providers.calls, res.ForLLM)
	}
}
