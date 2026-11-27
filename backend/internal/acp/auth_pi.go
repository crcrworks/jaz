package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wins/jaz/backend/internal/processenv"
	modelprovider "github.com/wins/jaz/backend/internal/provider"
	"github.com/wins/jaz/backend/internal/runtimefiles"
)

// piMissingCredentialReason is pi's two ways to get a provider key: Jaz's Model
// Providers, which Jaz feeds to pi, or pi's own login.
const piMissingCredentialReason = "add the provider's API key in Settings > Model Providers, or sign in to pi with `pi auth login <provider>` in its Jaz profile"

// resolvePiAuth reports whether pi itself says it has credentials for the model
// a thread would run. pi owns the answer; Jaz only hands it the providers it
// knows about and reads back what pi said.
func resolvePiAuth(cfg AgentConfig, root string, env map[string]string, providers map[string]modelprovider.ModelProviderConfig) resolvedAgentAuth {
	home := runtimefiles.New(root).ACPPiHome
	status := resolvedAgentAuth{
		Config:      AgentAuthConfig{Mode: AuthModeExistingCLI, Path: home},
		StoragePath: home,
		Source:      AuthModeExistingCLI,
	}
	if _, err := ResolveExecutable(AgentPi); err != nil {
		status.Reason = "pi executable not found; install pi on the Jaz server"
		return status
	}
	checkEnv := maps.Clone(env)
	if checkEnv == nil {
		checkEnv = map[string]string{}
	}
	maps.Copy(checkEnv, piProfileEnv(root))
	model := cfg.ProviderQualifiedModel()
	supplied, err := PreparePiProfile(root, checkEnv, providers, model)
	if err != nil {
		status.Reason = err.Error()
		return status
	}
	if model == "" {
		// No model is configured yet, so pi is asked about a provider instead:
		// the ones Jaz just handed it, else the ones it is signed in to. pi still
		// owns the answer.
		selector := supplied
		if len(selector) == 0 {
			selector = piAuthProviders(home)
		}
		if len(selector) == 0 {
			status.Reason = piMissingCredentialReason
			return status
		}
		authType, reason, ok := piAuthProviderCheck(checkEnv, piCommand(cfg), selector[0])
		if reason != "" || !ok {
			status.Reason = reason
			return status
		}
		status.Kind = AuthKindAPIKey
		if authType == "oauth" {
			status.Kind = AuthKindOAuth
		}
		status.markAuthenticated("pi_auth_check", status.Kind)
		return status
	}
	authType, reason, ok := piAuthCheck(checkEnv, piCommand(cfg), model)
	if reason != "" || !ok {
		status.Reason = reason
		return status
	}
	status.Kind = AuthKindAPIKey
	if authType == "oauth" {
		status.Kind = AuthKindOAuth
	}
	status.markAuthenticated("pi_auth_check", status.Kind)
	return status
}

// piAuthProviders lists the providers pi holds credentials for. Jaz reads only
// which providers exist in pi's own auth file, never the secrets.
func piAuthProviders(home string) []string {
	data, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		return nil
	}
	var stored map[string]json.RawMessage
	if json.Unmarshal(data, &stored) != nil {
		return nil
	}
	providers := make([]string, 0, len(stored))
	for provider := range stored {
		if strings.TrimSpace(provider) != "" {
			providers = append(providers, provider)
		}
	}
	sort.Strings(providers)
	return providers
}

// piAuthCheck runs `pi auth check` in the isolated profile and returns what pi
// said. It judges the same executable the thread runs. pi exits 0 for ready, 1
// for not ready and 2 for a model its profile cannot resolve, so the output
// carries the answer rather than the exit code.
func piAuthCheck(env map[string]string, command, model string) (authType, reason string, ok bool) {
	return piCheck(env, command, "--model", model)
}

// piAuthProviderCheck asks pi about a provider rather than a model, for the
// state before anyone has chosen one.
func piAuthProviderCheck(env map[string]string, command, providerID string) (authType, reason string, ok bool) {
	return piCheck(env, command, "--provider", providerID)
}

func piCheck(env map[string]string, command, flag, value string) (authType, reason string, ok bool) {
	if command == "" {
		return "", "pi executable not found; install pi on the Jaz server", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, "auth", "check", "--json", flag, value)
	cmd.Env = processenv.List(env)
	output, _ := cmd.Output()
	var checked struct {
		Status   string `json:"status"`
		AuthType string `json:"authType"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(output, &checked); err != nil {
		return "", "pi auth check returned no answer", false
	}
	switch checked.Status {
	case "ready":
		return checked.AuthType, "", true
	case "not_ready":
		if checked.Reason == "provider_not_found" {
			return "", fmt.Sprintf("pi does not know provider %q; add it in Settings > Model Providers", value), false
		}
		return checked.AuthType, piMissingCredentialReason, false
	default:
		// pi answers "invalid" when its profile does not describe the model at
		// all, which means a missing provider rather than a wrong model name.
		return "", fmt.Sprintf("pi cannot resolve %q in its Jaz profile (%s); add its provider in Settings > Model Providers", value, firstNonEmpty(checked.Reason, checked.Status)), false
	}
}

// piProfileEnv is the environment pi itself runs in: Jaz's isolated profile,
// never the user's ~/.pi.
func piProfileEnv(root string) map[string]string {
	return map[string]string{
		"PI_CODING_AGENT_DIR":   runtimefiles.New(root).ACPPiHome,
		"PI_SKIP_VERSION_CHECK": "1",
	}
}

// piLoginInvocation tells a person how to sign pi in. pi refuses to log in
// without a terminal, so Jaz does not pretend to run it for them; the providers
// Jaz knows are already declared in that profile, so the command resolves them.
func piLoginInvocation(root string) AgentLoginInvocation {
	home := piProfileEnv(root)["PI_CODING_AGENT_DIR"]
	command, err := ResolveExecutable(AgentPi)
	if err != nil {
		command = AgentPi
	}
	display := fmt.Sprintf("PI_CODING_AGENT_DIR=%s %s auth login <provider>", shellQuote(home), shellQuote(command))
	return AgentLoginInvocation{
		Env:        piProfileEnv(root),
		Executable: command,
		Display:    display,
		Reason:     "pi signs in from its own terminal; run the command on the Jaz server. Providers from Settings > Model Providers are declared in that profile already, so the command accepts them.",
	}
}
