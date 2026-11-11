package orcaserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type agentHealthStore struct {
	store.OrcaServerStore
	mu      sync.Mutex
	due     []store.OrcaServer
	writes  map[string]store.OrcaAgentHealth
	tenants []uuid.UUID
}

func (s *agentHealthStore) ListDueForCheck(context.Context, time.Time, int) ([]store.OrcaServer, error) {
	return s.due, nil
}

func (s *agentHealthStore) UpdateHealth(context.Context, uuid.UUID, store.OrcaServerHealth) error {
	return nil
}

func (s *agentHealthStore) UpdateAgentHealth(ctx context.Context, _ uuid.UUID, a store.OrcaAgentConfig, h store.OrcaAgentHealth) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return store.ErrOrcaServerNotFound
	}
	s.tenants = append(s.tenants, tid)
	if s.writes == nil {
		s.writes = map[string]store.OrcaAgentHealth{}
	}
	s.writes[a.Key] = h
	return nil
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func signedInFixture(t *testing.T) string {
	s := readFixture(t, "account_list_signed_out.json")
	s = strings.Replace(s, `"hasAuth": false`, `"hasAuth": true`, 1)
	return strings.Replace(s, `"claude": {
      "accounts": []`, `"claude": {
      "accounts": [{"id":"a1"}]`, 1)
}

