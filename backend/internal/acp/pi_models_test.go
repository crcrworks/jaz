package acp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wins/jaz/backend/internal/acp"
	"github.com/wins/jaz/backend/internal/provider"
)

// clearProviderEnv drops the provider variables host shells export, so a test
// sees only the keys it configures itself.
func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"OPENAI_API_KEY", "OPENROUTER_API_KEY", "JAZ_PROVIDER_COMMANDCODE_API_KEY"} {
		t.Setenv(key, "")
	}
}

// piProviderServer is the OpenAI-compatible provider a test profile points at:
// it advertises two models and requires the key Jaz binds.
func piProviderServer(t *testing.T, key string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if key != "" && r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"id": "deepseek/deepseek-v4.1-flash", "name": "DeepSeek V4.1 Flash", "context_length": 1000000},
			{"id": "gpt-6-luna", "name": "GPT-6 Luna", "context_length": 1050000},
		}})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestPreparePiProfileSuppliesModelProviders(t *testing.T) {
	clearProviderEnv(t)
	root := t.TempDir()
	const key = "provider-secret-key"
	server := piProviderServer(t, key)
	env := map[string]string{}
	providers := map[string]provider.ModelProviderConfig{
		"commandcode": {
			Type:         "openai-compatible",
			Label:        "CommandCode",
			BaseURL:      server.URL + "/v1",
			APIKey:       key,
			DefaultModel: "deepseek/deepseek-v4.1-flash",
		},
	}
	supplied, err := acp.PreparePiProfile(root, env, providers, "commandcode/gpt-6-luna")
	if err != nil {
		t.Fatal(err)
	}
	if len(supplied) != 1 || supplied[0] != "commandcode" {
		t.Fatalf("supplied = %v", supplied)
	}
	if env["JAZ_PROVIDER_COMMANDCODE_API_KEY"] != key {
		t.Fatalf("provider key not bound into pi's env: %v", env)
	}
	data, err := os.ReadFile(filepath.Join(root, "acp", "pi", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), key) {
		t.Fatalf("models.json holds the secret: %s", data)
	}
	var file struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			API     string `json:"api"`
			APIKey  string `json:"apiKey"`
			Models  []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	got := file.Providers["commandcode"]
	if got.BaseURL != server.URL+"/v1" || got.API != "openai-completions" || got.APIKey != "$JAZ_PROVIDER_COMMANDCODE_API_KEY" {
		t.Fatalf("commandcode = %+v", got)
	}
	ids := make([]string, 0, len(got.Models))
	for _, model := range got.Models {
		ids = append(ids, model.ID)
	}
	if strings.Join(ids, ",") != "deepseek/deepseek-v4.1-flash,gpt-6-luna" {
		t.Fatalf("models = %v", ids)
	}
}

// TestPreparePiProfileKeepsDeclaredModelMetadata pins that a model a person
// declared as thinking reaches Jaz's isolated profile that way. pi reads an
// entry without reasoning as a model that cannot think, so a model pi runs at
// many levels was offered none: Jaz declared it itself, in a bare form.
func TestPreparePiProfileKeepsDeclaredModelMetadata(t *testing.T) {
	clearProviderEnv(t)
	root := t.TempDir()
	const key = "provider-secret-key"
	server := piProviderServer(t, key)
	providers := map[string]provider.ModelProviderConfig{
		"commandcode": {
			Type: "openai-compatible", BaseURL: server.URL + "/v1", APIKey: key,
			DefaultModel: "deepseek/deepseek-v4.1-flash",
			Reasoning:    []string{"deepseek/deepseek-v4.1-flash"},
		},
	}
	if _, err := acp.PreparePiProfile(root, map[string]string{}, providers, "commandcode/deepseek/deepseek-v4.1-flash"); err != nil {
		t.Fatal(err)
	}
	for _, model := range piModels2026(t, root)["commandcode"] {
		if model["id"] != "deepseek/deepseek-v4.1-flash" {
			continue
		}
		if model["reasoning"] != true {
			t.Fatalf("declared reasoning lost: %+v", model)
		}
		return
	}
	t.Fatalf("model not declared")
}

