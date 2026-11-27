package pirpc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	acpschema "github.com/gluonfield/acp-transport/acp"
	"github.com/gluonfield/acp-transport/jsonrpc"
)

// bridgeFixture drives the bridge the way the manager does: an ACP peer on the
// other end of the pipe, with pi's own process behind it.
type bridgeFixture struct {
	peer   *jsonrpc.Peer
	root   string
	launch string

	mu      sync.Mutex
	updates []map[string]any
}

func openBridge(t *testing.T, prompt string, servers []MCPServer) *bridgeFixture {
	t.Helper()
	root := t.TempDir()
	fixture := &bridgeFixture{root: root, launch: filepath.Join(root, "launch.log")}
	fake := fakeLaunch(t, fixture.launch)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	conn := OpenACPConn(WithLaunchContext(ctx, prompt, root), Config{
		Command: fake.Command,
		Env:     fake.Env,
		Profile: NewProfile(root),
		Servers: func(context.Context) ([]MCPServer, error) { return servers, nil },
	})
	fixture.peer = jsonrpc.NewPeer(conn, jsonrpc.HandlerFunc(fixture.handle))
	go func() { _ = fixture.peer.Serve(ctx) }()
	t.Cleanup(func() { _ = conn.Close() })
	return fixture
}

// handle records the session updates the bridge publishes.
func (f *bridgeFixture) handle(_ context.Context, req jsonrpc.Request) (json.RawMessage, *jsonrpc.Error) {
	if req.Method != acpschema.ClientMethodSessionUpdate {
		return nil, jsonrpc.MethodNotFound(req.Method)
	}
	var note struct {
		Update map[string]any `json:"update"`
	}
	if json.Unmarshal(req.Params, &note) == nil {
		f.mu.Lock()
		f.updates = append(f.updates, note.Update)
		f.mu.Unlock()
	}
	return jsonrpc.EncodeResult(map[string]any{})
}

func (f *bridgeFixture) sessionUpdates() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.updates...)
}

