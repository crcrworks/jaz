package acp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func devinHome(t *testing.T) map[string]string {
	t.Helper()
	root := t.TempDir()
	data := filepath.Join(root, "share")
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("WINDSURF_CODEIUM_DIR", filepath.Join(root, "codeium"))
	return map[string]string{"HOME": root, "XDG_DATA_HOME": data, "WINDSURF_CODEIUM_DIR": filepath.Join(root, "codeium")}
}

func TestDevinAuthReadsOnlyItsOwnLogin(t *testing.T) {
	env := devinHome(t)
	cfg, ok := BuiltinAgents().Agent(AgentDevin)
	if !ok {
		t.Fatal("devin is not a builtin agent")
	}
	status := ProbeAgentAuth(AgentDevin, cfg, t.TempDir(), env)
	if status.Authenticated || status.StoragePath != filepath.Join(env["XDG_DATA_HOME"], "devin", "credentials.toml") {
		t.Fatalf("signed-out devin status = %#v", status)
	}
	if !strings.Contains(status.Reason, "devin auth login") {
		t.Fatalf("signed-out reason = %q", status.Reason)
	}
	if status.LoginCommand != "devin auth login" {
		t.Fatalf("login command = %q", status.LoginCommand)
	}
	if _, err := os.Stat(filepath.Dir(status.StoragePath)); !os.IsNotExist(err) {
		t.Fatalf("probing created Devin's credential directory: %v", err)
	}

	path := status.StoragePath
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("api_server_url = \"https://server.invalid\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := ProbeAgentAuth(AgentDevin, cfg, t.TempDir(), env); status.Authenticated {
		t.Fatalf("a credential file without a key reported a login: %#v", status)
	}
	if err := os.WriteFile(path, []byte("windsurf_api_key = \"token-value\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status = ProbeAgentAuth(AgentDevin, cfg, t.TempDir(), env)
	if !status.Authenticated || status.AuthKind != AuthKindOAuth || status.AuthMode != AuthModeExistingCLI || status.RefreshOwner != RefreshOwnerAgentCLI {
		t.Fatalf("signed-in devin status = %#v", status)
	}
	if status.RecommendedAuth.Mode != AuthModeExistingCLI || status.RecommendedAuth.Path != "" {
		t.Fatalf("devin recommended auth = %#v, want its own CLI login", status.RecommendedAuth)
	}
}

func TestDevinRulesViewCarriesJazPromptBesideUserRules(t *testing.T) {
	env := devinHome(t)
	source := env["WINDSURF_CODEIUM_DIR"]
	if err := os.MkdirAll(filepath.Join(source, "memories"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "memories", "global_rules.md"), []byte("user rule\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "memories", "other.md"), []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	launchEnv := map[string]string{}
	for key, value := range env {
		launchEnv[key] = value
	}
	cleanup, err := configureDevinPrompt(launchEnv, "jaz prompt")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	view := launchEnv["WINDSURF_CODEIUM_DIR"]
	if view == source {
		t.Fatalf("prompt replaced the user's own Codeium directory")
	}
	rules, err := os.ReadFile(filepath.Join(view, "memories", "global_rules.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(rules) != "user rule\n\njaz prompt\n" {
		t.Fatalf("rules = %q", rules)
	}
	if linked, err := os.Readlink(filepath.Join(view, "memories", "other.md")); err != nil || linked != filepath.Join(source, "memories", "other.md") {
		t.Fatalf("other rule link = %q, %v", linked, err)
	}
	if original, _ := os.ReadFile(filepath.Join(source, "memories", "global_rules.md")); string(original) != "user rule\n" {
		t.Fatalf("user's own rules were rewritten: %q", original)
	}
	cleanup()
	if _, err := os.Stat(view); !os.IsNotExist(err) {
		t.Fatalf("rules view outlived its launch: %v", err)
	}
}

func TestDevinAuthConfigRejectsAProfileTheCLIWouldDuplicate(t *testing.T) {
	if _, err := NormalizeAgentAuthConfig(AgentDevin, AgentAuthConfig{Mode: AuthModeJazProfile}); err == nil {
		t.Fatal("devin accepted a Jaz profile")
	}
	if _, err := NormalizeAgentAuthConfig(AgentDevin, AgentAuthConfig{Mode: AuthModeExistingCLI, Path: "/tmp/native"}); err == nil {
		t.Fatal("devin accepted a custom credential path")
	}
	auth, err := NormalizeAgentAuthConfig(AgentDevin, AgentAuthConfig{Mode: AuthModeAuto})
	if err != nil || auth.Mode != AuthModeAuto {
		t.Fatalf("devin auto auth = %#v, %v", auth, err)
	}
}
