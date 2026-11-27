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

// TestPreparePiProfileDeclaresProviderWithoutKey pins that a provider Jaz has
// no key for is still declared: pi's own `auth login` for that provider needs
// the declaration, and a stored pi credential outranks models.json.
func TestPreparePiProfileDeclaresProviderWithoutKey(t *testing.T) {
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

// TestPreparePiProfileLeavesBuiltinProvidersToPi pins that a built-in provider
// pi already describes is not declared: a models.json entry would replace pi's
// own api and model list with a narrower one.
func TestPreparePiProfileLeavesBuiltinProvidersToPi(t *testing.T) {
	root := t.TempDir()
	providers := map[string]provider.ModelProviderConfig{
		"openai": {APIKey: "openai-secret"},
	}
	supplied, err := acp.PreparePiProfile(root, map[string]string{}, providers, "openai/gpt-6-luna")
	if err != nil || len(supplied) != 0 {
		t.Fatalf("supplied = %v, err = %v", supplied, err)
	}
	if _, err := os.Stat(filepath.Join(root, "acp", "pi", "models.json")); !os.IsNotExist(err) {
		t.Fatalf("built-in provider must not be declared: %v", err)
	}
}

// TestPreparePiProfileWithoutProvidersWritesNoModels pins that a Jaz with no
// provider configured leaves pi's own profile alone.
func TestPreparePiProfileWithoutProvidersWritesNoModels(t *testing.T) {
	root := t.TempDir()
	supplied, err := acp.PreparePiProfile(root, map[string]string{}, nil, "")
	if err != nil || len(supplied) != 0 {
		t.Fatalf("supplied = %v, err = %v", supplied, err)
	}
	if _, err := os.Stat(filepath.Join(root, "acp", "pi", "models.json")); !os.IsNotExist(err) {
		t.Fatalf("models.json must not exist: %v", err)
	}
}