func (f *bridgeFixture) initialize(t *testing.T) {
	t.Helper()
	raw, err := f.peer.Call(context.Background(), acpschema.AgentMethodInitialize, acpschema.InitializeRequest{
		ProtocolVersion: acpschema.ProtocolVersion(acpschema.ProtocolVersionNumber),
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	var response acpschema.InitializeResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if response.AgentCapabilities == nil || !response.AgentCapabilities.LoadSession {
		t.Fatalf("pi bridge must advertise session restore: %s", raw)
	}
}

// newSession opens a thread and returns its id, checking that pi's own model
// and thinking level reached the ACP side as config options.
func (f *bridgeFixture) newSession(t *testing.T) string {
	t.Helper()
	raw, err := f.peer.Call(context.Background(), acpschema.AgentMethodSessionNew, map[string]any{"cwd": f.root})
	if err != nil {
		t.Fatalf("session/new: %v", err)
	}
	var response struct {
		SessionID     string `json:"sessionId"`
		ConfigOptions []struct {
			ID           string `json:"id"`
			Category     string `json:"category"`
			CurrentValue string `json:"currentValue"`
			Options      []struct {
				Value string `json:"value"`
			} `json:"options"`
		} `json:"configOptions"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if response.SessionID == "" || len(response.ConfigOptions) != 2 {
		t.Fatalf("session/new = %s", raw)
	}
	seen := map[string]string{}
	for _, option := range response.ConfigOptions {
		values := make([]string, 0, len(option.Options))
		for _, value := range option.Options {
			values = append(values, value.Value)
		}
		seen[option.Category] = option.CurrentValue + "|" + strings.Join(values, ",")
	}
	if seen["model"] != "fake/fake-model|fake/fake-model" {
		t.Fatalf("model option = %q, want pi's own model", seen["model"])
	}
	if seen["thought_level"] != "high|low,high" {
		t.Fatalf("thought level option = %q, want the levels pi advertises", seen["thought_level"])
	}
	return response.SessionID
}

// TestBridgeRunsATurn checks that a prompt reaches pi and that its stream
// reaches the ACP side as assistant text, thinking and tool work.
func TestBridgeRunsATurn(t *testing.T) {
	fixture := openBridge(t, "Jaz platform prompt", nil)
	fixture.initialize(t)
	sessionID := fixture.newSession(t)

	raw, _ := json.Marshal([]acpschema.ContentBlock{textBlock(t, "hello")})
	response, err := fixture.peer.Call(context.Background(), acpschema.AgentMethodSessionPrompt, map[string]any{
		"sessionId": sessionID,
		"prompt":    json.RawMessage(raw),
	})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if !strings.Contains(string(response), string(acpschema.StopReasonEndTurn)) {
		t.Fatalf("prompt response = %s", response)
	}

	var text, thought strings.Builder
	var toolStatus string
	for _, update := range fixture.sessionUpdates() {
		content, _ := update["content"].(map[string]any)
		switch update["sessionUpdate"] {
		case "agent_message_chunk":
			text.WriteString(stringValue(content, "text"))
		case "agent_thought_chunk":
			thought.WriteString(stringValue(content, "text"))
		case "tool_call", "tool_call_update":
			toolStatus, _ = update["status"].(string)
		}
	}
	if got := text.String(); got != "fake says hello" {
		t.Fatalf("assistant text = %q", got)
	}
	if got := thought.String(); got != "fake thinking" {
		t.Fatalf("thinking text = %q", got)
	}
	if toolStatus != "completed" {
		t.Fatalf("tool status = %q", toolStatus)
	}
}

// TestBridgeAppliesModelAndThinkingLevel checks that the ACP side's choices
// reach pi through pi's own commands.
func TestBridgeAppliesModelAndThinkingLevel(t *testing.T) {
	fixture := openBridge(t, "", nil)
	fixture.initialize(t)
	sessionID := fixture.newSession(t)

	for _, choice := range []struct{ id, value string }{
		{"model", "fake/fake-model"},
		{"thought_level", "low"},
	} {
		value, _ := json.Marshal(choice.value)
		if _, err := fixture.peer.Call(context.Background(), acpschema.AgentMethodSessionSetConfigOption, map[string]any{
			"sessionId": sessionID,
			"configId":  choice.id,
			"value":     json.RawMessage(value),
		}); err != nil {
			t.Fatalf("set %s: %v", choice.id, err)
		}
	}
	raw, err := fixture.peer.Call(context.Background(), acpschema.AgentMethodSessionSetConfigOption, map[string]any{
		"sessionId": sessionID,
		"configId":  "thought_level",
		"value":     json.RawMessage(`"low"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"currentValue":"low"`) {
		t.Fatalf("set_config_option must report pi's state: %s", raw)
	}
}

// TestBridgeCompactsWithPi checks that a compact asks pi to compact instead of
// sending the words as a prompt, which pi would treat as chat.
func TestBridgeCompactsWithPi(t *testing.T) {
	fixture := openBridge(t, "", nil)
	fixture.initialize(t)
	sessionID := fixture.newSession(t)

	raw, _ := json.Marshal([]acpschema.ContentBlock{textBlock(t, compactPrompt)})
	if _, err := fixture.peer.Call(context.Background(), acpschema.AgentMethodSessionPrompt, map[string]any{
		"sessionId": sessionID,
		"prompt":    json.RawMessage(raw),
	}); err != nil {
		t.Fatalf("compact: %v", err)
	}
	for _, update := range fixture.sessionUpdates() {
		if update["sessionUpdate"] == "agent_message_chunk" {
			t.Fatalf("compact must not start a chat turn: %#v", update)
		}
	}
}

// TestBridgeRestoresAThread checks that a resumed thread reopens the session
// file pi wrote, so provider history survives a restart.
func TestBridgeRestoresAThread(t *testing.T) {
	fixture := openBridge(t, "", nil)
	fixture.initialize(t)
	sessionID := fixture.newSession(t)

	if _, err := fixture.peer.Call(context.Background(), acpschema.AgentMethodSessionResume, map[string]any{
		"sessionId": sessionID,
		"cwd":       fixture.root,
	}); err != nil {
		t.Fatalf("session/resume: %v", err)
	}
	launch := readLines(t, fixture.launch)
	if len(launch) != 1 {
		t.Fatalf("one thread keeps one pi process, launches = %v", launch)
	}
	sessionFile := filepath.Join(fixture.root, "acp", "pi", "sessions", safeSessionName(sessionID), "session.jsonl")
	if !strings.Contains(launch[0], "--session "+sessionFile) {
		t.Fatalf("pi must run on the thread's own session file: %s", launch[0])
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := readFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(data), "\n")
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

// TestProfileKeepsPiOutOfTheUsersHome checks that the launch names Jaz's own
// profile, appends Jaz's prompt, and registers Jaz's servers with pi's client.
func TestProfileKeepsPiOutOfTheUsersHome(t *testing.T) {
	root := t.TempDir()
	profile := NewProfile(root)
	launch, err := profile.launch(Config{}, "thread-1", root, "Jaz prompt", []MCPServer{
		{Name: "jaztools", URL: "http://127.0.0.1:1/mcp/jaztools", Headers: map[string]string{"Authorization": "Bearer k"}},
		{Name: "bad name", URL: "http://127.0.0.1:1/x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(launch.Args, " ")
	prompt := filepath.Join(profile.Sessions, "thread-1", "append-prompt.md")
	extension := filepath.Join(profile.Sessions, "thread-1", "mcp.ts")
	if !strings.Contains(joined, "--append-system-prompt "+prompt) {
		t.Fatalf("launch must append Jaz's prompt: %v", launch.Args)
	}
	if !strings.Contains(joined, "--extension "+extension) {
		t.Fatalf("launch must load Jaz's MCP registration: %v", launch.Args)
	}
	promptText, err := readFile(prompt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(promptText, "Jaz prompt") || !strings.Contains(promptText, "mcp__jaztools__ask_user") {
		t.Fatalf("prompt = %q", promptText)
	}
	registered, err := readFile(extension)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(registered, `pi.registerMcpServer("jaztools"`) || strings.Contains(registered, "bad name") {
		t.Fatalf("extension = %s", registered)
	}
}

func stringValue(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func textBlock(t *testing.T, text string) acpschema.ContentBlock {
	t.Helper()
	raw, err := json.Marshal(acpschema.TextContentBlock{
		Kind:        acpschema.ContentBlockText,
		TextContent: acpschema.TextContent{Text: text},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

var _ = time.Second
