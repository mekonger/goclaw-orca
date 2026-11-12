package jev

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeProviders struct {
	list  []store.LLMProviderData
	err   error
	calls int
}

func (f *fakeProviders) ListProviders(context.Context) ([]store.LLMProviderData, error) {
	f.calls++
	return f.list, f.err
}

func jevProvider(name string, enabled bool, base, settings string) store.LLMProviderData {
	p := store.LLMProviderData{Name: name, Enabled: enabled, APIBase: base, APIKey: "sk-" + name}
	if settings != "" {
		p.Settings = json.RawMessage(settings)
	}
	return p
}

func TestResolve(t *testing.T) {
	cases := []struct {
		name      string
		list      []store.LLMProviderData
		err       error
		ok        bool
		endpoint  string
		key       string
		model     string
		threshold float64
	}{
		{name: "no providers"},
		{name: "list error", err: errors.New("db down")},
		{name: "non-jev only", list: []store.LLMProviderData{jevProvider("openrouter", true, "", "")}},
		{name: "disabled jev", list: []store.LLMProviderData{jevProvider("jev-main", false, "", "")}},
		{name: "default", list: []store.LLMProviderData{jevProvider("jev-main", true, "", "")}, ok: true,
			endpoint: "https://openrouter.ai/api/v1/systemone", key: "sk-jev-main", model: DefaultModel, threshold: DefaultThreshold},
		{name: "two providers first by name", list: []store.LLMProviderData{
			jevProvider("jev-z", true, "", ""), jevProvider("jev-a", true, "", ""), jevProvider("jev-0", false, "", ""),
		}, ok: true, endpoint: "https://openrouter.ai/api/v1/systemone", key: "sk-jev-a", model: DefaultModel, threshold: DefaultThreshold},
		{name: "api base override", list: []store.LLMProviderData{jevProvider("jev-main", true, "https://proxy.example/v1/", "")}, ok: true,
			endpoint: "https://proxy.example/v1/systemone", key: "sk-jev-main", model: DefaultModel, threshold: DefaultThreshold},
		{name: "settings model and threshold", list: []store.LLMProviderData{jevProvider("jev-main", true, "", `{"model":"typesafe/jev-2","threshold":0.75}`)}, ok: true,
			endpoint: "https://openrouter.ai/api/v1/systemone", key: "sk-jev-main", model: "typesafe/jev-2", threshold: 0.75},
		{name: "threshold out of range", list: []store.LLMProviderData{jevProvider("jev-main", true, "", `{"threshold":1.5}`)}, ok: true,
			endpoint: "https://openrouter.ai/api/v1/systemone", key: "sk-jev-main", model: DefaultModel, threshold: DefaultThreshold},
		{name: "threshold zero allowed", list: []store.LLMProviderData{jevProvider("jev-main", true, "", `{"threshold":0}`)}, ok: true,
			endpoint: "https://openrouter.ai/api/v1/systemone", key: "sk-jev-main", model: DefaultModel, threshold: 0},
		{name: "bad settings json", list: []store.LLMProviderData{jevProvider("jev-main", true, "", `[1]`)}, ok: true,
			endpoint: "https://openrouter.ai/api/v1/systemone", key: "sk-jev-main", model: DefaultModel, threshold: DefaultThreshold},
		{name: "empty key", list: []store.LLMProviderData{{Name: "jev-main", Enabled: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := Resolve(context.Background(), &fakeProviders{list: tc.list, err: tc.err})
			if ok != tc.ok {
				t.Fatalf("ok = %v", ok)
			}
			if !ok {
				if c != nil {
					t.Fatal("client returned with ok=false")
				}
				return
			}
			if c.endpoint != tc.endpoint || c.apiKey != tc.key || c.Model() != tc.model || c.Threshold() != tc.threshold {
				t.Fatalf("client = %s %s %s %v", c.endpoint, c.apiKey, c.model, c.threshold)
			}
		})
	}
}

func TestResolveNilStore(t *testing.T) {
	if _, ok := Resolve(context.Background(), nil); ok {
		t.Fatal("nil store resolved")
	}
}
