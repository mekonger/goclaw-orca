package i18n

import (
	"strings"
	"testing"
)

func TestI18nCatalogs_HasSecureCLILabelKeys(t *testing.T) {
	keys := []string{MsgSecureCLILabelInvalid, MsgSecureCLILabelLimit, MsgSecureCLIDescriptionTooLong}
	for _, locale := range []string{LocaleEN, LocaleVI, LocaleZH, LocaleKO, LocaleRU} {
		for _, key := range keys {
			msg := lookup(locale, key)
			if msg == key || strings.TrimSpace(msg) == "" {
				t.Errorf("locale=%s key=%s missing translation", locale, key)
			}
		}
	}
}

func TestI18nCatalogs_HasOrcaAgentKeys(t *testing.T) {
	keys := []string{MsgOrcaAgentsTooMany, MsgOrcaAgentInvalidField, MsgOrcaAgentDuplicateKey,
		MsgOrcaAgentInvalidTier, MsgOrcaAgentFallbackRule, MsgOrcaAgentPlanningLimit,
		MsgOrcaAutoUnroutable, MsgOrcaAutoWithModel, MsgOrcaNoAgents, MsgOrcaNoPlanningAgent,
		MsgOrcaNoUsableAgent, MsgOrcaAgentNotConfigured, MsgOrcaAgentsLoadFailed, MsgOrcaRepeatedFlag}
	for _, locale := range []string{LocaleEN, LocaleVI, LocaleZH, LocaleKO, LocaleRU} {
		for _, key := range keys {
			msg := lookup(locale, key)
			if msg == key || strings.TrimSpace(msg) == "" {
				t.Errorf("locale=%s key=%s missing translation", locale, key)
			}
		}
	}
}
