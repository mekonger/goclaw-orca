package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/jev"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	orcaJevBriefMaxRunes = 6000
	orcaJevTimeout       = 25 * time.Second
	orcaJevInstructions  = "Pick the coding agent best suited to the task in the brief. Each option describes one agent and model."
)

type jevChooser interface {
	Choose(ctx context.Context, state map[string]any, q jev.Question) (jev.Answer, error)
	Threshold() float64
}

type orcaPick struct {
	Entry  store.OrcaAgentConfig
	Reason string
	Jev    *jev.Answer
}

type orcaPickFailure int

const (
	orcaPickOK orcaPickFailure = iota
	orcaPickNoAgents
	orcaPickNoPlanning
	orcaPickNoUsable
)

func orcaAgentStatus(health map[string]store.OrcaAgentHealth, key string) string {
	if h, ok := health[key]; ok && h.Status != "" {
		return h.Status
	}
	return store.OrcaAgentStatusUnknown
}

func promptHasCommand(prompt, cmd string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(prompt), cmd)
	return ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n')
}

func truncateBrief(s string) string {
	if utf8.RuneCountInString(s) <= orcaJevBriefMaxRunes {
		return s
	}
	return string([]rune(s)[:orcaJevBriefMaxRunes])
}

func pickOrcaAgent(ctx context.Context, agents []store.OrcaAgentConfig, health map[string]store.OrcaAgentHealth,
	command, prompt string, jevFn func() jevChooser) (orcaPick, orcaPickFailure) {

	if len(agents) == 0 {
		return orcaPick{}, orcaPickNoAgents
	}
	usable := func(a store.OrcaAgentConfig) bool {
		return a.Enabled && orcaAgentStatus(health, a.Key) != store.OrcaAgentStatusUnhealthy
	}
	fallback := func(reason string) (orcaPick, orcaPickFailure) {
		for _, a := range agents {
			if a.Fallback && usable(a) {
				return orcaPick{Entry: a, Reason: "fallback: " + reason}, orcaPickOK
			}
		}
		return orcaPick{}, orcaPickNoUsable
	}

	if promptHasCommand(prompt, "/planning") {
		for _, a := range agents {
			if a.Tier == store.OrcaAgentTierPlanning && usable(a) {
				return orcaPick{Entry: a, Reason: "rule: /planning"}, orcaPickOK
			}
		}
		return orcaPick{}, orcaPickNoPlanning
	}

	if promptHasCommand(prompt, "/cook") {
		for _, want := range []string{store.OrcaAgentStatusHealthy, store.OrcaAgentStatusUnknown} {
			for _, a := range agents {
				if a.Enabled && a.Tier == store.OrcaAgentTierHard && orcaAgentStatus(health, a.Key) == want {
					return orcaPick{Entry: a, Reason: "rule: /cook"}, orcaPickOK
				}
			}
		}
		return fallback("no usable hard agent for /cook")
	}

	var candidates []store.OrcaAgentConfig
	for _, a := range agents {
		if usable(a) && (a.Tier == store.OrcaAgentTierRoutine || a.Tier == store.OrcaAgentTierHard) {
			candidates = append(candidates, a)
		}
	}
	switch len(candidates) {
	case 0:
		return fallback("no usable candidates")
	case 1:
		return orcaPick{Entry: candidates[0], Reason: "single candidate"}, orcaPickOK
	}

	var chooser jevChooser
	if jevFn != nil {
		chooser = jevFn()
	}
	if chooser == nil {
		return fallback("jev off")
	}
	criteria := make(map[string]string, len(candidates))
	for _, a := range candidates {
		desc := a.Description
		if desc == "" {
			desc = fmt.Sprintf("%s agent, model %s, %s tier", a.Agent, a.Model, a.Tier)
		}
		criteria[a.Key] = desc
	}
	jctx, cancel := context.WithTimeout(ctx, orcaJevTimeout)
	defer cancel()
	ans, err := chooser.Choose(jctx, map[string]any{"command": command, "brief": truncateBrief(prompt)},
		jev.Question{Instructions: orcaJevInstructions, Criteria: criteria})
	if err != nil {
		slog.Warn("orca.route.jev_failed", "error", err)
		return fallback("jev error")
	}
	if ans.Confidence < chooser.Threshold() {
		p, f := fallback(fmt.Sprintf("jev confidence %.2f below %.2f", ans.Confidence, chooser.Threshold()))
		p.Jev = &ans
		return p, f
	}
	for _, a := range candidates {
		if a.Key == ans.Choice {
			return orcaPick{Entry: a, Reason: fmt.Sprintf("jev %.2f", ans.Confidence), Jev: &ans}, orcaPickOK
		}
	}
	p, f := fallback("jev picked an unknown agent")
	p.Jev = &ans
	return p, f
}
