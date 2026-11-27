package acp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wins/jaz/backend/internal/acp"
)

// TestPiIsACompactCapableBuiltinAgent pins how pi appears to the rest of Jaz:
// selectable, offering pi's own thinking levels, and compactable through pi's
// own compaction rather than a Jaz-written summary.
func TestPiIsACompactCapableBuiltinAgent(t *testing.T) {
	catalog := acp.BuiltinAgents()
	cfg, ok := catalog.Agent(acp.AgentPi)
	if !ok || !cfg.BuiltinBridge {
		t.Fatalf("pi = %#v, %v", cfg, ok)
	}
	if !acp.AgentSupportsCompact(acp.AgentPi) {
		t.Fatal("pi compacts through its own compact command")
	}
	names := acp.SelectableAgentNames(catalog.Names())
	if names[len(names)-1] != acp.AgentPi {
		t.Fatalf("selectable agents = %v", names)
	}
	options := acp.AgentOptionsForConfig(acp.AgentPi, cfg, t.TempDir())
	levels := make([]string, 0, len(options.ReasoningEfforts))
	for _, option := range options.ReasoningEfforts {
		levels = append(levels, option.Value)
	}
	want := []string{"", "off", "minimal", "low", "medium", "high", "xhigh", "max"}
	if len(levels) != len(want) {
		t.Fatalf("pi thinking levels = %v", levels)
	}
	for i := range want {
		if levels[i] != want[i] {
			t.Fatalf("pi thinking levels = %v", levels)
		}
	}
}

// TestPiProfileStaysInsideJaz checks that a pi thread's process is pointed at
// Jaz's own pi profile rather than the user's ~/.pi.
func TestPiProfileStaysInsideJaz(t *testing.T) {
	root := t.TempDir()
	layout, err := acp.PreparePiProfile(root)
	if err != nil {
		t.Fatal(err)
	}
	if layout != filepath.Join(root, "acp", "pi") {
		t.Fatalf("pi profile = %q", layout)
	}
	if info, err := os.Stat(layout); err != nil || !info.IsDir() {
		t.Fatalf("pi profile must exist: %v", err)
	}
}

// TestPiThreadsRunInJazsOwnProfile checks the wiring a real thread depends on:
// pi's process environment names Jaz's profile, not the user's ~/.pi.
func TestPiThreadsRunInJazsOwnProfile(t *testing.T) {
	store, root := newTestStore(t)
	manager := newFakeNamedAgentManagerWithOptions(t, store, root, acp.AgentPi, map[string]string{}, "", "")
	env, err := manager.ProbePiEnv(acp.AgentPi)
	if err != nil {
		t.Fatal(err)
	}
	if env["PI_CODING_AGENT_DIR"] != filepath.Join(root, "acp", "pi") {
		t.Fatalf("PI_CODING_AGENT_DIR = %q", env["PI_CODING_AGENT_DIR"])
	}
	if info, err := os.Stat(env["PI_CODING_AGENT_DIR"]); err != nil || !info.IsDir() {
		t.Fatalf("pi profile must be prepared: %v", err)
	}
}
