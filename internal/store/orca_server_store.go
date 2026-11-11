package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const (
	OrcaServerMaxPerTenant     = 20
	OrcaPairingCodeMaxLen      = 2048
	OrcaPairingCodePrefix      = "orca://"
	OrcaServerStatusUnknown    = "unknown"
	OrcaServerStatusHealthy    = "healthy"
	OrcaServerStatusUnhealthy  = "unhealthy"
	OrcaServerStatusError      = "error"
	OrcaServerStatusCLIMissing = "cli_missing"
)

var (
	ErrOrcaServerNotFound   = errors.New("orca server not found")
	ErrOrcaServerLabelTaken = errors.New("orca server label already exists")
	ErrOrcaServerLimit      = errors.New("orca server limit reached")
)

type OrcaServer struct {
	ID               uuid.UUID  `json:"id"`
	TenantID         uuid.UUID  `json:"tenant_id"`
	Label            string     `json:"label"`
	Description      string     `json:"description"`
	PairingCode      string     `json:"-"`
	HasPairingCode   bool       `json:"has_pairing_code"`
	LastStatus       string     `json:"last_status"`
	LastRuntimeState string     `json:"last_runtime_state"`
	LastError        string     `json:"last_error"`
	LastCheckedAt    *time.Time `json:"last_checked_at"`
	LastHealthyAt    *time.Time `json:"last_healthy_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	CreatedBy        string     `json:"created_by"`

	Agents      []OrcaAgentConfig          `json:"agents"`
	AgentHealth map[string]OrcaAgentHealth `json:"agent_health"`
}

type OrcaServerHealth struct {
	Status       string
	RuntimeState string
	Error        string
	CheckedAt    time.Time
	Healthy      bool
}

type OrcaServerStore interface {
	List(ctx context.Context) ([]OrcaServer, error)
	Get(ctx context.Context, id uuid.UUID) (*OrcaServer, error)
	GetWithSecret(ctx context.Context, id uuid.UUID) (*OrcaServer, error)
	Create(ctx context.Context, srv *OrcaServer) error
	Update(ctx context.Context, id uuid.UUID, description *string, pairingCode *string) error
	Delete(ctx context.Context, id uuid.UUID) error
	UpdateHealth(ctx context.Context, id uuid.UUID, h OrcaServerHealth) error
	ListDueForCheck(ctx context.Context, olderThan time.Time, limit int) ([]OrcaServer, error)
	UpdateAgents(ctx context.Context, id uuid.UUID, agents []OrcaAgentConfig) error
	UpdateAgentHealth(ctx context.Context, id uuid.UUID, a OrcaAgentConfig, h OrcaAgentHealth) error
}

func DecodeOrcaAgentColumns(agentsRaw, healthRaw []byte, srv *OrcaServer) error {
	srv.Agents = []OrcaAgentConfig{}
	srv.AgentHealth = map[string]OrcaAgentHealth{}
	if len(agentsRaw) > 0 {
		if err := json.Unmarshal(agentsRaw, &srv.Agents); err != nil {
			return fmt.Errorf("decode orca agents: %w", err)
		}
	}
	if len(healthRaw) > 0 {
		if err := json.Unmarshal(healthRaw, &srv.AgentHealth); err != nil {
			return fmt.Errorf("decode orca agent health: %w", err)
		}
	}
	if srv.Agents == nil {
		srv.Agents = []OrcaAgentConfig{}
	}
	if srv.AgentHealth == nil {
		srv.AgentHealth = map[string]OrcaAgentHealth{}
	}
	return nil
}

func ValidateOrcaPairingCode(s string) error {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, OrcaPairingCodePrefix) || len(s) == len(OrcaPairingCodePrefix) {
		return fmt.Errorf("pairing code must start with %s", OrcaPairingCodePrefix)
	}
	if len(s) > OrcaPairingCodeMaxLen {
		return fmt.Errorf("pairing code exceeds %d characters", OrcaPairingCodeMaxLen)
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return errors.New("pairing code must not contain whitespace or control characters")
		}
	}
	return nil
}

func PrepareOrcaServerInput(srv *OrcaServer) error {
	label := NormalizeSecureCLICredentialLabel(srv.Label)
	if label == "" {
		return errors.New("label is required")
	}
	if err := ValidateSecureCLICredentialLabel(label); err != nil {
		return err
	}
	srv.PairingCode = strings.TrimSpace(srv.PairingCode)
	if err := ValidateOrcaPairingCode(srv.PairingCode); err != nil {
		return err
	}
	srv.Label = label
	srv.Description = SanitizeSecureCLICredentialDescription(srv.Description)
	return nil
}
