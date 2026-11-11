package store

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

const (
	SecureCLICredentialLabelMaxLen       = 32
	SecureCLICredentialDescriptionMaxLen = 200
	SecureCLIDefaultLabelAlias           = "default"
	SecureCLISourceOrcaServer            = "orca_server"
	OrcaPairingCodeEnvVar                = "ORCA_PAIRING_CODE"
)

var secureCLICredentialLabelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type SecureCLICredentialLabel struct {
	BinaryID    uuid.UUID `json:"binary_id"`
	Label       string    `json:"label"`
	Description string    `json:"description"`
	Source      string    `json:"source"`
}

type SecureCLICredentialLabelQuery struct {
	BinaryID *uuid.UUID
	AgentID  *uuid.UUID
	UserID   string
}

type SecureCLIResolvedCredential struct {
	Label          string  `json:"label"`
	Source         string  `json:"source"`
	SubjectID      string  `json:"subject_id"`
	Env            []byte  `json:"-"`
	CredentialType *string `json:"credential_type,omitempty"`
	HostScope      *string `json:"host_scope,omitempty"`
}

type SecureCLICredentialWrite struct {
	Label          string
	Description    string
	Env            []byte
	CredentialType *string
	HostScope      *string
}

type SecureCLILabeledCredentialStore interface {
	ListCredentialLabels(ctx context.Context, q SecureCLICredentialLabelQuery) ([]SecureCLICredentialLabel, error)
	ResolveLabeledCredential(ctx context.Context, binaryID uuid.UUID, agentID *uuid.UUID, userID, label string) (*SecureCLIResolvedCredential, error)
	GetUserCredentialByLabel(ctx context.Context, binaryID uuid.UUID, userID, label string) (*SecureCLIUserCredential, error)
	SetUserCredentialLabeled(ctx context.Context, binaryID uuid.UUID, userID string, w SecureCLICredentialWrite) error
	DeleteUserCredentialByLabel(ctx context.Context, binaryID uuid.UUID, userID, label string) error
	GetAgentCredentialByLabel(ctx context.Context, binaryID, agentID uuid.UUID, label string) (*SecureCLIAgentCredential, error)
	SetAgentCredentialLabeled(ctx context.Context, binaryID, agentID uuid.UUID, w SecureCLICredentialWrite, createdBy string) error
	DeleteAgentCredentialByLabel(ctx context.Context, binaryID, agentID uuid.UUID, label string) error
}

func NormalizeSecureCLICredentialLabel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == SecureCLIDefaultLabelAlias {
		return ""
	}
	return s
}

func ValidateSecureCLICredentialLabel(label string) error {
	if label == SecureCLIDefaultLabelAlias {
		return fmt.Errorf("label %q is reserved", label)
	}
	if !secureCLICredentialLabelPattern.MatchString(label) {
		return fmt.Errorf("label must match %s", secureCLICredentialLabelPattern.String())
	}
	return nil
}

func PrepareSecureCLICredentialLabel(raw string) (string, error) {
	label := NormalizeSecureCLICredentialLabel(raw)
	if label == "" {
		return "", nil
	}
	if err := ValidateSecureCLICredentialLabel(label); err != nil {
		return "", err
	}
	return label, nil
}

func SanitizeSecureCLICredentialDescription(s string) string {
	return sanitizeDescription(s, SecureCLICredentialDescriptionMaxLen)
}

func sanitizeDescription(s string, maxLen int) string {
	var sb strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) {
			sb.WriteRune(' ')
			continue
		}
		sb.WriteRune(r)
	}
	out := strings.TrimSpace(sb.String())
	runes := []rune(out)
	if len(runes) > maxLen {
		out = strings.TrimSpace(string(runes[:maxLen]))
	}
	return out
}

func secureCLILabelSourceRank(source string) int {
	switch source {
	case "user":
		return 0
	case "agent":
		return 1
	default:
		return 2
	}
}

func OrcaServerCredentialEnv(pairingCode string) ([]byte, error) {
	return json.Marshal(map[string]string{OrcaPairingCodeEnvVar: pairingCode})
}

func MergeSecureCLICredentialLabels(rows []SecureCLICredentialLabel) []SecureCLICredentialLabel {
	type key struct {
		binary uuid.UUID
		label  string
	}
	idx := make(map[key]int, len(rows))
	out := make([]SecureCLICredentialLabel, 0, len(rows))
	for _, r := range rows {
		k := key{r.BinaryID, r.Label}
		i, seen := idx[k]
		if !seen {
			idx[k] = len(out)
			out = append(out, r)
			continue
		}
		if secureCLILabelSourceRank(r.Source) < secureCLILabelSourceRank(out[i].Source) {
			out[i] = r
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].BinaryID != out[j].BinaryID {
			return out[i].BinaryID.String() < out[j].BinaryID.String()
		}
		return out[i].Label < out[j].Label
	})
	return out
}

func (b *SecureCLIBinary) ApplyLabeledCredential(c *SecureCLIResolvedCredential) {
	if b == nil || c == nil {
		return
	}
	if b.CredentialSource == "context" && (b.CredentialType == nil || *b.CredentialType == "") && len(b.CredentialEnv) > 0 {
		b.EncryptedEnv = b.CredentialEnv
	}
	b.UserEnv = nil
	b.UserCredentialType = nil
	b.UserHostScope = nil
	b.CredentialSubjectID = ""
	b.SetEffectiveCredential(c.Env, c.CredentialType, c.HostScope, c.Source, c.SubjectID)
}
