package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/wins/jaz/backend/internal/pirpc"
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
	authType, reason, ok := piAuthCheck(env, piModelPattern(cfg))
	if reason != "" {
		status.Reason = reason
		return status
	}
	status.Kind = AuthKindAPIKey
	if authType == "oauth" {
		status.Kind = AuthKindOAuth
	}
	if ok {
		status.Evidence = "pi auth check"
		status.markAuthenticated("pi_auth_check", status.Kind)
	}
	return status
}

// piModelPattern names the model pi should judge its credentials against: the
// configured one, or the model pi itself would start with.
func piModelPattern(cfg AgentConfig) string {
	if model := strings.TrimSpace(cfg.ProviderQualifiedModel()); model != "" {
		return model
	}
	return ""
}

// piAuthCheck runs `pi auth check` in the isolated profile and returns what pi
// said. pi exits 0 for ready, 1 for not ready and 2 for an unknown provider, so
// the output carries the answer rather than the exit code.
func piAuthCheck(env map[string]string, model string) (authType, reason string, ok bool) {
	command, err := ResolveExecutable(AgentPi)
	if err != nil {
		return "", "pi executable not found; install pi on the Jaz server", false
	}
	args := []string{"auth", "check", "--json"}
	if model != "" {
		args = append(args, "--model", model)
	} else {
		return "", "", false
	}
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

func disconnectPiAuth(ctx context.Context, cfg AgentConfig, root string) error {
	command, err := ResolveExecutable(AgentPi)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, "auth", "logout", strings.TrimSpace(cfg.ModelProvider))
	cmd.Env = processenv.List(piProfileEnv(root))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pi sign out: %w", err)
	}
	return nil
}

var _ = pirpc.CheckInstalled
