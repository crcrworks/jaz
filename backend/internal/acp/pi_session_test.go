package acp_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wins/jaz/backend/internal/acp"
	mcpconfig "github.com/wins/jaz/backend/internal/mcpconfig"
	"github.com/wins/jaz/backend/internal/mcpsession"
	modelprovider "github.com/wins/jaz/backend/internal/provider"
	"github.com/wins/jaz/backend/internal/storage"
)

// staticMCPServers is a fixed MCP server list, as Settings holds.
type staticMCPServers []mcpconfig.Server

func (s staticMCPServers) ListMCPServers() ([]mcpconfig.Server, error) {
	return s, nil
}

// jaztoolsServer is Jaz's own server, whose session header is the placeholder
// the runtime resolves per thread.
func jaztoolsServer(sessionHeader string) mcpconfig.Server {
	return mcpconfig.Server{
		ID: "jaztools", Name: "jaztools", Enabled: true, Transport: mcpconfig.TransportStreamableHTTP,
		URL: "http://127.0.0.1:1/mcp/jaztools",
		Headers: []mcpconfig.Header{
			{Name: "Authorization", Value: "Bearer k"},
			{Name: mcpsession.HeaderName, Value: sessionHeader},
		},
	}
}

// TestPiIsACompactCapableBuiltinAgent pins how pi appears to the rest of Jaz:
// selectable, offering pi's own thinking levels, and compactable through pi's
// own compaction rather than a Jaz-written summary.
func TestPiIsACompactCapableBuiltinAgent(t *testing.T) {
	catalog := acp.BuiltinAgents()
	cfg, ok := catalog.Agent(acp.AgentPi)
	if !ok || !cfg.BuiltinBridge {
		t.Fatalf("pi = %#v, %v", cfg, ok)
	}
	if !acp.AgentSupportsCompact(acp.AgentPi) {
		t.Fatal("pi compacts through its own compact command")
	}
	names := acp.SelectableAgentNames(catalog.Names())
	if names[len(names)-1] != acp.AgentPi {
		t.Fatalf("selectable agents = %v", names)
	}
	options := acp.AgentOptionsForConfig(acp.AgentPi, cfg, t.TempDir())
	levels := make([]string, 0, len(options.ReasoningEfforts))
	for _, option := range options.ReasoningEfforts {
		levels = append(levels, option.Value)
	}
	want := []string{"", "off", "minimal", "low", "medium", "high", "xhigh", "max"}
	if len(levels) != len(want) {
		t.Fatalf("pi thinking levels = %v", levels)
	}
	for i := range want {
		if levels[i] != want[i] {
			t.Fatalf("pi thinking levels = %v", levels)
		}
	}
}

// TestPiThreadsRunInJazsOwnProfile checks the wiring a real thread depends on:
// pi's process environment names Jaz's profile, not the user's ~/.pi.
func TestPiThreadsRunInJazsOwnProfile(t *testing.T) {
	store, root := newTestStore(t)
	manager := newFakeNamedAgentManagerWithOptions(t, store, root, acp.AgentPi, map[string]string{}, "", "")
	env, err := manager.ProbePiEnv(acp.AgentPi)
	if err != nil {
		t.Fatal(err)
	}
	if env["PI_CODING_AGENT_DIR"] != filepath.Join(root, "acp", "pi") {
		t.Fatalf("PI_CODING_AGENT_DIR = %q", env["PI_CODING_AGENT_DIR"])
	}
	if info, err := os.Stat(env["PI_CODING_AGENT_DIR"]); err != nil || !info.IsDir() {
		t.Fatalf("pi profile must be prepared: %v", err)
	}
}

