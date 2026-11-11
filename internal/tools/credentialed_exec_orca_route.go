package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/jev"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const orcaAutoAgent = "auto"

type orcaProviderLister interface {
	ListProviders(ctx context.Context) ([]store.LLMProviderData, error)
}

type orcaRoute struct {
	ServerID uuid.UUID
	Label    string
	Entry    store.OrcaAgentConfig
	HasEntry bool
	Note     string
}

func (t *ExecTool) SetOrcaRouting(servers store.OrcaServerStore, providers store.ProviderStore) {
	t.orcaServers = servers
	if providers != nil {
		t.orcaProviders = providers
	}
}

func (t *ExecTool) InheritOrcaRouting(parent *ExecTool) {
	if t == nil || parent == nil || t == parent {
		return
	}
	t.orcaServers = parent.orcaServers
	t.orcaProviders = parent.orcaProviders
}

func findFlagValue(args []string, flag string) (value string, idx int, inline bool) {
	for i, a := range args {
		if a == "--" {
			break
		}
		if a == flag {
			if i+1 < len(args) {
				return args[i+1], i, false
			}
			return "", i, false
		}
		if v, ok := strings.CutPrefix(a, flag+"="); ok {
			return v, i, true
		}
	}
	return "", -1, false
}

func countFlag(args []string, flag string) int {
	n := 0
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == flag || strings.HasPrefix(a, flag+"=") {
			n++
		}
	}
	return n
}

func isOrcaBinary(binary string) bool {
	return strings.TrimSuffix(filepath.Base(binary), ".exe") == "orca"
}

func orcaLaunchCommand(binary string, args []string) string {
	if !isOrcaBinary(binary) || len(args) < 2 {
		return ""
	}
	switch cmd := args[0] + " " + args[1]; cmd {
	case "worktree create", "agent launch":
		return cmd
	}
	return ""
}

func rewriteOrcaAgentArgs(args []string, idx int, inline bool, agent, model string) []string {
	out := make([]string, 0, len(args)+2)
	out = append(out, args[:idx]...)
	if inline {
		out = append(out, "--agent="+agent)
	} else {
		out = append(out, "--agent", agent)
	}
	if model != "" {
		out = append(out, "--model", model)
	}
	next := idx + 1
	if !inline {
		next = idx + 2
	}
	return append(out, args[min(next, len(args)):]...)
}

func orcaRouteError(ctx context.Context, key string, args ...any) *Result {
	return ErrorResult("[CREDENTIALED EXEC] " + i18n.T(store.LocaleFromContext(ctx), key, args...))
}

func (t *ExecTool) orcaServerFor(ctx context.Context, cred *store.SecureCLIBinary) (*store.OrcaServer, error) {
	if t.orcaServers == nil || cred == nil || cred.CredentialSource != store.SecureCLISourceOrcaServer {
		return nil, nil
	}
	id, err := uuid.Parse(cred.CredentialSubjectID)
	if err != nil {
		return nil, nil
	}
	return t.orcaServers.Get(ctx, id)
}

