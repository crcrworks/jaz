package acp_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/wins/jaz/backend/internal/acp"
	"github.com/wins/jaz/backend/internal/provider"
	jsonstore "github.com/wins/jaz/backend/internal/storage/json"
)

// devinLaunches records what Devin's process was actually given: the rules
// directory and the system prompt that reached the CLI instead of a session.
type devinLaunches struct {
	codeiumDir, rules, prompt string
}

func readDevinLaunch(capture string) (devinLaunches, error) {
	dir, err := os.ReadFile(filepath.Join(capture, "codeium"))
	if err != nil {
		return devinLaunches{}, err
	}
	launch := devinLaunches{codeiumDir: string(dir)}
	if launch.rules, err = readFile(filepath.Join(launch.codeiumDir, "memories", "global_rules.md")); err != nil {
		return devinLaunches{}, err
	}
	launch.prompt, err = readFile(filepath.Join(capture, "system-prompt"))
	return launch, err
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

func TestDevinGetsJazPromptThroughItsOwnRulesFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "codeium", "windsurf")
	if err := os.MkdirAll(filepath.Join(source, "memories"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "memories", "global_rules.md"), []byte("user rule"), 0o600); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(root, "capture")
	if err := os.MkdirAll(capture, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := acp.NewManager(store, acp.Config{
		Root:         root,
		Workspace:    root,
		SystemPrompt: staticPrompt("Jaz context"),
		Agents: map[string]acp.AgentConfig{
			acp.AgentDevin: {
				Command: os.Args[0],
				Args:    []string{"-test.run=TestFakeACPAgentProcess"},
				Env: map[string]string{
					"JAZ_FAKE_ACP_AGENT":            "1",
					"JAZ_FAKE_ACP_RESUME":           "1",
					"JAZ_FAKE_ACP_MODELS":           "fake-large",
					"JAZ_FAKE_ACP_CODEXIUM_CAPTURE": capture,
					"WINDSURF_CODEIUM_DIR":          source,
				},
			},
		},
	}, log.New(io.Discard))
	defer manager.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	spawned, err := manager.Spawn(ctx, acp.SpawnRequest{ACPAgent: acp.AgentDevin, Slug: "devin-context"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Send(ctx, acp.SendRequest{Session: spawned.SessionID, Message: "hello", Completion: acp.CompletionInline}); err != nil {
		t.Fatal(err)
	}
	job, err := manager.Wait(ctx, acp.WaitRequest{Session: spawned.SessionID, Timeout: 5 * time.Second})
	if err != nil || job.State != acp.StateIdle || job.Assistant != "hello from fake agent" {
		t.Fatalf("Devin turn = %#v, %v", job, err)
	}
	launch, err := readDevinLaunch(capture)
	if err != nil {
		t.Fatal(err)
	}
	if launch.codeiumDir == source {
		t.Fatalf("Devin launched against the user's own Codeium directory")
	}
	if launch.rules != "user rule\n\nJaz context\n" {
		t.Fatalf("Devin rules = %q", launch.rules)
	}
	// Devin has no session prompt channel, so nothing may ride session _meta.
	if launch.prompt != "" {
		t.Fatalf("Devin received a session system prompt: %q", launch.prompt)
	}
	messages, err := store.LoadMessages(spawned.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if strings.Contains(provider.MessageContent(message), "Jaz context") {
			t.Fatal("Jaz context leaked into the transcript")
		}
	}
	manager.Close()
	if _, err := os.Stat(launch.codeiumDir); !os.IsNotExist(err) {
		t.Fatalf("rules view survived Close: %v", err)
	}
	if rules, err := readFile(filepath.Join(source, "memories", "global_rules.md")); err != nil || rules != "user rule" {
		t.Fatalf("user's own rules changed: %q, %v", rules, err)
	}
}
