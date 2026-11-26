package acp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	modelprovider "github.com/wins/jaz/backend/internal/provider"
)

func (m *Manager) openCodeTestEnv(t *testing.T, cfg AgentConfig) (map[string]string, error) {
	t.Helper()
	env, err := m.processEnvPrepared(AgentOpenCode, cfg)
	if err != nil {
		return nil, err
	}
	cleanup, err := m.configureOpenCode(env, cfg, "")
	t.Cleanup(cleanup)
	return env, err
}

func TestOpenCodeFailedLaunchRemovesOnlyItsOwnInstructions(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(nil, Config{Root: root}, nil)
	cfg := AgentConfig{Command: filepath.Join(root, "missing-agent")}
	env, err := manager.processEnvPrepared(AgentOpenCode, cfg)
	if err != nil {
		t.Fatal(err)
	}
	profile := env["OPENCODE_CONFIG_DIR"]
	legacy := filepath.Join(profile, "jaz-instructions.md")
	if err := os.WriteFile(legacy, []byte("another process"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.openConn(context.Background(), AgentOpenCode, cfg, env, root, "private instructions"); err == nil {
		t.Fatal("missing executable started")
	}
	files, err := os.ReadDir(profile)
	if err != nil || len(files) != 1 || files[0].Name() != "jaz-instructions.md" {
		t.Fatalf("files after failed launch = %v, %v", files, err)
	}
	data, err := os.ReadFile(legacy)
	if err != nil || string(data) != "another process" {
		t.Fatalf("another process's file changed: %q, %v", data, err)
	}
}

func TestOpenCodeExplicitConfigKeepsItsInstructions(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(nil, Config{Root: root}, nil)
	custom := `{"instructions":["user.md"],"model":"native/model"}`
	cfg := AgentConfig{Env: map[string]string{"OPENCODE_CONFIG_CONTENT": custom}}
	env, err := manager.processEnvPrepared(AgentOpenCode, cfg)
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := manager.configureOpenCode(env, cfg, "Jaz instructions")
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if env["OPENCODE_CONFIG_CONTENT"] != custom {
		t.Fatalf("explicit configuration changed: %s", env["OPENCODE_CONFIG_CONTENT"])
	}
	files, err := os.ReadDir(env["OPENCODE_CONFIG_DIR"])
	if err != nil || len(files) != 0 {
		t.Fatalf("explicit configuration created unused files: %v, %v", files, err)
	}
}

// OpenCode chooses a reasoning variant by name, so a model's thinking-off level
// has to reach its configuration as "none"; dropping it would leave the session
// on whatever thinking level the model defaults to.
func TestOpenCodeWritesThinkingOffVariant(t *testing.T) {
	manager := NewManager(nil, Config{Root: t.TempDir()}, nil)
	cfg := AgentConfig{
		ProviderMode:    AgentProviderModeAgentDefaults,
		ModelProvider:   modelprovider.ProviderOpenRouter,
		Model:           "z-ai/glm-5.2",
		ReasoningEffort: "none",
	}
	env, err := manager.openCodeTestEnv(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var content struct {
		Provider map[string]struct {
			Models map[string]struct {
				Variants map[string]struct {
					Reasoning struct {
						Effort string `json:"effort"`
					} `json:"reasoning"`
				} `json:"variants"`
			} `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &content); err != nil {
		t.Fatal(err)
	}
	effort := content.Provider["openrouter"].Models["z-ai/glm-5.2"].Variants["none"].Reasoning.Effort
	if effort != "none" {
		t.Fatalf("variants = %#v, want a none variant", content.Provider["openrouter"].Models["z-ai/glm-5.2"].Variants)
	}
}
