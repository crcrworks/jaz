package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wins/jaz/backend/internal/processenv"
	"github.com/wins/jaz/backend/internal/runtimefiles"
)

// resolvePiAuth reports whether pi itself says it has credentials for the
// model a thread would run. pi owns the check; Jaz only reads the answer.
func resolvePiAuth(cfg AgentConfig, root string, env map[string]string) resolvedAgentAuth {
	status := resolvedAgentAuth{
		Config:      AgentAuthConfig{Mode: AuthModeExistingCLI, Path: runtimefiles.New(root).ACPPiHome},
		StoragePath: runtimefiles.New(root).ACPPiHome,
		Source:      AuthModeExistingCLI,
	}
	if _, err := ResolveExecutable(AgentPi); err != nil {
		status.Reason = "pi executable not found; install pi on the Jaz server"
		return status
	}
	providers := piAuthProviders(status.StoragePath)
	if len(providers) == 0 {
		status.Reason = "sign in to pi with `pi auth login <provider>` in its Jaz profile"
		return status
	}
	if model := piModelPattern(cfg); model != "" {
		authType, reason, ok := piAuthCheck(env, model)
		if reason != "" {
			status.Reason = reason
			return status
		}
		if !ok {
			status.Reason = "sign in to pi with `pi auth login <provider>` in its Jaz profile"
			return status
		}
		status.Kind = AuthKindAPIKey
		if authType == "oauth" {
			status.Kind = AuthKindOAuth
		}
		status.Evidence = "pi auth check"
		status.markAuthenticated("pi_auth_check", status.Kind)
		return status
	}
	status.Evidence = "auth.json"
	status.markAuthenticated("auth_json", AuthKindOAuth)
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

// piModelPattern names the model pi should judge its credentials against. pi
// names models "provider/id" itself, so the configured id is passed through.
func piModelPattern(cfg AgentConfig) string {
	return strings.TrimSpace(cfg.Model)
}

// piAuthCheck runs `pi auth check` in the isolated profile and returns what pi
// said. pi exits 0 for ready, 1 for not ready and 2 for an unknown provider, so
// the output carries the answer rather than the exit code.
func piAuthCheck(env map[string]string, model string) (authType, reason string, ok bool) {
	command, err := ResolveExecutable(AgentPi)
	if err != nil {
		return "", "pi executable not found; install pi on the Jaz server", false
	}
	args := []string{"auth", "check", "--json", "--model", model}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = processenv.List(env)
	output, _ := cmd.Output()
	var checked struct {
		Status   string `json:"status"`
		AuthType string `json:"authType"`
	}
	if err := json.Unmarshal(output, &checked); err != nil {
		return "", "pi auth check returned no answer", false
	}
	switch checked.Status {
	case "ready":
		return checked.AuthType, "", true
	case "not_ready":
		return checked.AuthType, "sign in to pi with `pi auth login <provider>` in its Jaz profile, or add an API key there", false
	default:
		return checked.AuthType, fmt.Sprintf("pi does not know the provider of model %q", model), false
	}
}

// PiProfileEnv is the environment a person runs pi's own login command in: Jaz's
// isolated profile, never their ~/.pi.
func piProfileEnv(root string) map[string]string {
	return map[string]string{
		"PI_CODING_AGENT_DIR": runtimefiles.New(root).ACPPiHome,
		"PI_SKIP_VERSION_CHECK": "1",
	}
}

// piLoginInvocation tells a person how to sign pi in. pi refuses to log in
// without a terminal, so Jaz does not pretend to run it for them.
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
		Reason:     "pi signs in from its own terminal; run the command on the Jaz server",
	}
}


// PreparePiProfile creates the isolated pi profile a thread runs in, so a
// person can sign pi in there before any thread exists.
func PreparePiProfile(root string) (string, error) {
	home := runtimefiles.New(root).ACPPiHome
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	return home, nil
}
