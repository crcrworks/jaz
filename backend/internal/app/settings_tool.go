package app

import (
	"context"

	"github.com/wins/jaz/backend/internal/acp"
	"github.com/wins/jaz/backend/internal/bots"
	"github.com/wins/jaz/backend/internal/sessionevents"
	"github.com/wins/jaz/backend/internal/settingauth"
	sqlitestore "github.com/wins/jaz/backend/internal/storage/sqlite"
)

// NewSettingAuth serves a bot's settings tools: what it may see, what it may
// change alone, and what waits for the user.
func NewSettingAuth(store *sqlitestore.Store, botService *bots.Service, events *sessionevents.Bus, manager *acp.Manager) *settingauth.MCPTools {
	return settingauth.NewMCPTools(settingauth.New(settingauth.Dependencies{
		Settings:  store,
		Changes:   store,
		Bots:      settingAuthBots{service: botService},
		Events:    store,
		Bus:       events,
		Sessions:  store,
		Approvals: manager,
	}))
}

// settingAuthBots narrows the bot service to the fields the settings tool
// owns, so the tool cannot reach a bot field it does not serve.
type settingAuthBots struct {
	service *bots.Service
}

func (b settingAuthBots) Load(id string) (settingauth.Bot, error) {
	bot, err := b.service.Load(id)
	if err != nil {
		return settingauth.Bot{}, err
	}
	return settingauth.Bot{
		ID: bot.ID, Name: bot.Name, Prompt: bot.Prompt, Agent: bot.Agent, Model: bot.Model,
		ReasoningEffort: bot.ReasoningEffort, Plugins: bot.Plugins, Workspaces: bot.Workspaces,
	}, nil
}

func (b settingAuthBots) Update(ctx context.Context, id string, input settingauth.BotUpdate) (settingauth.Bot, error) {
	if _, err := b.service.Update(ctx, id, bots.UpdateBot{
		Name: input.Name, Prompt: input.Prompt, Agent: input.Agent, Model: input.Model,
		ReasoningEffort: input.ReasoningEffort, Plugins: input.Plugins, Workspaces: input.Workspaces,
	}); err != nil {
		return settingauth.Bot{}, err
	}
	return b.Load(id)
}
