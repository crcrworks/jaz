package settingauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wins/jaz/backend/internal/mcpsession"
)

// sessionTransport hands the tools the Jaz thread their calls come from, as
// the real client does with its session header.
type sessionTransport struct {
	sessionID string
}

func (t sessionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set(mcpsession.HeaderName, t.sessionID)
	return http.DefaultTransport.RoundTrip(req)
}

func connectTools(t *testing.T, service *Service, sessionID string) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "jaztools", Version: "1"}, nil)
	NewMCPTools(service).AddTo(server)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	client, err := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   httpServer.URL,
		HTTPClient: &http.Client{Transport: sessionTransport{sessionID: sessionID}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestMCPToolsChangeTheCallingBotsSettings(t *testing.T) {
	fixture := newSettingsFixture(t, &fakeApprovals{})
	client := connectTools(t, fixture.service, fixture.thread.ID)
	ctx := context.Background()

	call, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "settings_update", Arguments: map[string]any{
		"key": "bot.prompt", "value": `"Answer in bullet points"`,
	}})
	if err != nil || call.IsError {
		t.Fatalf("settings_update = %+v, %v", call, err)
	}
	if bot, _ := fixture.bots.Load(fixture.bot.ID); bot.Prompt != "Answer in bullet points" {
		t.Fatalf("prompt = %q", bot.Prompt)
	}
	call, err = client.CallTool(ctx, &mcp.CallToolParams{Name: "settings_update", Arguments: map[string]any{
		"key": "providers.custom", "value": `{"providers":[]}`,
	}})
	if err != nil || !call.IsError {
		t.Fatalf("credential change was not refused: %+v, %v", call, err)
	}
	call, err = client.CallTool(ctx, &mcp.CallToolParams{Name: "settings_list", Arguments: map[string]any{}})
	if err != nil || call.IsError {
		t.Fatalf("settings_list = %+v, %v", call, err)
	}
	raw, err := json.Marshal(call.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var listed MCPListOutput
	if err := json.Unmarshal(raw, &listed); err != nil || len(listed.Settings) == 0 {
		t.Fatalf("listed = %s, %v", raw, err)
	}
}
