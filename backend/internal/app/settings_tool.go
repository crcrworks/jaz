package app

import (
	"context"
	"time"

	"github.com/wins/jaz/backend/internal/acp"
	"github.com/wins/jaz/backend/internal/bots"
	"github.com/wins/jaz/backend/internal/computercontrol"
	"github.com/wins/jaz/backend/internal/jaztools"
	mcpruntime "github.com/wins/jaz/backend/internal/mcp"
	"github.com/wins/jaz/backend/internal/memoryservice"
	"github.com/wins/jaz/backend/internal/sessionevents"
	"github.com/wins/jaz/backend/internal/settingauth"
	jazsettings "github.com/wins/jaz/backend/internal/settings"
	sqlitestore "github.com/wins/jaz/backend/internal/storage/sqlite"
)

// SettingsApplied runs what Jaz does after an app-global setting is stored
// through a bot's settings tool; the HTTP settings routes do the same work.
type SettingsApplied func(ctx context.Context, key string) error

// NewSettingsApplied is the follow-up an approved app-global change needs to
// take effect: memory's scheduler, the browser and computer tools, and the MCP
// catalog all read settings when they start or sync.
func NewSettingsApplied(store *sqlitestore.Store, memory *memoryservice.Service, computer *computercontrol.DesktopBackend, tools *jaztools.Service, mcp *mcpruntime.Manager) SettingsApplied {
	return func(_ context.Context, key string) error {
		switch key {
		case "memory.settings":
			settings, err := jazsettings.LoadMemorySettings(store)
			if err != nil {
				return err
			}
			if memory != nil && memory.Scheduler != nil {
				if settings.Enabled {
					memory.Scheduler.Start()
				} else {
					memory.Scheduler.Stop()
				}
			}
		case "computer.settings":
			if computer != nil && !jazsettings.ComputerEnabled(store) {
				_ = computer.Close()
			}
		}
		if tools != nil {
			tools.Sync()
		}
		if mcp != nil {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				mcp.Refresh(ctx)
			}()
		}
		return nil
	}
}

// NewSettingAuth serves a bot's settings tools: what it may see, what it may
// change alone, and what waits for the user.
func NewSettingAuth(store *sqlitestore.Store, botService *bots.Service, events *sessionevents.Bus, manager *acp.Manager, applied SettingsApplied) *settingauth.MCPTools {
	return settingauth.NewMCPTools(settingauth.New(settingauth.Dependencies{
		Settings:  store,
		Changes:   store,
		Bots:      settingAuthBots{service: botService},
		Events:    store,
		Bus:       events,
		Sessions:  store,
		Approvals: manager,
		Applied:   applied,
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