func (t *ExecTool) routeOrcaAgent(ctx context.Context, cred *store.SecureCLIBinary, binary string, args []string) (*orcaRoute, []string, *Result) {
	command := orcaLaunchCommand(binary, args)
	if command == "" {
		if v, i, _ := findFlagValue(args, "--agent"); isOrcaBinary(binary) && i >= 0 && v == orcaAutoAgent {
			return nil, args, orcaRouteError(ctx, i18n.MsgOrcaAutoUnroutable)
		}
		return nil, args, nil
	}
	agent, idx, inline := findFlagValue(args, "--agent")
	if idx < 0 || agent == "" {
		return nil, args, nil
	}
	for _, f := range []string{"--agent", "--model", "--effort"} {
		if countFlag(args, f) > 1 {
			return nil, args, orcaRouteError(ctx, i18n.MsgOrcaRepeatedFlag, f)
		}
	}
	if agent != orcaAutoAgent {
		return t.explicitOrcaRoute(ctx, cred, args, agent), args, nil
	}
	if _, i, _ := findFlagValue(args, "--model"); i >= 0 {
		return nil, args, orcaRouteError(ctx, i18n.MsgOrcaAutoWithModel)
	}
	if _, i, _ := findFlagValue(args, "--effort"); i >= 0 {
		return nil, args, orcaRouteError(ctx, i18n.MsgOrcaAutoWithModel)
	}
	srv, err := t.orcaServerFor(ctx, cred)
	if errors.Is(err, store.ErrOrcaServerNotFound) || (err == nil && srv == nil) {
		return nil, args, orcaRouteError(ctx, i18n.MsgOrcaAutoUnroutable)
	}
	if err != nil {
		slog.Warn("orca.route.server_load_failed", "server_id", cred.CredentialSubjectID, "error", err)
		return nil, args, orcaRouteError(ctx, i18n.MsgOrcaAgentsLoadFailed, cred.CredentialSubjectID)
	}
	prompt, _, _ := findFlagValue(args, "--prompt")
	pick, failure := pickOrcaAgent(ctx, srv.Agents, srv.AgentHealth, command, prompt, func() jevChooser {
		if t.orcaProviders == nil {
			return nil
		}
		if c, ok := jev.Resolve(ctx, t.orcaProviders); ok {
			return c
		}
		return nil
	})
	switch failure {
	case orcaPickNoAgents:
		return nil, args, orcaRouteError(ctx, i18n.MsgOrcaNoAgents, srv.Label)
	case orcaPickNoPlanning:
		return nil, args, orcaRouteError(ctx, i18n.MsgOrcaNoPlanningAgent, srv.Label)
	case orcaPickNoUsable:
		return nil, args, orcaRouteError(ctx, i18n.MsgOrcaNoUsableAgent, srv.Label, describeOrcaAgents(srv))
	}
	route := &orcaRoute{ServerID: srv.ID, Label: srv.Label, Entry: pick.Entry, HasEntry: true}
	route.Note = "routed: --agent " + pick.Entry.Agent
	if pick.Entry.Model != "" {
		route.Note += " --model " + pick.Entry.Model
	}
	route.Note += " (" + pick.Reason + ")"
	logAttrs := []any{"server_id", srv.ID, "label", srv.Label, "key", pick.Entry.Key, "agent", pick.Entry.Agent,
		"model", pick.Entry.Model, "reason", pick.Reason}
	if pick.Jev != nil {
		logAttrs = append(logAttrs, "jev_choice", pick.Jev.Choice, "jev_confidence", pick.Jev.Confidence,
			"jev_cost", pick.Jev.Cost, "jev_model", pick.Jev.Model)
	}
	slog.Info("orca.route", logAttrs...)
	return route, rewriteOrcaAgentArgs(args, idx, inline, pick.Entry.Agent, pick.Entry.Model), nil
}

func (t *ExecTool) explicitOrcaRoute(ctx context.Context, cred *store.SecureCLIBinary, args []string, agent string) *orcaRoute {
	srv, err := t.orcaServerFor(ctx, cred)
	if err != nil || srv == nil {
		return nil
	}
	model, _, _ := findFlagValue(args, "--model")
	route := &orcaRoute{ServerID: srv.ID, Label: srv.Label}
	configured, matches := false, 0
	for _, a := range srv.Agents {
		if !a.Enabled || a.Agent != agent {
			continue
		}
		configured = true
		if a.Model == model {
			matches++
			route.Entry = a
		}
	}
	route.HasEntry = matches == 1
	if !configured {
		route.Note = i18n.T(store.LocaleFromContext(ctx), i18n.MsgOrcaAgentNotConfigured, agent, srv.Label)
	}
	if !route.HasEntry && route.Note == "" {
		return nil
	}
	return route
}

func describeOrcaAgents(srv *store.OrcaServer) string {
	parts := make([]string, 0, len(srv.Agents))
	for _, a := range srv.Agents {
		state := orcaAgentStatus(srv.AgentHealth, a.Key)
		if !a.Enabled {
			state = "disabled"
		}
		parts = append(parts, fmt.Sprintf("%s (%s %s, %s, %s)", a.Key, a.Agent, a.Model, a.Tier, state))
	}
	return strings.Join(parts, "; ")
}
