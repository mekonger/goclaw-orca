package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validOrcaAgents() []OrcaAgentConfig {
	return []OrcaAgentConfig{
		{Key: "codex-fast", Agent: "codex", Model: "gpt-5", Tier: OrcaAgentTierRoutine, Description: "quick edits", Enabled: true, Fallback: true},
		{Key: "claude-hard", Agent: "claude", Model: "sonnet", Tier: OrcaAgentTierHard, Description: "multi-file work", Enabled: true},
		{Key: "claude-plan", Agent: "claude", Model: "opus", Tier: OrcaAgentTierPlanning, Description: "planning", Enabled: true},
		{Key: "oc", Agent: "opencode", Model: "openai/gpt-5", Tier: OrcaAgentTierRoutine, Enabled: false},
	}
}

func TestValidateOrcaAgents(t *testing.T) {
	cases := []struct {
		name   string
		mutate func([]OrcaAgentConfig) []OrcaAgentConfig
		code   OrcaAgentsErrorCode
	}{
		{"valid", func(a []OrcaAgentConfig) []OrcaAgentConfig { return a }, ""},
		{"empty", func([]OrcaAgentConfig) []OrcaAgentConfig { return nil }, ""},
		{"too many", func(a []OrcaAgentConfig) []OrcaAgentConfig {
			out := make([]OrcaAgentConfig, OrcaAgentsMax+1)
			for i := range out {
				out[i] = OrcaAgentConfig{Key: "k" + strings.Repeat("a", i+1), Agent: "codex", Tier: OrcaAgentTierRoutine}
			}
			return out
		}, OrcaAgentsErrTooMany},
		{"bad key", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[0].Key = "Bad Key"; return a }, OrcaAgentsErrInvalidField},
		{"empty key", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[0].Key = ""; return a }, OrcaAgentsErrInvalidField},
		{"bad agent dash", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[0].Agent = "-rm"; return a }, OrcaAgentsErrInvalidField},
		{"bad model dash", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[0].Model = "--effort"; return a }, OrcaAgentsErrInvalidField},
		{"bad model space", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[0].Model = "gpt 5"; return a }, OrcaAgentsErrInvalidField},
		{"empty model ok", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[0].Model = ""; return a }, ""},
		{"model 128 chars ok", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[0].Model = "m" + strings.Repeat("a", 127); return a }, ""},
		{"model 129 chars", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[0].Model = "m" + strings.Repeat("a", 128); return a }, OrcaAgentsErrInvalidField},
		{"key 64 chars ok", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[0].Key = "k" + strings.Repeat("a", 63); return a }, ""},
		{"key 65 chars", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[0].Key = "k" + strings.Repeat("a", 64); return a }, OrcaAgentsErrInvalidField},
		{"agent 65 chars", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[0].Agent = "a" + strings.Repeat("a", 64); return a }, OrcaAgentsErrInvalidField},
		{"duplicate key", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[1].Key = a[0].Key; return a }, OrcaAgentsErrDuplicateKey},
		{"bad tier", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[1].Tier = "expert"; return a }, OrcaAgentsErrInvalidTier},
		{"two fallbacks", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[1].Fallback = true; return a }, OrcaAgentsErrFallback},
		{"fallback disabled", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[0].Enabled = false; return a }, OrcaAgentsErrFallback},
		{"fallback planning", func(a []OrcaAgentConfig) []OrcaAgentConfig {
			a[0].Fallback = false
			a[2].Fallback = true
			return a
		}, OrcaAgentsErrFallback},
		{"two planning", func(a []OrcaAgentConfig) []OrcaAgentConfig { a[1].Tier = OrcaAgentTierPlanning; return a }, OrcaAgentsErrPlanningLimit},
		{"description too long", func(a []OrcaAgentConfig) []OrcaAgentConfig {
			a[0].Description = strings.Repeat("x", OrcaAgentDescriptionMaxLen+1)
			return a
		}, OrcaAgentsErrDescriptionLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateOrcaAgents(tc.mutate(validOrcaAgents()))
			if tc.code == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var ve *OrcaAgentsError
			if !errors.As(err, &ve) || ve.Code != tc.code {
				t.Fatalf("got %v, want code %s", err, tc.code)
			}
		})
	}
}

func TestValidateOrcaAgentsSanitizesDescription(t *testing.T) {
	a := validOrcaAgents()
	a[0].Description = "  line\nbreak\t "
	if err := ValidateOrcaAgents(a); err != nil {
		t.Fatal(err)
	}
	if a[0].Description != "line break" {
		t.Fatalf("description = %q", a[0].Description)
	}
}

func TestValidateOrcaAgentsKeepsMaxLengthDescription(t *testing.T) {
	a := validOrcaAgents()
	a[0].Description = strings.Repeat("x", OrcaAgentDescriptionMaxLen)
	if err := ValidateOrcaAgents(a); err != nil {
		t.Fatal(err)
	}
	if len(a[0].Description) != OrcaAgentDescriptionMaxLen {
		t.Fatalf("description truncated to %d", len(a[0].Description))
	}
}

func TestPruneOrcaAgentHealth(t *testing.T) {
	old := validOrcaAgents()
	h := OrcaAgentHealth{Status: OrcaAgentStatusHealthy, CheckedAt: time.Now()}
	health := map[string]OrcaAgentHealth{"codex-fast": h, "claude-hard": h, "claude-plan": h}
	next := validOrcaAgents()[:3]
	next[1].Model = "opus"
	next[2].Description = "changed"
	got := PruneOrcaAgentHealth(old, next, health)
	if _, ok := got["codex-fast"]; !ok {
		t.Error("unchanged entry dropped")
	}
	if _, ok := got["claude-hard"]; ok {
		t.Error("model change kept health")
	}
	if _, ok := got["claude-plan"]; !ok {
		t.Error("description-only change dropped health")
	}
	if len(PruneOrcaAgentHealth(old, nil, health)) != 0 {
		t.Error("empty list kept health")
	}
}
