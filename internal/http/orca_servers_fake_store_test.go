package http

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type memOrcaStore struct {
	mu   sync.Mutex
	rows map[uuid.UUID]*store.OrcaServer
}

func newMemOrcaStore() *memOrcaStore {
	return &memOrcaStore{rows: map[uuid.UUID]*store.OrcaServer{}}
}

func (m *memOrcaStore) find(ctx context.Context, id uuid.UUID) (*store.OrcaServer, error) {
	r, ok := m.rows[id]
	if !ok || r.TenantID != store.TenantIDFromContext(ctx) {
		return nil, store.ErrOrcaServerNotFound
	}
	return r, nil
}

func (m *memOrcaStore) List(ctx context.Context) ([]store.OrcaServer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.OrcaServer
	for _, r := range m.rows {
		if r.TenantID == store.TenantIDFromContext(ctx) {
			c := *r
			c.PairingCode = ""
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memOrcaStore) Get(ctx context.Context, id uuid.UUID) (*store.OrcaServer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.find(ctx, id)
	if err != nil {
		return nil, err
	}
	c := *r
	c.PairingCode = ""
	return &c, nil
}

func (m *memOrcaStore) GetWithSecret(ctx context.Context, id uuid.UUID) (*store.OrcaServer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.find(ctx, id)
	if err != nil {
		return nil, err
	}
	c := *r
	return &c, nil
}

func (m *memOrcaStore) Create(ctx context.Context, s *store.OrcaServer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tid := store.TenantIDFromContext(ctx)
	n := 0
	for _, r := range m.rows {
		if r.TenantID == tid {
			n++
			if r.Label == s.Label {
				return store.ErrOrcaServerLabelTaken
			}
		}
	}
	if n >= store.OrcaServerMaxPerTenant {
		return store.ErrOrcaServerLimit
	}
	s.ID, s.TenantID, s.HasPairingCode, s.LastStatus = uuid.New(), tid, true, store.OrcaServerStatusUnknown
	c := *s
	m.rows[s.ID] = &c
	s.PairingCode = ""
	return nil
}

func (m *memOrcaStore) Update(ctx context.Context, id uuid.UUID, desc, code *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.find(ctx, id)
	if err != nil {
		return err
	}
	if desc != nil {
		r.Description = *desc
	}
	if code != nil {
		r.PairingCode = *code
	}
	return nil
}

func (m *memOrcaStore) Delete(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.find(ctx, id); err != nil {
		return err
	}
	delete(m.rows, id)
	return nil
}

func (m *memOrcaStore) UpdateHealth(_ context.Context, id uuid.UUID, h store.OrcaServerHealth) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rows[id]; ok {
		r.LastStatus, r.LastRuntimeState, r.LastError = h.Status, h.RuntimeState, h.Error
		t := h.CheckedAt
		r.LastCheckedAt = &t
	}
	return nil
}

func (m *memOrcaStore) ListDueForCheck(context.Context, time.Time, int) ([]store.OrcaServer, error) {
	return nil, nil
}

func (m *memOrcaStore) UpdateAgents(ctx context.Context, id uuid.UUID, agents []store.OrcaAgentConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.find(ctx, id)
	if err != nil {
		return err
	}
	if err := store.ValidateOrcaAgents(agents); err != nil {
		return err
	}
	r.AgentHealth = store.PruneOrcaAgentHealth(r.Agents, agents, r.AgentHealth)
	r.Agents = append([]store.OrcaAgentConfig{}, agents...)
	return nil
}

func (m *memOrcaStore) UpdateAgentHealth(ctx context.Context, id uuid.UUID, cfg store.OrcaAgentConfig, h store.OrcaAgentHealth) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.find(ctx, id)
	if err != nil {
		return err
	}
	for _, a := range r.Agents {
		if a.Key == cfg.Key && a.Agent == cfg.Agent && a.Model == cfg.Model {
			if r.AgentHealth == nil {
				r.AgentHealth = map[string]store.OrcaAgentHealth{}
			}
			r.AgentHealth[a.Key] = h
			return nil
		}
	}
	return store.ErrOrcaServerNotFound
}
