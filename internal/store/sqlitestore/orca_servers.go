//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var _ store.OrcaServerStore = (*SQLiteOrcaServerStore)(nil)

const orcaTimeLayout = "2006-01-02T15:04:05.000Z"

type SQLiteOrcaServerStore struct {
	db     *sql.DB
	encKey string
}

func NewSQLiteOrcaServerStore(db *sql.DB, encryptionKey string) *SQLiteOrcaServerStore {
	return &SQLiteOrcaServerStore{db: db, encKey: encryptionKey}
}

const orcaServerCols = `id, tenant_id, label, description, pairing_code, last_status, last_runtime_state,
 last_error, last_checked_at, last_healthy_at, created_at, updated_at, created_by, agents, agent_health`

func (s *SQLiteOrcaServerStore) encrypt(code string) ([]byte, error) {
	if s.encKey == "" {
		return []byte(code), nil
	}
	enc, err := crypto.Encrypt(code, s.encKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt orca pairing code: %w", err)
	}
	return []byte(enc), nil
}

func (s *SQLiteOrcaServerStore) decrypt(raw []byte) (string, error) {
	if s.encKey == "" {
		return string(raw), nil
	}
	dec, err := crypto.Decrypt(string(raw), s.encKey)
	if err != nil {
		return "", fmt.Errorf("decrypt orca pairing code: %w", err)
	}
	return dec, nil
}

type orcaRowScanner interface{ Scan(dest ...any) error }

func (s *SQLiteOrcaServerStore) scan(row orcaRowScanner, withSecret bool) (*store.OrcaServer, error) {
	var (
		o                    store.OrcaServer
		code                 []byte
		idStr, tidStr        string
		checked, healthy     nullSqliteTime
		createdAt, updatedAt sqliteTime
		agentsRaw, healthRaw []byte
	)
	if err := row.Scan(&idStr, &tidStr, &o.Label, &o.Description, &code, &o.LastStatus, &o.LastRuntimeState,
		&o.LastError, &checked, &healthy, &createdAt, &updatedAt, &o.CreatedBy, &agentsRaw, &healthRaw); err != nil {
		return nil, err
	}
	o.ID, _ = uuid.Parse(idStr)
	o.TenantID, _ = uuid.Parse(tidStr)
	o.HasPairingCode = len(code) > 0
	if err := store.DecodeOrcaAgentColumns(agentsRaw, healthRaw, &o); err != nil {
		return nil, err
	}
	o.CreatedAt, o.UpdatedAt = createdAt.Time, updatedAt.Time
	if checked.Valid {
		o.LastCheckedAt = &checked.Time
	}
	if healthy.Valid {
		o.LastHealthyAt = &healthy.Time
	}
	if withSecret {
		plain, err := s.decrypt(code)
		if err != nil {
			return &o, err
		}
		o.PairingCode = plain
	}
	return &o, nil
}

func (s *SQLiteOrcaServerStore) List(ctx context.Context) ([]store.OrcaServer, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+orcaServerCols+` FROM orca_servers WHERE tenant_id = ? ORDER BY label`, tid.String())
	if err != nil {
		return nil, fmt.Errorf("list orca servers: %w", err)
	}
	defer rows.Close()
	var out []store.OrcaServer
	for rows.Next() {
		o, err := s.scan(rows, false)
		if err != nil {
			return nil, fmt.Errorf("scan orca server: %w", err)
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

func (s *SQLiteOrcaServerStore) get(ctx context.Context, id uuid.UUID, withSecret bool) (*store.OrcaServer, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, store.ErrOrcaServerNotFound
	}
	o, err := s.scan(s.db.QueryRowContext(ctx,
		`SELECT `+orcaServerCols+` FROM orca_servers WHERE id = ? AND tenant_id = ?`, id.String(), tid.String()), withSecret)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrOrcaServerNotFound
	}
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (s *SQLiteOrcaServerStore) Get(ctx context.Context, id uuid.UUID) (*store.OrcaServer, error) {
	return s.get(ctx, id, false)
}

func (s *SQLiteOrcaServerStore) GetWithSecret(ctx context.Context, id uuid.UUID) (*store.OrcaServer, error) {
	return s.get(ctx, id, true)
}

func (s *SQLiteOrcaServerStore) Create(ctx context.Context, srv *store.OrcaServer) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return errors.New("tenant_id required")
	}
	if err := store.PrepareOrcaServerInput(srv); err != nil {
		return err
	}
	enc, err := s.encrypt(srv.PairingCode)
	if err != nil {
		return err
	}
	if srv.ID == uuid.Nil {
		srv.ID = store.GenNewID()
	}
	now := time.Now().UTC()
	nowStr := now.Format(orcaTimeLayout)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO orca_servers (id, tenant_id, label, description, pairing_code, created_at, updated_at, created_by)
		 SELECT ?, ?, ?, ?, ?, ?, ?, ?
		 WHERE (SELECT COUNT(*) FROM orca_servers WHERE tenant_id = ?) < ?`,
		srv.ID.String(), tid.String(), srv.Label, srv.Description, enc, nowStr, nowStr, srv.CreatedBy,
		tid.String(), store.OrcaServerMaxPerTenant)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return store.ErrOrcaServerLabelTaken
		}
		return fmt.Errorf("create orca server: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrOrcaServerLimit
	}
	srv.TenantID = tid
	srv.PairingCode = ""
	srv.HasPairingCode = true
	srv.LastStatus = store.OrcaServerStatusUnknown
	srv.Agents = []store.OrcaAgentConfig{}
	srv.AgentHealth = map[string]store.OrcaAgentHealth{}
	srv.CreatedAt, srv.UpdatedAt = now, now
	return nil
}

func (s *SQLiteOrcaServerStore) Update(ctx context.Context, id uuid.UUID, description *string, pairingCode *string) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return store.ErrOrcaServerNotFound
	}
	sets := []string{"updated_at = ?"}
	args := []any{time.Now().UTC().Format(orcaTimeLayout)}
	if description != nil {
		sets = append(sets, "description = ?")
		args = append(args, store.SanitizeSecureCLICredentialDescription(*description))
	}
	if pairingCode != nil {
		code := strings.TrimSpace(*pairingCode)
		if err := store.ValidateOrcaPairingCode(code); err != nil {
			return err
		}
		enc, err := s.encrypt(code)
		if err != nil {
			return err
		}
		sets = append(sets, "pairing_code = ?")
		args = append(args, enc)
	}
	args = append(args, id.String(), tid.String())
	res, err := s.db.ExecContext(ctx,
		"UPDATE orca_servers SET "+strings.Join(sets, ", ")+" WHERE id = ? AND tenant_id = ?", args...)
	if err != nil {
		return fmt.Errorf("update orca server: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrOrcaServerNotFound
	}
	return nil
}

func (s *SQLiteOrcaServerStore) Delete(ctx context.Context, id uuid.UUID) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return store.ErrOrcaServerNotFound
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM orca_servers WHERE id = ? AND tenant_id = ?`, id.String(), tid.String())
	if err != nil {
		return fmt.Errorf("delete orca server: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrOrcaServerNotFound
	}
	return nil
}
