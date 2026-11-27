package acp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	modelprovider "github.com/wins/jaz/backend/internal/provider"
)

// piProviderServer is an OpenAI-compatible provider advertising two models; the
// profile Jaz writes must point pi at it for pi to resolve the thread's model.
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

// fakePi answers `auth check` the way pi does: a provider comes from the
// profile's models.json (or pi's bundled catalog, which the "openai" and
// "openrouter" stand-ins cover), and a credential comes from auth.json, from
// models.json's apiKey ("$NAME" reads pi's environment), or from the provider's
// own variable.
func fakePi(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	name := "pi"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	script := `#!/bin/sh
model=""
while [ $# -gt 0 ]; do
  case "$1" in
    --model) model="$2"; shift ;;
    --provider) model="$2/*"; shift ;;
  esac
  shift
done
provider=${model%%/*}
home="$PI_CODING_AGENT_DIR"
bundled=" openai openrouter "
case "$bundled" in
  *" $provider "*) builtin=1 ;;
  *) builtin=0 ;;
esac
entry=$(grep -A8 "\"$provider\": {" "$home/models.json" 2>/dev/null)
if [ "$builtin" = 0 ] && [ -z "$entry" ]; then
  printf '{"status":"invalid","provider":"%s","reason":"invalid_state"}\n' "$model"
  exit 2
fi
if grep -q "\"$provider\"" "$home/auth.json" 2>/dev/null; then
  printf '{"status":"ready","provider":"%s","authType":"api_key"}\n' "$provider"
  exit 0
fi
declared=$(printf '%s\n' "$entry" | grep '"apiKey"' | sed 's/.*"apiKey": "\(.*\)".*/\1/')
case "$declared" in
  \$*) [ -n "$(printenv "${declared#\$}")" ] && key=declared ;;
  "") key="" ;;
  *) key=declared ;;
esac
if [ -z "$key" ]; then
  # A built-in provider is ready only when pi's own variable carries a key, the
  # same way real pi reads OPENAI_API_KEY / OPENROUTER_API_KEY.
  case "$provider" in
    openai) variable=OPENAI_API_KEY ;;
    openrouter) variable=OPENROUTER_API_KEY ;;
  esac
  [ -n "$variable" ] && [ -n "$(printenv "$variable")" ] && key=variable
fi
if [ -n "$key" ]; then
  printf '{"status":"ready","provider":"%s","authType":"api_key"}\n' "$provider"
  exit 0
fi
printf '{"status":"not_ready","provider":"%s","reason":"credentials_not_configured"}\n' "$provider"
exit 1
`
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// The stand-in still needs the shell utilities it calls; clearHostEnv wipes
	// PATH, so the standard directories are added back.
	path := dir
	for _, known := range []string{os.Getenv("PATH"), "/usr/bin:/bin:/usr/sbin:/sbin"} {
		if known != "" {
			path += string(os.PathListSeparator) + known
		}
	}
	t.Setenv("PATH", path)
}

// TestPiAuthBindsBuiltinProviderKey is the acceptance criterion for a provider
// pi describes itself: the key in Settings > Model Providers must reach pi's own
// environment variable, else pi is told to add the key it already has.
func TestPiAuthBindsBuiltinProviderKey(t *testing.T) {
	clearHostEnv(t)
	fakePi(t)
	for _, test := range []struct {
		provider string
		model    string
	}{
		{"openai", "gpt-6-luna"},
		{"openrouter", "z-ai/glm-5.2"},
	} {
		cfg := piConfig(test.provider, test.model)
		auth, readiness := ProbeAuthAndReadinessWithProviders(AgentPi, cfg, t.TempDir(), nil, map[string]modelprovider.ModelProviderConfig{
			test.provider: {APIKey: test.provider + "-secret"},
		})
		if !auth.Authenticated || !readiness.Available || auth.AuthKind != AuthKindAPIKey || auth.AuthEvidence != "pi_auth_check" {
			t.Fatalf("%s + Model Providers = %#v / %#v", test.provider, auth, readiness)
		}
	}
}

// TestPiAuthUsesModelProviderKeys is the acceptance criterion for a provider Jaz
// and pi do not share: pi answers ready only because Jaz declared the provider in
// pi's profile and bound its key into pi's environment.
func TestPiAuthUsesModelProviderKeys(t *testing.T) {
	clearHostEnv(t)
	fakePi(t)
	root := t.TempDir()
	server := piProviderServer(t, "command-secret")
	providers := map[string]modelprovider.ModelProviderConfig{
		"commandcode": {
			Type:         "openai-compatible",
			Label:        "CommandCode",
			BaseURL:      server.URL + "/v1",
			APIKey:       "command-secret",
			Capabilities: []string{modelprovider.CapabilityChatCompletions},
		},
	}
	cfg := piConfig("commandcode", "deepseek/deepseek-v4.1-flash")
	auth, readiness := ProbeAuthAndReadinessWithProviders(AgentPi, cfg, root, nil, providers)
	if !auth.Authenticated || !readiness.Available || auth.AuthKind != AuthKindAPIKey || auth.AuthEvidence != "pi_auth_check" {
		t.Fatalf("pi + Model Providers = %#v / %#v", auth, readiness)
	}

	// The same provider with its key variable unset: pi declares it, finds no
	// credential, and Jaz repeats what pi needs.
	auth, readiness = ProbeAuthAndReadinessWithProviders(AgentPi, cfg, t.TempDir(), nil, map[string]modelprovider.ModelProviderConfig{
		"commandcode": {Type: "openai-compatible", BaseURL: server.URL + "/v1", APIKeyEnv: "JAZ_PROVIDER_COMMANDCODE_API_KEY"},
	})
	if auth.Authenticated || readiness.Available {
		t.Fatalf("pi without a key = %#v / %#v", auth, readiness)
	}
	for _, want := range []string{"Settings > Model Providers", "pi auth login"} {
		if !strings.Contains(readiness.Reason, want) {
			t.Fatalf("reason %q does not mention %q", readiness.Reason, want)
		}
	}
}

// TestPiAuthHonoursPiOwnLogin pins the other credential source: a provider pi is
// signed in to stays ready when Jaz has no provider configured, and a provider
// pi describes itself is left entirely to pi.
func TestPiAuthHonoursPiOwnLogin(t *testing.T) {
	clearHostEnv(t)
	fakePi(t)
	root := t.TempDir()
	home := filepath.Join(root, "acp", "pi")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"openrouter":{"type":"api_key","key":"login-key"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := piConfig("openrouter", "z-ai/glm-5.2")
	auth, readiness := ProbeAuthAndReadinessWithProviders(AgentPi, cfg, root, nil, map[string]modelprovider.ModelProviderConfig{
		"openrouter": {APIKey: "jaz-key"},
	})
	if !auth.Authenticated || !readiness.Available {
		t.Fatalf("signed-in pi = %#v / %#v", auth, readiness)
	}
	if _, err := os.Stat(filepath.Join(home, "models.json")); !os.IsNotExist(err) {
		t.Fatalf("a provider pi describes itself must not be declared: %v", err)
	}
}

// piConfig is a pi thread's agent config pointed at one provider's model.
func piConfig(providerID, model string) AgentConfig {
	cfg, _ := BuiltinAgents().Agent(AgentPi)
	cfg.ProviderMode = AgentProviderModeAgentDefaults
	cfg.ModelProvider = providerID
	cfg.Model = model
	return cfg
}
