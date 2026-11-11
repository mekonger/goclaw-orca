package store

import (
	"fmt"
	"regexp"
	"time"
)

const (
	OrcaAgentTierRoutine  = "routine"
	OrcaAgentTierHard     = "hard"
	OrcaAgentTierPlanning = "planning"

	OrcaAgentStatusUnknown   = "unknown"
	OrcaAgentStatusHealthy   = "healthy"
	OrcaAgentStatusUnhealthy = "unhealthy"

	OrcaAgentsMax              = 10
	OrcaAgentDescriptionMaxLen = 300
)

type OrcaAgentConfig struct {
	Key         string `json:"key"`
	Agent       string `json:"agent"`
	Model       string `json:"model,omitempty"`
	Tier        string `json:"tier"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Fallback    bool   `json:"fallback,omitempty"`
}

type OrcaAgentHealth struct {
	Status    string     `json:"status"`
	Error     string     `json:"error,omitempty"`
	CheckedAt time.Time  `json:"checked_at"`
	LastOKAt  *time.Time `json:"last_ok_at,omitempty"`
}

type OrcaAgentsErrorCode string

const (
	OrcaAgentsErrTooMany         OrcaAgentsErrorCode = "too_many"
	OrcaAgentsErrInvalidField    OrcaAgentsErrorCode = "invalid_field"
	OrcaAgentsErrDuplicateKey    OrcaAgentsErrorCode = "duplicate_key"
	OrcaAgentsErrInvalidTier     OrcaAgentsErrorCode = "invalid_tier"
	OrcaAgentsErrFallback        OrcaAgentsErrorCode = "fallback"
	OrcaAgentsErrPlanningLimit   OrcaAgentsErrorCode = "planning_limit"
	OrcaAgentsErrDescriptionLong OrcaAgentsErrorCode = "description_too_long"
)

type OrcaAgentsError struct {
	Code OrcaAgentsErrorCode
	Args []any
}

func (e *OrcaAgentsError) Error() string {
	return fmt.Sprintf("invalid orca agents: %s %v", e.Code, e.Args)
}

var (
	orcaAgentIDRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	orcaAgentModelRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
)

func ValidateOrcaAgents(agents []OrcaAgentConfig) error {
	if len(agents) > OrcaAgentsMax {
		return &OrcaAgentsError{Code: OrcaAgentsErrTooMany, Args: []any{OrcaAgentsMax}}
	}
	seen := map[string]bool{}
	fallbacks, planning := 0, 0
	for i := range agents {
		a := &agents[i]
		n := i + 1
		if !orcaAgentIDRe.MatchString(a.Key) {
			return &OrcaAgentsError{Code: OrcaAgentsErrInvalidField, Args: []any{n, "key", a.Key}}
		}
		if !orcaAgentIDRe.MatchString(a.Agent) {
			return &OrcaAgentsError{Code: OrcaAgentsErrInvalidField, Args: []any{n, "agent", a.Agent}}
		}
		if a.Model != "" && !orcaAgentModelRe.MatchString(a.Model) {
			return &OrcaAgentsError{Code: OrcaAgentsErrInvalidField, Args: []any{n, "model", a.Model}}
		}
		if seen[a.Key] {
			return &OrcaAgentsError{Code: OrcaAgentsErrDuplicateKey, Args: []any{a.Key}}
		}
		seen[a.Key] = true
		switch a.Tier {
		case OrcaAgentTierRoutine, OrcaAgentTierHard:
		case OrcaAgentTierPlanning:
			planning++
		default:
			return &OrcaAgentsError{Code: OrcaAgentsErrInvalidTier, Args: []any{n, a.Tier}}
		}
		if len([]rune(a.Description)) > OrcaAgentDescriptionMaxLen {
			return &OrcaAgentsError{Code: OrcaAgentsErrDescriptionLong, Args: []any{OrcaAgentDescriptionMaxLen}}
		}
		a.Description = sanitizeDescription(a.Description, OrcaAgentDescriptionMaxLen)
		if a.Fallback {
			fallbacks++
			if !a.Enabled || a.Tier == OrcaAgentTierPlanning {
				return &OrcaAgentsError{Code: OrcaAgentsErrFallback}
			}
		}
	}
	if fallbacks > 1 {
		return &OrcaAgentsError{Code: OrcaAgentsErrFallback}
	}
	if planning > 1 {
		return &OrcaAgentsError{Code: OrcaAgentsErrPlanningLimit}
	}
	return nil
}

func PruneOrcaAgentHealth(old, next []OrcaAgentConfig, health map[string]OrcaAgentHealth) map[string]OrcaAgentHealth {
	prev := make(map[string]OrcaAgentConfig, len(old))
	for _, a := range old {
		prev[a.Key] = a
	}
	out := map[string]OrcaAgentHealth{}
	for _, a := range next {
		p, ok := prev[a.Key]
		h, has := health[a.Key]
		if ok && has && p.Agent == a.Agent && p.Model == a.Model {
			out[a.Key] = h
		}
	}
	return out
}
