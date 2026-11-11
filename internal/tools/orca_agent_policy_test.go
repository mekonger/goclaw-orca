package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/jev"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeJev struct {
	ans       jev.Answer
	err       error
	threshold float64
	calls     int
	criteria  map[string]string
}

func (f *fakeJev) Choose(_ context.Context, state map[string]any, q jev.Question) (jev.Answer, error) {
	f.calls++
	f.criteria = q.Criteria
	return f.ans, f.err
}

func (f *fakeJev) Threshold() float64 { return f.threshold }

func policyAgents() []store.OrcaAgentConfig {
	return []store.OrcaAgentConfig{
		{Key: "codex-fast", Agent: "codex", Model: "gpt-5", Tier: store.OrcaAgentTierRoutine, Description: "quick", Enabled: true, Fallback: true},
		{Key: "claude-hard", Agent: "claude", Model: "sonnet", Tier: store.OrcaAgentTierHard, Description: "big", Enabled: true},
		{Key: "claude-hard2", Agent: "claude", Model: "opus", Tier: store.OrcaAgentTierHard, Description: "bigger", Enabled: true},
		{Key: "claude-plan", Agent: "claude", Model: "opus", Tier: store.OrcaAgentTierPlanning, Description: "plan", Enabled: true},
		{Key: "off", Agent: "gemini", Tier: store.OrcaAgentTierRoutine, Enabled: false},
	}
}

func status(s string) store.OrcaAgentHealth { return store.OrcaAgentHealth{Status: s} }

func TestPickOrcaAgent(t *testing.T) {
	healthy, unhealthy := store.OrcaAgentStatusHealthy, store.OrcaAgentStatusUnhealthy
	cases := []struct {
		name      string
		agents    []store.OrcaAgentConfig
		health    map[string]store.OrcaAgentHealth
		prompt    string
		jev       *fakeJev
		wantKey   string
		wantFail  orcaPickFailure
		wantCalls int
		reason    string
	}{
		{name: "no agents", agents: []store.OrcaAgentConfig{}, wantFail: orcaPickNoAgents},
		{name: "planning hit", prompt: "/planning add auth", jev: &fakeJev{}, wantKey: "claude-plan", reason: "rule: /planning"},
		{name: "planning miss", agents: policyAgents()[:3], prompt: "/planning x", wantFail: orcaPickNoPlanning},
		{name: "planning unhealthy", prompt: "/planning", health: map[string]store.OrcaAgentHealth{"claude-plan": status(unhealthy)}, wantFail: orcaPickNoPlanning},
		{name: "planning prefix only", prompt: "/planningx do", jev: &fakeJev{err: errors.New("x")}, wantKey: "codex-fast", wantCalls: 1},
		{name: "cook healthy hard first", prompt: "/cook plan.md", jev: &fakeJev{},
			health: map[string]store.OrcaAgentHealth{"claude-hard2": status(healthy)}, wantKey: "claude-hard2", reason: "rule: /cook"},
		{name: "cook unknown hard", prompt: "/cook", health: map[string]store.OrcaAgentHealth{"claude-hard": status(unhealthy)}, wantKey: "claude-hard2"},
		{name: "cook falls back", prompt: "/cook", health: map[string]store.OrcaAgentHealth{"claude-hard": status(unhealthy), "claude-hard2": status(unhealthy)},
			wantKey: "codex-fast", reason: "fallback: no usable hard agent for /cook"},
		{name: "single candidate skips jev", agents: policyAgents()[:2], health: map[string]store.OrcaAgentHealth{"claude-hard": status(unhealthy)},
			jev: &fakeJev{}, wantKey: "codex-fast", reason: "single candidate"},
		{name: "jev off", wantKey: "codex-fast", reason: "fallback: jev off"},
		{name: "jev picks", jev: &fakeJev{ans: jev.Answer{Choice: "claude-hard", Confidence: 0.86}, threshold: 0.6}, wantKey: "claude-hard", wantCalls: 1, reason: "jev 0.86"},
		{name: "jev error", jev: &fakeJev{err: errors.New("boom")}, wantKey: "codex-fast", wantCalls: 1, reason: "fallback: jev error"},
		{name: "jev low confidence", jev: &fakeJev{ans: jev.Answer{Choice: "claude-hard", Confidence: 0.4}, threshold: 0.6}, wantKey: "codex-fast", wantCalls: 1},
		{name: "jev unknown key", jev: &fakeJev{ans: jev.Answer{Choice: "off", Confidence: 0.9}, threshold: 0.6}, wantKey: "codex-fast", wantCalls: 1,
			reason: "fallback: jev picked an unknown agent"},
		{name: "unhealthy fallback", jev: &fakeJev{err: errors.New("boom")}, health: map[string]store.OrcaAgentHealth{"codex-fast": status(unhealthy)},
			wantFail: orcaPickNoUsable, wantCalls: 1},
		{name: "unknown status eligible", jev: &fakeJev{ans: jev.Answer{Choice: "claude-hard2", Confidence: 0.7}, threshold: 0.6},
			health: map[string]store.OrcaAgentHealth{"claude-hard": status(healthy)}, wantKey: "claude-hard2", wantCalls: 1},
		{name: "no candidates no fallback", agents: []store.OrcaAgentConfig{{Key: "p", Agent: "claude", Tier: store.OrcaAgentTierPlanning, Enabled: true}},
			wantFail: orcaPickNoUsable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agents := tc.agents
			if agents == nil {
				agents = policyAgents()
			}
			var fn func() jevChooser
			if tc.jev != nil {
				fn = func() jevChooser { return tc.jev }
			}
			pick, fail := pickOrcaAgent(context.Background(), agents, tc.health, "worktree create", tc.prompt, fn)
			if fail != tc.wantFail {
				t.Fatalf("failure = %v, want %v", fail, tc.wantFail)
			}
			if fail == orcaPickOK && pick.Entry.Key != tc.wantKey {
				t.Fatalf("key = %s, want %s (%s)", pick.Entry.Key, tc.wantKey, pick.Reason)
			}
			if tc.reason != "" && pick.Reason != tc.reason {
				t.Fatalf("reason = %q, want %q", pick.Reason, tc.reason)
			}
			if tc.jev != nil && tc.jev.calls != tc.wantCalls {
				t.Fatalf("jev calls = %d, want %d", tc.jev.calls, tc.wantCalls)
			}
		})
	}
}

func TestPickOrcaAgentJevCriteriaExcludesPlanningAndDisabled(t *testing.T) {
	f := &fakeJev{ans: jev.Answer{Choice: "claude-hard", Confidence: 0.9}, threshold: 0.6}
	if _, fail := pickOrcaAgent(context.Background(), policyAgents(), nil, "agent launch", "fix bug", func() jevChooser { return f }); fail != orcaPickOK {
		t.Fatal(fail)
	}
	if len(f.criteria) != 3 || f.criteria["claude-plan"] != "" || f.criteria["off"] != "" || f.criteria["codex-fast"] != "quick" {
		t.Fatalf("criteria = %v", f.criteria)
	}
}
