package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readOrcaFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "orca", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestClassifyOrcaLaunchFixtures(t *testing.T) {
	cases := map[string]bool{
		"agent_launch_unknown_agent.json":         true,
		"worktree_create_unknown_agent.json":      true,
		"agent_launch_ok_chat.json":               false,
		"worktree_create_ok.json":                 false,
		"worktree_create_bad_model_accepted.json": false,
		"agent_launch_effort_without_model.json":  false,
	}
	for name, want := range cases {
		got, reason := classifyOrcaLaunch(readOrcaFixture(t, name), "nosuchagent")
		if got != want {
			t.Errorf("%s: unhealthy = %v", name, got)
		}
		if want && !strings.HasPrefix(reason, "Unknown TUI agent") {
			t.Errorf("%s: reason = %q", name, reason)
		}
	}
	if got, _ := classifyOrcaLaunch(readOrcaFixture(t, "agent_launch_unknown_agent.json"), "claude"); got {
		t.Error("error naming another agent must not mark this one unhealthy")
	}
	for _, raw := range []string{"", "not json", `{"ok":false}`, `{"ok":false,"error":{"code":"runtime_unavailable","message":"Unknown TUI agent \"nosuchagent\""}}`} {
		if got, _ := classifyOrcaLaunch(raw, "nosuchagent"); got {
			t.Errorf("%q classified unhealthy", raw)
		}
	}
}
