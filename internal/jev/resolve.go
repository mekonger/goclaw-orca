package jev

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const ProviderPrefix = "jev-"

type providerLister interface {
	ListProviders(ctx context.Context) ([]store.LLMProviderData, error)
}

type providerSettings struct {
	Model     string   `json:"model"`
	Threshold *float64 `json:"threshold"`
}

func Resolve(ctx context.Context, providers providerLister) (*Client, bool) {
	if providers == nil {
		return nil, false
	}
	list, err := providers.ListProviders(ctx)
	if err != nil {
		slog.Warn("jev.resolve: list providers failed", "error", err)
		return nil, false
	}
	var matches []store.LLMProviderData
	for _, p := range list {
		if p.Enabled && strings.HasPrefix(p.Name, ProviderPrefix) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return nil, false
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Name < matches[j].Name })
	p := matches[0]
	if len(matches) > 1 {
		slog.Warn("jev.resolve: several jev providers enabled, using first by name", "provider", p.Name, "count", len(matches))
	}
	if strings.TrimSpace(p.APIKey) == "" {
		slog.Warn("jev.resolve: jev provider has no api key", "provider", p.Name)
		return nil, false
	}
	var s providerSettings
	if len(p.Settings) > 0 {
		if err := json.Unmarshal(p.Settings, &s); err != nil {
			slog.Warn("jev.resolve: invalid provider settings, using defaults", "provider", p.Name)
		}
	}
	model := strings.TrimSpace(s.Model)
	if model == "" {
		model = DefaultModel
	}
	threshold := DefaultThreshold
	if s.Threshold != nil {
		if *s.Threshold >= 0 && *s.Threshold <= 1 {
			threshold = *s.Threshold
		} else {
			slog.Warn("jev.resolve: threshold out of range, using default", "provider", p.Name, "threshold", *s.Threshold)
		}
	}
	base := strings.TrimSpace(p.APIBase)
	if base == "" {
		base = DefaultAPIBase
	}
	return NewClient(strings.TrimRight(base, "/")+"/systemone", p.APIKey, model, threshold, nil), true
}