// TestPreparePiProfileRefreshesItsOwnModels pins that the two kinds of entry
// stay apart. A model Jaz added is rewritten with what the provider says now,
// so a rename takes effect and a retired model leaves, while an entry Jaz did
// not add keeps everything declared for it.
func TestPreparePiProfileRefreshesItsOwnModels(t *testing.T) {
	clearProviderEnv(t)
	root := t.TempDir()
	home := filepath.Join(root, "acp", "pi")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	// A model Jaz never added: a person declared its thinking here, next to the
	// index that says which models are Jaz's own.
	declared := `{"jazModels":{"commandcode":["retired"]},"providers":{"commandcode":{"models":[` +
		`{"id":"flash","reasoning":true,"cost":{"input":1}}]}}}`
	if err := os.WriteFile(filepath.Join(home, "models.json"), []byte(declared), 0o600); err != nil {
		t.Fatal(err)
	}
	// The provider now calls it "Flash 2" with a doubled window, but that entry
	// is a person's: what they declared about it stays as they declared it.
	refreshPiProfile(t, root,
		[]map[string]any{
			{"id": "flash", "name": "Flash", "context_length": 1000000},
			{"id": "retired", "name": "Retired", "context_length": 1000000},
		},
		[]map[string]any{{"id": "flash", "name": "Flash 2", "context_length": 2000000}},
	)
	models := piModels2026(t, root)["commandcode"]
	if len(models) != 1 || models[0]["id"] != "flash" || models[0]["reasoning"] != true {
		t.Fatalf("declared entry not kept as declared: %+v", models)
	}
	if _, ok := models[0]["cost"]; !ok {
		t.Fatalf("declared entry lost its other fields: %+v", models[0])
	}
}

// TestPreparePiProfileRefreshesWhatJazAdded pins the other half: an entry Jaz
// wrote is the provider's to change.
func TestPreparePiProfileRefreshesWhatJazAdded(t *testing.T) {
	clearProviderEnv(t)
	root := t.TempDir()
	refreshPiProfile(t, root,
		[]map[string]any{
			{"id": "flash", "name": "Flash", "context_length": 1000000},
			{"id": "retired", "name": "Retired", "context_length": 1000000},
		},
		[]map[string]any{{"id": "flash", "name": "Flash 2", "context_length": 2000000}},
	)
	models := piModels2026(t, root)["commandcode"]
	if len(models) != 1 || models[0]["id"] != "flash" || models[0]["name"] != "Flash 2" || models[0]["contextWindow"] != float64(2000000) {
		t.Fatalf("provider response not reflected: %+v", models)
	}
}

// TestPreparePiProfileWithdrawsAThinkingDeclaration pins that what Jaz wrote for
// a model follows the declaration now: withdrawing it stops claiming the model
// thinks, or the picker keeps offering levels nothing backs any more.
func TestPreparePiProfileWithdrawsAThinkingDeclaration(t *testing.T) {
	clearProviderEnv(t)
	root := t.TempDir()
	answer := []map[string]any{{"id": "flash", "name": "Flash", "context_length": 1000000}}
	declaring := []string{"flash"} // a person declared it thinking
	refreshDeclaring(t, root, declaring, answer)
	refreshDeclaring(t, root, nil, answer) // and then withdrew

	models := piModels2026(t, root)["commandcode"]
	if len(models) != 1 || models[0]["id"] != "flash" {
		t.Fatalf("model not declared: %+v", models)
	}
	if _, ok := models[0]["reasoning"]; ok {
		t.Fatalf("withdrawn declaration still claims thinking: %+v", models[0])
	}
}

// TestPreparePiProfileMigratesWhatJazWrote pins that the index Jaz adds to an
// older file does not make the entries in it someone else's. Before the index
// only Jaz wrote this file, so every entry was Jaz's own and keeps being
// refreshed — with the fields a person declared about it still there.
func TestPreparePiProfileMigratesWhatJazWrote(t *testing.T) {
	clearProviderEnv(t)
	root := t.TempDir()
	home := filepath.Join(root, "acp", "pi")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	// A file from before the index, with thinking declared by hand.
	older := `{"providers":{"commandcode":{"models":[` +
		`{"id":"flash","reasoning":true,"thinkingLevelMap":{"high":"high"},"contextWindow":1000000}]}}}`
	if err := os.WriteFile(filepath.Join(home, "models.json"), []byte(older), 0o600); err != nil {
		t.Fatal(err)
	}
	refreshPiProfile(t, root,
		[]map[string]any{{"id": "flash", "name": "Flash", "context_length": 2000000}},
		[]map[string]any{{"id": "flash", "name": "Flash 2", "context_length": 3000000}},
	)
	models := piModels2026(t, root)["commandcode"]
	if len(models) != 1 || models[0]["name"] != "Flash 2" || models[0]["contextWindow"] != float64(3000000) {
		t.Fatalf("Jaz's own entry stopped being refreshed: %+v", models)
	}
	if models[0]["reasoning"] != true {
		t.Fatalf("the declared thinking was dropped: %+v", models[0])
	}
	if _, ok := models[0]["thinkingLevelMap"]; !ok {
		t.Fatalf("the declared level map was dropped: %+v", models[0])
	}
}

