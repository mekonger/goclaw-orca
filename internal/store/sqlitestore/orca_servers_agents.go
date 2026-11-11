//go:build sqlite || sqliteonly

package sqlitestore

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

func (s *SQLiteOrcaServerStore) UpdateAgents(ctx context.Context, id uuid.UUID, agents []store.OrcaAgentConfig) error {
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
		`SELECT agents, agent_health FROM orca_servers WHERE id = ? AND tenant_id = ?`, id.String(), tid.String()).
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
		`UPDATE orca_servers SET agents = ?, agent_health = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		string(agentsJSON), string(healthJSON), time.Now().UTC().Format(orcaTimeLayout), id.String(), tid.String()); err != nil {
		return fmt.Errorf("update orca agents: %w", err)
	}
	return tx.Commit()
}

func (s *SQLiteOrcaServerStore) UpdateAgentHealth(ctx context.Context, id uuid.UUID, a store.OrcaAgentConfig, h store.OrcaAgentHealth) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return store.ErrOrcaServerNotFound
	}
	raw, err := json.Marshal(h)
	if err != nil {
		return fmt.Errorf("encode orca agent health: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE orca_servers SET agent_health = json_set(agent_health, '$."' || ? || '"', json(?))
		 WHERE id = ? AND tenant_id = ?
		   AND EXISTS (SELECT 1 FROM json_each(agents)
		               WHERE json_extract(value, '$.key') = ? AND json_extract(value, '$.agent') = ?
		                 AND COALESCE(json_extract(value, '$.model'), '') = ?)`,
		a.Key, string(raw), id.String(), tid.String(), a.Key, a.Agent, a.Model)
	if err != nil {
		return fmt.Errorf("update orca agent health: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrOrcaServerNotFound
	}
	return nil
}