// TestPiThreadBindsModelProviderKey checks the runtime launch environment, not
// just the settings probe: a key in Model Providers must reach pi's own variable
// in the process Jaz actually spawns. A built-in provider is the case that used
// to be dropped.
func TestPiThreadBindsModelProviderKey(t *testing.T) {
	// openai is exactly as Settings holds it for a built-in: a key and nothing
	// else. commandcode is a provider pi does not describe itself.
	for _, test := range []struct {
		provider string
		variable string
		config   modelprovider.ModelProviderConfig
	}{
		{"openai", "OPENAI_API_KEY", modelprovider.ModelProviderConfig{APIKey: "openai-secret"}},
		{"commandcode", "JAZ_PROVIDER_COMMANDCODE_API_KEY", modelprovider.ModelProviderConfig{APIKey: "commandcode-secret", BaseURL: "https://provider.test/v1", Type: "openai-compatible"}},
	} {
		store, root := newTestStore(t)
		manager := newFakeNamedAgentManagerWithOptions(t, store, root, acp.AgentPi, map[string]string{}, test.provider+"/some-model", "", func(cfg *acp.Config) {
			cfg.Providers = map[string]modelprovider.ModelProviderConfig{test.provider: test.config}
		})
		env, err := manager.ProbePiEnv(acp.AgentPi)
		if err != nil {
			t.Fatal(err)
		}
		if env[test.variable] != test.config.APIKey {
			t.Fatalf("%s: %s not bound into pi's launch env", test.provider, test.variable)
		}
	}
}

// TestPiThreadKeepsJazsPlatformPrompt checks the prompt a pi thread is launched
// with: Jaz appends its instructions to pi's own prompt, and does so at every
// launch, so a resume never comes back with less than the thread started with.
func TestPiThreadKeepsJazsPlatformPrompt(t *testing.T) {
	store, root := newTestStore(t)
	launchLog := filepath.Join(root, "pi-launches.log")
	manager := newFakePiManager(t, store, root, launchLog, func(cfg *acp.Config) {
		cfg.SystemPrompt = staticPrompt("## Jaz platform\n\nPLATFORM-MARKER")
	})
	ctx := testContext(t)

	session, err := manager.CreateSession(ctx, acp.SpawnRequest{ACPAgent: acp.AgentPi})
	if err != nil {
		t.Fatalf("create pi session: %v", err)
	}
	if _, err := manager.Send(ctx, acp.SendRequest{Session: session.ID, Message: "hello", FromUser: true, Completion: acp.CompletionAsync}); err != nil {
		t.Fatalf("send: %v", err)
	}

	stored, err := store.LoadSession(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := readText(t, piPromptFile(root, stored)); !strings.Contains(got, "PLATFORM-MARKER") {
		t.Fatalf("pi's appended prompt must carry Jaz's platform prompt: %q", got)
	}
	for _, args := range launchArgs(t, launchLog) {
		if !strings.Contains(args, "--append-system-prompt") {
			t.Fatalf("pi must be launched with Jaz's prompt, got %q", args)
		}
	}
}

// launchArgs is what pi was started with, one entry per launch.
func launchArgs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read launch log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		var args string
		if err := json.Unmarshal([]byte(line), &args); err != nil {
			t.Fatalf("launch log line %q: %v", line, err)
		}
		out = append(out, args)
	}
	return out
}

// piPromptFile is the prompt pi appends for one thread.
func piPromptFile(root string, session storage.Session) string {
	return filepath.Join(root, "acp", "pi", "sessions", session.RuntimeRef.SessionID, "append-prompt.md")
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}
	return string(data)
}

// newFakePiManager is a manager whose pi executable speaks pi's RPC records:
// this test binary runs as pi's own process, so the adapter is exercised
// against the protocol rather than a stub of it.
func newFakePiManager(t *testing.T, store acp.Store, root, launchLog string, configure ...func(*acp.Config)) *acp.Manager {
	t.Helper()
	script := filepath.Join(t.TempDir(), "pi")
	// The launch records what it was handed, then becomes the fake pi process:
	// this test binary speaking pi's records.
	body := "#!/bin/sh\nJAZ_FAKE_PI_ARGS=\"$*\" exec " + shellQuoteArg(os.Args[0]) + " -test.run=^TestFakePiProcess$\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"JAZ_PI_COMMAND": script, "JAZ_FAKE_PI": "1"}
	if launchLog != "" {
		env["JAZ_FAKE_PI_LAUNCH_LOG"] = launchLog
	}
	overrides := append([]func(*acp.Config){func(cfg *acp.Config) {
		// pi runs through its own bridge, so its catalog entry must keep it
		// while gaining the fake executable.
		cfg.Agents = nil
		cfg.AgentSource = acp.MergeAgents(acp.BuiltinAgents(), map[string]acp.AgentConfig{
			acp.AgentPi: {Env: env},
		})
	}}, configure...)
	return newFakeNamedAgentManagerWithOptions(t, store, root, acp.AgentPi, env, "", "", overrides...)
}

func shellQuoteArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// TestPiMcpServersNameTheThread checks the headers pi's own MCP client sends:
// Jaz's tools read the thread from X-Jaz-Session-ID, so a pi thread that leaves
// the placeholder there cannot ask a question or act on its own thread.
func TestPiMcpServersNameTheThread(t *testing.T) {
	store, root := newTestStore(t)
	manager := newFakePiManager(t, store, root, "", func(cfg *acp.Config) {
		cfg.MCPStore = staticMCPServers{jaztoolsServer("{{jaz_session_id}}")}
	})
	ctx := testContext(t)

	session, err := manager.CreateSession(ctx, acp.SpawnRequest{ACPAgent: acp.AgentPi})
	if err != nil {
		t.Fatalf("create pi session: %v", err)
	}
	// The thread's first turn launches pi, and that launch writes the MCP
	// registration this test reads.
	if _, err := manager.Send(ctx, acp.SendRequest{Session: session.ID, Message: "one", FromUser: true, Completion: acp.CompletionAsync}); err != nil {
		t.Fatalf("send: %v", err)
	}
	extension := ""
	for range 100 {
		if stored, err := store.LoadSession(session.ID); err == nil && stored.RuntimeRef.SessionID != "" {
			extension = filepath.Join(root, "acp", "pi", "sessions", stored.RuntimeRef.SessionID, "mcp.ts")
			if _, err := os.Stat(extension); err == nil {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	written := readText(t, extension)
	if strings.Contains(written, mcpsession.HeaderPlaceholder) {
		t.Fatalf("pi must be given the thread's own session header:\n%s", written)
	}
	if !strings.Contains(written, `"X-Jaz-Session-ID"`) {
		t.Fatalf("mcp.ts must carry the thread's own session header:\n%s", written)
	}
}

// TestFakePiProcess is the pi process these tests drive: it speaks pi's own
// RPC records, so the adapter is exercised against the protocol pi documents.
func TestFakePiProcess(t *testing.T) {
	if os.Getenv("JAZ_FAKE_PI") != "1" {
		return
	}
	if path := os.Getenv("JAZ_FAKE_PI_LAUNCH_LOG"); path != "" {
		record, _ := json.Marshal(os.Getenv("JAZ_FAKE_PI_ARGS"))
		if file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			_, _ = file.Write(append(record, '\n'))
			_ = file.Close()
		}
	}
	model := map[string]any{
		"id": "fake-model", "name": "Fake Model", "provider": "fake",
		"contextWindow": 128000, "maxTokens": 4096, "reasoning": true,
		"input":            []string{"text"},
		"thinkingLevelMap": map[string]any{"off": nil, "low": "low", "high": "high"},
	}
	thinking := "high"
	emit := func(record any) {
		data, _ := json.Marshal(record)
		_, _ = os.Stdout.Write(append(data, '\n'))
	}
	respond := func(command string, id json.RawMessage, data any, failure string) {
		record := map[string]any{"id": id, "type": "response", "command": command, "success": failure == ""}
		if failure != "" {
			record["error"] = failure
		} else {
			record["data"] = data
		}
		emit(record)
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			return
		}
		var command struct {
			ID      json.RawMessage `json:"id"`
			Type    string          `json:"type"`
			Message string          `json:"message"`
			Level   string          `json:"level"`
		}
		if json.Unmarshal(line, &command) != nil {
			continue
		}
		switch command.Type {
		case "get_state":
			respond(command.Type, command.ID, map[string]any{"model": model, "thinkingLevel": thinking}, "")
		case "get_available_models":
			respond(command.Type, command.ID, map[string]any{"models": []any{model}}, "")
		case "get_available_thinking_levels":
			respond(command.Type, command.ID, map[string]any{"levels": []string{"low", "high"}}, "")
		case "set_model", "compact":
			respond(command.Type, command.ID, map[string]any{}, "")
		case "set_thinking_level":
			thinking = command.Level
			respond(command.Type, command.ID, map[string]any{}, "")
		case "prompt":
			respond(command.Type, command.ID, map[string]any{"disposition": "started"}, "")
			emit(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": "fake says " + command.Message}})
			emit(map[string]any{"type": "agent_settled", "aborted": false})
		default:
			respond(command.Type, command.ID, nil, "Unknown command: "+command.Type)
		}
	}
}
