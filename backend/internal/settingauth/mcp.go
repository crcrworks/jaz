package settingauth

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wins/jaz/backend/internal/mcpsession"
)

type MCPTools struct {
	service *Service
}

func NewMCPTools(service *Service) *MCPTools {
	return &MCPTools{service: service}
}

func (t *MCPTools) AddTo(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "settings_list",
		Title:       "List Jaz settings",
		Description: "List the Jaz settings you may see, with their scope, risk, whether they are secret, and how they are changed. A secret setting shows that it exists, never its value.",
	}, t.List)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "settings_read",
		Title:       "Read a Jaz setting",
		Description: "Read one setting's metadata and, unless it is secret, the value it holds now. An app-wide setting nobody has changed yet reads as null and runs on Jaz's default.",
	}, t.Read)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "settings_update",
		Title:       "Change a Jaz setting",
		Description: "Change one setting. Your own settings apply at once; a Jaz-wide setting opens an approval card for the user and applies only if they approve. A change that would widen your own or another bot's reach is refused before any approval.",
	}, t.Update)
}

type MCPListInput struct{}

type MCPListOutput struct {
	Settings []Info `json:"settings"`
}

func (t *MCPTools) List(_ context.Context, req *mcp.CallToolRequest, _ MCPListInput) (*mcp.CallToolResult, MCPListOutput, error) {
	settings, err := t.service.List(mcpsession.SessionID(req))
	return nil, MCPListOutput{Settings: settings}, err
}

type MCPReadInput struct {
	Key string `json:"key" jsonschema:"Setting key from settings_list."`
}

func (t *MCPTools) Read(_ context.Context, req *mcp.CallToolRequest, input MCPReadInput) (*mcp.CallToolResult, View, error) {
	view, err := t.service.Read(mcpsession.SessionID(req), input.Key)
	return nil, view, err
}

func (t *MCPTools) Update(ctx context.Context, req *mcp.CallToolRequest, input UpdateInput) (*mcp.CallToolResult, UpdateOutput, error) {
	out, err := t.service.Update(ctx, mcpsession.SessionID(req), input)
	return nil, out, err
}