func installAccountOrca(t *testing.T, statusJSON, accountJSON string) string {
	t.Helper()
	counter := filepath.Join(t.TempDir(), "account-calls")
	acc := filepath.Join(t.TempDir(), "accounts.json")
	if err := os.WriteFile(acc, []byte(accountJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	installFakeOrca(t, `case "$1 $2" in
"status --json") echo '`+statusJSON+`' ;;
"account list") echo x >> `+counter+`; cat `+acc+` ;;
esac
`)
	return counter
}

func countCalls(path string) int {
	b, _ := os.ReadFile(path)
	return strings.Count(string(b), "x")
}

func TestParseAccountListFixture(t *testing.T) {
	auth, ok := parseAccountList([]byte(readFixture(t, "account_list_signed_out.json")))
	if !ok || auth["codex"] != authSignedOut || auth["claude"] != authUnknown || auth["opencode"] != authUnknown {
		t.Fatalf("signed out: %v %v", auth, ok)
	}
	if _, has := auth["rateLimits"]; has {
		t.Fatal("rateLimits is not an agent")
	}
	auth, ok = parseAccountList([]byte(signedInFixture(t)))
	if !ok || auth["codex"] != authSignedIn || auth["claude"] != authSignedIn {
		t.Fatalf("signed in: %v", auth)
	}
	for _, raw := range []string{"", "nope", `{"ok":false,"result":{}}`, `{"result":{"codex":{}}}`} {
		if _, ok := parseAccountList([]byte(raw)); ok {
			t.Errorf("%q parsed", raw)
		}
	}
}

func TestNextAgentHealth(t *testing.T) {
	now := time.Now().UTC()
	codex := store.OrcaAgentConfig{Key: "c", Agent: "codex", Enabled: true}
	gemini := store.OrcaAgentConfig{Key: "g", Agent: "gemini", Enabled: true}
	unhealthyOld := store.OrcaAgentHealth{Status: store.OrcaAgentStatusUnhealthy, CheckedAt: now.Add(-31 * time.Minute)}
	unhealthyNew := store.OrcaAgentHealth{Status: store.OrcaAgentStatusUnhealthy, CheckedAt: now.Add(-time.Minute)}
	healthy := store.OrcaAgentHealth{Status: store.OrcaAgentStatusHealthy, CheckedAt: now.Add(-time.Minute)}
	in := map[string]agentAuth{"codex": authSignedIn}
	out := map[string]agentAuth{"codex": authSignedOut}
	cases := []struct {
		name          string
		cur           store.OrcaAgentHealth
		a             store.OrcaAgentConfig
		serverHealthy bool
		reset         bool
		auth          map[string]agentAuth
		want          string
		changed       bool
	}{
		{"server down resets healthy", healthy, codex, false, false, in, store.OrcaAgentStatusUnknown, true},
		{"server down unknown stays", store.OrcaAgentHealth{}, codex, false, false, in, store.OrcaAgentStatusUnknown, false},
		{"signed in -> healthy", store.OrcaAgentHealth{}, codex, true, false, in, store.OrcaAgentStatusHealthy, true},
		{"signed out -> unhealthy", healthy, codex, true, false, out, store.OrcaAgentStatusUnhealthy, true},
		{"agent absent unchanged", healthy, gemini, true, false, in, store.OrcaAgentStatusHealthy, false},
		{"no account data unchanged", healthy, codex, true, false, nil, store.OrcaAgentStatusHealthy, false},
		{"fresh unhealthy kept", unhealthyNew, gemini, true, false, nil, store.OrcaAgentStatusUnhealthy, false},
		{"old unhealthy reset", unhealthyOld, gemini, true, false, nil, store.OrcaAgentStatusUnknown, true},
		{"check resets fresh unhealthy", unhealthyNew, gemini, true, true, nil, store.OrcaAgentStatusUnknown, true},
		{"reset then signed out again", unhealthyOld, codex, true, true, out, store.OrcaAgentStatusUnhealthy, true},
		{"fresh launch failure kept when signed in", store.OrcaAgentHealth{Status: store.OrcaAgentStatusUnhealthy, Error: `Unknown TUI agent "codex"`, CheckedAt: now.Add(-5 * time.Minute)},
			codex, true, false, in, store.OrcaAgentStatusUnhealthy, false},
		{"not signed in recovers on sign in", store.OrcaAgentHealth{Status: store.OrcaAgentStatusUnhealthy, Error: notSignedInError, CheckedAt: now.Add(-time.Minute)},
			codex, true, false, in, store.OrcaAgentStatusHealthy, true},
		{"old launch failure reset then healthy", unhealthyOld, codex, true, false, in, store.OrcaAgentStatusHealthy, true},
		{"disabled entry ignores auth", store.OrcaAgentHealth{}, store.OrcaAgentConfig{Key: "c", Agent: "codex"}, true, false, out, store.OrcaAgentStatusUnknown, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, changed := nextAgentHealth(tc.cur, tc.a, tc.serverHealthy, tc.reset, tc.auth, now)
			if next.Status != tc.want || changed != tc.changed {
				t.Fatalf("got %s changed=%v, want %s changed=%v", next.Status, changed, tc.want, tc.changed)
			}
		})
	}
}

func dueServer(health map[string]store.OrcaAgentHealth) store.OrcaServer {
	return store.OrcaServer{ID: uuid.New(), TenantID: uuid.New(), Label: "vm", PairingCode: testCode, AgentHealth: health,
		Agents: []store.OrcaAgentConfig{
			{Key: "codex-fast", Agent: "codex", Tier: store.OrcaAgentTierRoutine, Enabled: true, Fallback: true},
			{Key: "claude-hard", Agent: "claude", Tier: store.OrcaAgentTierHard, Enabled: true},
		}}
}

func TestRefresherUnhealthyServerSkipsAccountList(t *testing.T) {
	counter := installAccountOrca(t, `{"result":{"runtime":{"state":"connecting"}}}`, signedInFixture(t))
	srv := dueServer(map[string]store.OrcaAgentHealth{"codex-fast": {Status: store.OrcaAgentStatusHealthy}})
	s := &agentHealthStore{due: []store.OrcaServer{srv}}
	NewRefresher(s, NewChecker()).RunOnce(context.Background())
	if n := countCalls(counter); n != 0 {
		t.Fatalf("account list called %d times", n)
	}
	if s.writes["codex-fast"].Status != store.OrcaAgentStatusUnknown || len(s.writes) != 1 {
		t.Fatalf("writes = %v", s.writes)
	}
}

func TestRefresherSignedInMarksHealthyWithTenantCtx(t *testing.T) {
	counter := installAccountOrca(t, `{"ok":true,"result":{"runtime":{"state":"ready"}}}`, signedInFixture(t))
	srv := dueServer(nil)
	s := &agentHealthStore{due: []store.OrcaServer{srv}}
	NewRefresher(s, NewChecker()).RunOnce(context.Background())
	if countCalls(counter) != 1 {
		t.Fatalf("account list calls = %d", countCalls(counter))
	}
	for _, k := range []string{"codex-fast", "claude-hard"} {
		if h := s.writes[k]; h.Status != store.OrcaAgentStatusHealthy || h.LastOKAt == nil {
			t.Fatalf("%s = %+v", k, h)
		}
	}
	for _, tid := range s.tenants {
		if tid != srv.TenantID {
			t.Fatalf("write under tenant %s, want %s", tid, srv.TenantID)
		}
	}
}

func TestRefresherSignedOutCodexOnly(t *testing.T) {
	installAccountOrca(t, `{"ok":true,"result":{"runtime":{"state":"ready"}}}`, readFixture(t, "account_list_signed_out.json"))
	s := &agentHealthStore{due: []store.OrcaServer{dueServer(nil)}}
	NewRefresher(s, NewChecker()).RunOnce(context.Background())
	if h := s.writes["codex-fast"]; h.Status != store.OrcaAgentStatusUnhealthy || h.Error != notSignedInError {
		t.Fatalf("codex = %+v", h)
	}
	if _, ok := s.writes["claude-hard"]; ok {
		t.Fatal("claude without systemDefault must not be downgraded on empty accounts")
	}
}

func TestCheckResetsUnhealthyAndUnknownShapeChangesNothingElse(t *testing.T) {
	installAccountOrca(t, `{"ok":true,"result":{"runtime":{"state":"ready"}}}`, `{"ok":true,"something":"else"}`)
	srv := dueServer(map[string]store.OrcaAgentHealth{
		"codex-fast":  {Status: store.OrcaAgentStatusUnhealthy, CheckedAt: time.Now().UTC()},
		"claude-hard": {Status: store.OrcaAgentStatusHealthy, CheckedAt: time.Now().UTC()},
	})
	s := &agentHealthStore{}
	if err := CheckAndStore(context.Background(), s, NewChecker(), &srv, true); err != nil {
		t.Fatal(err)
	}
	if s.writes["codex-fast"].Status != store.OrcaAgentStatusUnknown {
		t.Fatalf("check must reset unhealthy: %v", s.writes)
	}
	if _, ok := s.writes["claude-hard"]; ok {
		t.Fatal("unknown account shape must not change healthy agents")
	}
}
