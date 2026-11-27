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

// OpenCode takes a model's thinking levels from the variants in its own
// configuration, so a custom OpenAI-compatible provider needs the Jaz-selected
// effort written there too; limiting variants to OpenRouter left among the
// agent's levels and the turn ran on the model default while the thread showed
// the chosen one.
func TestOpenCodeWritesCustomProviderVariant(t *testing.T) {
	providers := map[string]modelprovider.ModelProviderConfig{
		"commandcode": {
			Type:         "openai-compatible",
			BaseURL:      "https://commandcode.example/v1",
			APIKey:       "commandcode-key",
			Capabilities: []string{modelprovider.CapabilityChatCompletions},
		},
	}
	manager := NewManager(nil, Config{Root: t.TempDir(), Providers: providers}, nil)
	cfg := AgentConfig{
		ProviderMode:    AgentProviderModeAgentDefaults,
		ModelProvider:   "commandcode",
		Model:           "deepseek/deepseek-v4.1-flash",
		ReasoningEffort: "medium",
	}
	env, err := manager.openCodeTestEnv(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var content openCodeConfigContent
	if err := json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &content); err != nil {
		t.Fatal(err)
	}
	variant, ok := content.Provider["commandcode"].Models["deepseek/deepseek-v4.1-flash"].Variants["medium"]
	if !ok || variant.Reasoning == nil || variant.Reasoning.Effort != "medium" {
		t.Fatalf("custom provider variants = %#v, want a medium variant", content.Provider["commandcode"].Models)
	}
}

// A model entry Jaz did not describe belongs to the user's own OpenCode
// configuration, so Jaz adds no variant to it.
func TestOpenCodeLeavesUndescribedProviderAlone(t *testing.T) {
	content := openCodeConfigContent{Provider: map[string]openCodeProviderConfig{
		"mine": {Models: map[string]openCodeModelConfig{"other": {}}},
	}}
	addOpenCodeReasoningVariant(&content, "mine/chat", "high")
	if len(content.Provider["mine"].Models["chat"].Variants) != 0 {
		t.Fatalf("variant added to a model Jaz does not describe: %#v", content.Provider["mine"].Models)
	}
}
