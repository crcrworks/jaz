package acp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/wins/jaz/backend/internal/processenv"
)

// prepareDevinEnv leaves Devin's own configuration where the user's CLI keeps
// it: Devin owns its login and its session store, so Jaz neither copies the
// credential nor points the CLI at a profile of its own.
func prepareDevinEnv(env map[string]string) {
	processenv.PreserveHost(env,
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"LANG",
		"LC_ALL",
		"LC_CTYPE",
		"LOGNAME",
		"NO_PROXY",
		"SSL_CERT_DIR",
		"SSL_CERT_FILE",
		"SSH_AUTH_SOCK",
		"USER",
		"WINDSURF_CODEIUM_DIR",
		"XDG_CACHE_HOME",
		"XDG_CONFIG_HOME",
		"XDG_DATA_HOME",
	)
}

// devinCredentialsPath follows Devin's own data directory; the CLI writes its
// login to $XDG_DATA_HOME/devin/credentials.toml.
func devinCredentialsPath(env map[string]string) string {
	data := strings.TrimSpace(env["XDG_DATA_HOME"])
	if data == "" {
		if home := firstNonEmpty(env["HOME"], defaultHomePath("")); home != "" {
			data = filepath.Join(home, ".local", "share")
		}
	}
	if data == "" {
		return ""
	}
	return filepath.Join(data, "devin", "credentials.toml")
}

// devinLoggedIn reads only whether Devin's credential file holds a login; the
// token itself stays in Devin's file and never reaches Jaz.
func devinLoggedIn(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var credential struct {
		APIKey string `toml:"windsurf_api_key"`
	}
	return toml.Unmarshal(data, &credential) == nil && strings.TrimSpace(credential.APIKey) != ""
}

func resolveDevinAuth(env map[string]string) resolvedAgentAuth {
	path := devinCredentialsPath(env)
	status := resolvedAgentAuth{
		Config:      AgentAuthConfig{Mode: AuthModeExistingCLI},
		StoragePath: path,
		Source:      AuthModeExistingCLI,
	}
	switch {
	case path != "" && devinLoggedIn(path):
		status.markAuthenticated("credentials_toml", AuthKindOAuth)
	case path != "":
		status.Reason = "Run `devin auth login` to sign in to Devin"
	default:
		status.Reason = "Devin's home directory is unknown; install the Devin CLI and run `devin auth login`"
	}
	return status
}

func disconnectDevinAuth(ctx context.Context, cfg AgentConfig) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command, args := launchCommand("devin", []string{"auth", "logout"})
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = processenv.List(NewManager(nil, Config{}, nil).probeEnv(AgentDevin, cfg))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Devin sign out: %w", err)
	}
	return nil
}
