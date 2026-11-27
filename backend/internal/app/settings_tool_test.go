package app

import (
	"context"
	"io"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/wins/jaz/backend/internal/acp"
	"github.com/wins/jaz/backend/internal/bots"
	"github.com/wins/jaz/backend/internal/loops"
	"github.com/wins/jaz/backend/internal/sessionevents"
	"github.com/wins/jaz/backend/internal/settingauth"
	"github.com/wins/jaz/backend/internal/storage"
	sqlitestore "github.com/wins/jaz/backend/internal/storage/sqlite"
)

// The adapter is what production runs: a plugin set restored to "inherit every
// installed plugin" has to reach the bot service, not stop at the tool.
func TestSettingAuthBotsRestoresInheritedPlugins(t *testing.T) {
	store, err := sqlitestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	session, err := store.CreateSession(storage.CreateSession{Slug: "gimli", Title: "Gimli", SourceType: storage.SourceBot})
	if err != nil {
		t.Fatal(err)
	}
	narrowed := []string{"jaz-tools"}
	if err := store.SaveBot(storage.BotRecord{ThreadID: session.ID, Kind: bots.KindBot, Plugins: &narrowed}); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard)
	service := bots.NewService(store, t.TempDir(), idleThreads{}, nil, nil, loops.NewService(store, nil, logger), sessionevents.New(), logger)
	adapter := settingAuthBots{service: service}

	bot, err := adapter.Update(context.Background(), session.ID, settingauth.BotUpdate{InheritPlugins: true})
	if err != nil {
		t.Fatal(err)
	}
	if bot.Plugins != nil {
		t.Fatalf("plugins after restoring inheritance = %v", *bot.Plugins)
	}
	stored, err := store.LoadBot(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Plugins != nil {
		t.Fatalf("stored plugins = %v", *stored.Plugins)
	}
}

// idleThreads is the agent runtime doing nothing: this test changes a stored
// bot field that never reaches a running agent.
type idleThreads struct{}

func (idleThreads) CreateSession(context.Context, acp.SpawnRequest) (storage.Session, error) {
	return storage.Session{}, nil
}

func (idleThreads) SteerInternal(context.Context, string, string, bool) (acp.Job, error) {
	return acp.Job{}, nil
}

func (idleThreads) SwitchAgent(context.Context, string, string) error { return nil }

func (idleThreads) SetModel(context.Context, string, string, string) error { return nil }

func (idleThreads) AwaitsAnswer(string) bool { return false }

func (idleThreads) Thinking(string) bool { return false }
