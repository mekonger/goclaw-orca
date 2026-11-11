package orcaserver

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestRefresherUnknownAccountShapeWritesNothing(t *testing.T) {
	counter := installAccountOrca(t, `{"ok":true,"result":{"runtime":{"state":"ready"}}}`, `{"ok":true,"something":"else"}`)
	now := time.Now().UTC()
	srv := dueServer(map[string]store.OrcaAgentHealth{
		"codex-fast":  {Status: store.OrcaAgentStatusHealthy, CheckedAt: now},
		"claude-hard": {Status: store.OrcaAgentStatusUnhealthy, Error: "boom", CheckedAt: now},
	})
	s := &agentHealthStore{due: []store.OrcaServer{srv}}
	NewRefresher(s, NewChecker()).RunOnce(context.Background())
	if n := countCalls(counter); n != 1 {
		t.Fatalf("account list calls = %d, want 1", n)
	}
	if len(s.writes) != 0 {
		t.Fatalf("unknown shape must write nothing, got %v", s.writes)
	}
}
