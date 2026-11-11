package pg

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

var _ store.OrcaServerStore = (*PGOrcaServerStore)(nil)

type PGOrcaServerStore struct {
	db     *sql.DB
	encKey string
}

func NewPGOrcaServerStore(db *sql.DB, encryptionKey string) *PGOrcaServerStore {
	return &PGOrcaServerStore{db: db, encKey: encryptionKey}
}

const orcaServerCols = `id, tenant_id, label, description, pairing_code, last_status, last_runtime_state,
 last_error, last_checked_at, last_healthy_at, created_at, updated_at, created_by, agents, agent_health`

func (s *PGOrcaServerStore) encrypt(code string) ([]byte, error) {
	if s.encKey == "" {
		return []byte(code), nil
	}
	enc, err := crypto.Encrypt(code, s.encKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt orca pairing code: %w", err)
	}
	return []byte(enc), nil
}

func (s *PGOrcaServerStore) decrypt(raw []byte) (string, error) {
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

func (s *PGOrcaServerStore) scan(row orcaRowScanner, withSecret bool) (*store.OrcaServer, []byte, error) {
	var (
		o       store.OrcaServer
		code    []byte
		checked sql.NullTime
		healthy sql.NullTime

		agentsRaw, healthRaw []byte
	)
	if err := row.Scan(&o.ID, &o.TenantID, &o.Label, &o.Description, &code, &o.LastStatus, &o.LastRuntimeState,
		&o.LastError, &checked, &healthy, &o.CreatedAt, &o.UpdatedAt, &o.CreatedBy, &agentsRaw, &healthRaw); err != nil {
		return nil, nil, err
	}
	o.HasPairingCode = len(code) > 0
	if err := store.DecodeOrcaAgentColumns(agentsRaw, healthRaw, &o); err != nil {
		return nil, nil, err
	}
	if checked.Valid {
		o.LastCheckedAt = &checked.Time
	}
	if healthy.Valid {
		o.LastHealthyAt = &healthy.Time
	}
	if withSecret {
		plain, err := s.decrypt(code)
		if err != nil {
			return &o, code, err
		}
		o.PairingCode = plain
	}
	return &o, code, nil
}

func (s *PGOrcaServerStore) List(ctx context.Context) ([]store.OrcaServer, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+orcaServerCols+` FROM orca_servers WHERE tenant_id = $1 ORDER BY label`, tid)
	if err != nil {
		return nil, fmt.Errorf("list orca servers: %w", err)
	}
	defer rows.Close()
	var out []store.OrcaServer
	for rows.Next() {
		o, _, err := s.scan(rows, false)
		if err != nil {
			return nil, fmt.Errorf("scan orca server: %w", err)
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

func (s *PGOrcaServerStore) get(ctx context.Context, id uuid.UUID, withSecret bool) (*store.OrcaServer, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, store.ErrOrcaServerNotFound
	}
	o, _, err := s.scan(s.db.QueryRowContext(ctx,
		`SELECT `+orcaServerCols+` FROM orca_servers WHERE id = $1 AND tenant_id = $2`, id, tid), withSecret)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrOrcaServerNotFound
	}
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (s *PGOrcaServerStore) Get(ctx context.Context, id uuid.UUID) (*store.OrcaServer, error) {
	return s.get(ctx, id, false)
}

func (s *PGOrcaServerStore) GetWithSecret(ctx context.Context, id uuid.UUID) (*store.OrcaServer, error) {
	return s.get(ctx, id, true)
}

func (s *PGOrcaServerStore) Create(ctx context.Context, srv *store.OrcaServer) error {
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
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO orca_servers (id, tenant_id, label, description, pairing_code, created_at, updated_at, created_by)
		 SELECT $1::uuid, $2::uuid, $3::varchar, $4::text, $5::bytea, $6::timestamptz, $6::timestamptz, $7::varchar
		 WHERE (SELECT COUNT(*) FROM orca_servers WHERE tenant_id = $2::uuid) < $8`,
		srv.ID, tid, srv.Label, srv.Description, enc, now, srv.CreatedBy, store.OrcaServerMaxPerTenant)
	if err != nil {
		if strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key") {
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

func (s *PGOrcaServerStore) Update(ctx context.Context, id uuid.UUID, description *string, pairingCode *string) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return store.ErrOrcaServerNotFound
	}
	sets := []string{"updated_at = $1"}
	args := []any{time.Now().UTC()}
	if description != nil {
		args = append(args, store.SanitizeSecureCLICredentialDescription(*description))
		sets = append(sets, fmt.Sprintf("description = $%d", len(args)))
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
		args = append(args, enc)
		sets = append(sets, fmt.Sprintf("pairing_code = $%d", len(args)))
	}
	args = append(args, id, tid)
	res, err := s.db.ExecContext(ctx,
		fmt.Sprintf("UPDATE orca_servers SET %s WHERE id = $%d AND tenant_id = $%d",
			strings.Join(sets, ", "), len(args)-1, len(args)), args...)
	if err != nil {
		return fmt.Errorf("update orca server: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrOrcaServerNotFound
	}
	return nil
}

func (s *PGOrcaServerStore) Delete(ctx context.Context, id uuid.UUID) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return store.ErrOrcaServerNotFound
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM orca_servers WHERE id = $1 AND tenant_id = $2`, id, tid)
	if err != nil {
		return fmt.Errorf("delete orca server: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrOrcaServerNotFound
	}
	return nil
}
