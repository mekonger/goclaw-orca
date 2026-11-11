package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	orcaRawStdoutMax      = 64 << 10
	orcaHealthErrMaxRunes = 200
)

type rawStdoutSinkKey struct{}

type rawStdoutSink struct{ out string }

func withRawStdoutSink(ctx context.Context) (context.Context, *rawStdoutSink) {
	s := &rawStdoutSink{}
	return context.WithValue(ctx, rawStdoutSinkKey{}, s), s
}

func captureRawStdout(ctx context.Context, stdout string) {
	if s, ok := ctx.Value(rawStdoutSinkKey{}).(*rawStdoutSink); ok {
		s.out = stdout[:min(len(stdout), orcaRawStdoutMax)]
	}
}

type orcaLaunchReply struct {
	OK    *bool `json:"ok"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func classifyOrcaLaunch(stdout, agent string) (unhealthy bool, reason string) {
	var r orcaLaunchReply
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &r); err != nil || r.OK == nil || *r.OK || r.Error == nil {
		return false, ""
	}
	if r.Error.Code == "invalid_argument" && r.Error.Message == "Unknown TUI agent "+strconv.Quote(agent) {
		return true, truncateRunes(store.SanitizeSecureCLICredentialDescription(r.Error.Message), orcaHealthErrMaxRunes)
	}
	return false, ""
}

func (t *ExecTool) recordOrcaLaunchHealth(ctx context.Context, route *orcaRoute, stdout string) {
	if route == nil || !route.HasEntry || t.orcaServers == nil {
		return
	}
	unhealthy, reason := classifyOrcaLaunch(stdout, route.Entry.Agent)
	if !unhealthy {
		return
	}
	h := store.OrcaAgentHealth{Status: store.OrcaAgentStatusUnhealthy, Error: reason, CheckedAt: time.Now().UTC()}
	if err := t.orcaServers.UpdateAgentHealth(ctx, route.ServerID, route.Entry, h); err != nil {
		slog.Warn("orca.route.health_write_failed", "server_id", route.ServerID, "key", route.Entry.Key, "error", err)
		return
	}
	slog.Info("orca.route.agent_unhealthy", "server_id", route.ServerID, "label", route.Label, "key", route.Entry.Key)
}