// TestPreparePiProfileTakesAModelsFileWithoutProviders pins that a models.json
// which names no providers is read as none. The file is Jaz's own declaration
// point, so a blank or null one must not stop a thread from starting.
func TestPreparePiProfileTakesAModelsFileWithoutProviders(t *testing.T) {
	clearProviderEnv(t)
	root := t.TempDir()
	home := filepath.Join(root, "acp", "pi")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "models.json"), []byte(`{"providers":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := piProviderServer(t, "")
	providers := map[string]provider.ModelProviderConfig{
		"commandcode": {Type: "openai-compatible", BaseURL: server.URL + "/v1", DefaultModel: "gpt-6-luna"},
	}
	if _, err := acp.PreparePiProfile(root, map[string]string{}, providers, ""); err != nil {
		t.Fatal(err)
	}
	if got := piModels2026(t, root)["commandcode"]; len(got) == 0 {
		t.Fatalf("provider not declared over an empty file")
	}
}

// piModels2026 reads the models.json Jaz wrote, per provider.
// refreshPiProfile runs the declaration against a sequence of provider answers,
// one refresh per answer: a provider's list is cached per endpoint, so each
// refresh is served by its own server.
func refreshPiProfile(t *testing.T, root string, answers ...[]map[string]any) {
	refreshDeclaring(t, root, nil, answers...)
}

// refreshDeclaring refreshes with a person's thinking declarations in place.
func refreshDeclaring(t *testing.T, root string, reasoning []string, answers ...[]map[string]any) {
	for _, models := range answers {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": models})
		}))
		t.Cleanup(server.Close)
		providers := map[string]provider.ModelProviderConfig{
			"commandcode": {Type: "openai-compatible", BaseURL: server.URL + "/v1", APIKey: "k", Reasoning: reasoning},
		}
		if _, err := acp.PreparePiProfile(root, map[string]string{}, providers, ""); err != nil {
			t.Fatal(err)
		}
	}
}

func piModels2026(t *testing.T, root string) map[string][]map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "acp", "pi", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Providers map[string]struct {
			Models []map[string]any `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("models.json: %v: %s", err, data)
	}
	out := make(map[string][]map[string]any, len(file.Providers))
	for id, provider := range file.Providers {
		out[id] = provider.Models
	}
	return out
}

// TestPreparePiProfileDeclaresProviderWithoutKey pins that a provider Jaz has
// no key for is still declared: pi's own `auth login` for that provider needs
// the declaration, and a stored pi credential outranks models.json.
func TestPreparePiProfileDeclaresProviderWithoutKey(t *testing.T) {
	clearProviderEnv(t)
	providers := map[string]provider.ModelProviderConfig{
		"commandcode": {Type: "openai-compatible", BaseURL: "https://provider.test/v1", DefaultModel: "gpt-6-luna"},
	}
	root := t.TempDir()
	supplied, err := acp.PreparePiProfile(root, map[string]string{}, providers, "commandcode/gpt-6-luna")
	if err != nil || len(supplied) != 0 {
		t.Fatalf("supplied = %v, err = %v", supplied, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "acp", "pi", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Providers map[string]struct {
			APIKey string `json:"apiKey"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if got, ok := file.Providers["commandcode"]; !ok || got.APIKey != "" {
		t.Fatalf("commandcode = %+v, present = %v", got, ok)
	}
}

// TestPreparePiProfileBindsBuiltinProviderKey pins the acceptance criterion for
// a provider pi describes itself: the key reaches pi's own environment variable
// (pi reads OPENAI_API_KEY), while models.json stays untouched so pi keeps its
// own api and model list.
func TestPreparePiProfileBindsBuiltinProviderKey(t *testing.T) {
	clearProviderEnv(t)
	root := t.TempDir()
	env := map[string]string{}
	providers := map[string]provider.ModelProviderConfig{
		"openai": {APIKey: "openai-secret"},
	}
	supplied, err := acp.PreparePiProfile(root, env, providers, "openai/gpt-6-luna")
	if err != nil || len(supplied) != 1 || supplied[0] != "openai" {
		t.Fatalf("supplied = %v, err = %v", supplied, err)
	}
	if env["OPENAI_API_KEY"] != "openai-secret" {
		t.Fatalf("built-in provider key did not reach pi's env: %v", env)
	}
	if _, err := os.Stat(filepath.Join(root, "acp", "pi", "models.json")); !os.IsNotExist(err) {
		t.Fatalf("built-in provider must not be declared: %v", err)
	}
}

// TestPreparePiProfileWithoutProvidersWritesNoModels pins that a Jaz with no
// provider configured leaves pi's own profile alone.
func TestPreparePiProfileWithoutProvidersWritesNoModels(t *testing.T) {
	clearProviderEnv(t)
	root := t.TempDir()
	supplied, err := acp.PreparePiProfile(root, map[string]string{}, nil, "")
	if err != nil || len(supplied) != 0 {
		t.Fatalf("supplied = %v, err = %v", supplied, err)
	}
	if _, err := os.Stat(filepath.Join(root, "acp", "pi", "models.json")); !os.IsNotExist(err) {
		t.Fatalf("models.json must not exist: %v", err)
	}
}
