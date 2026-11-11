package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *PGOrcaServerStore) UpdateAgents(ctx context.Context, id uuid.UUID, agents []store.OrcaAgentConfig) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return store.ErrOrcaServerNotFound
	}
	if agents == nil {
		agents = []store.OrcaAgentConfig{}
	}
	if err := store.ValidateOrcaAgents(agents); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update orca agents: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var agentsRaw, healthRaw []byte
	err = tx.QueryRowContext(ctx,
		`SELECT agents, agent_health FROM orca_servers WHERE id = $1 AND tenant_id = $2 FOR UPDATE`, id, tid).
		Scan(&agentsRaw, &healthRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrOrcaServerNotFound
	}
	if err != nil {
		return fmt.Errorf("load orca agents: %w", err)
	}
	var cur store.OrcaServer
	if err := store.DecodeOrcaAgentColumns(agentsRaw, healthRaw, &cur); err != nil {
		return err
	}
	agentsJSON, err := json.Marshal(agents)
	if err != nil {
		return fmt.Errorf("encode orca agents: %w", err)
	}
	healthJSON, err := json.Marshal(store.PruneOrcaAgentHealth(cur.Agents, agents, cur.AgentHealth))
	if err != nil {
		return fmt.Errorf("encode orca agent health: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE orca_servers SET agents = $1::jsonb, agent_health = $2::jsonb, updated_at = $3 WHERE id = $4 AND tenant_id = $5`,
		agentsJSON, healthJSON, time.Now().UTC(), id, tid); err != nil {
		return fmt.Errorf("update orca agents: %w", err)
	}
	return tx.Commit()
}

func (s *PGOrcaServerStore) UpdateAgentHealth(ctx context.Context, id uuid.UUID, a store.OrcaAgentConfig, h store.OrcaAgentHealth) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return store.ErrOrcaServerNotFound
	}
	raw, err := json.Marshal(h)
	if err != nil {
		return fmt.Errorf("encode orca agent health: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE orca_servers SET agent_health = jsonb_set(agent_health, ARRAY[$1::text], $2::jsonb)
		 WHERE id = $3 AND tenant_id = $4
		   AND EXISTS (SELECT 1 FROM jsonb_array_elements(agents) e
		               WHERE e->>'key' = $1::text AND e->>'agent' = $5::text AND COALESCE(e->>'model', '') = $6::text)`,
		a.Key, raw, id, tid, a.Agent, a.Model)
	if err != nil {
		return fmt.Errorf("update orca agent health: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrOrcaServerNotFound
	}
	return nil
}
