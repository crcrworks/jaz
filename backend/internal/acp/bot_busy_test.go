package acp_test

import (
	"slices"
	"testing"

	"github.com/wins/jaz/backend/internal/acp"
	"github.com/wins/jaz/backend/internal/bots"
	"github.com/wins/jaz/backend/internal/loops"
	"github.com/wins/jaz/backend/internal/sessionevents"
	"github.com/wins/jaz/backend/internal/storage"
)

// busy reads a bot's list entry.
func busy(t *testing.T, service *bots.Service, id string) func() bool {
	t.Helper()
	return func() bool {
		t.Helper()
		loaded, err := service.Load(id)
		if err != nil {
			t.Fatal(err)
		}
		return loaded.Busy
	}
}

// A bot with no turn of its own reads busy while the child thread it started
// works.
func TestABotReadsBusyWhileItsWorkerTurns(t *testing.T) {
	store, root := newTestStore(t)
	p := startJaz(t, testContext(t), store, root, map[string]string{})
	bot, err := p.bots.Create(p.ctx, bots.CreateBot{Name: "Research"})
	if err != nil {
		t.Fatal(err)
	}
	reading := busy(t, p.bots, bot.ID)
	if reading() {
		t.Fatal("a new bot reads busy")
	}
	worker, err := p.manager.Spawn(p.ctx, acp.SpawnRequest{Slug: "worker", ParentID: bot.ID, SourceType: storage.SourceBotWorker, SourceID: bot.ID, Commitment: owe(t, store, bot.ID)})
	if err != nil {
		t.Fatal(err)
	}
	p.busy(worker.SessionID)
	if session, err := store.LoadSession(bot.ID); err != nil || session.Status == storage.StatusRunning || !reading() {
		t.Fatalf("bot = %+v, %v; want it busy while its worker works and its own thread free", session, err)
	}
}

// The turns the list animates follow the real manager's own events: a worker's
// start and finish each reach the bot's stream as a change carrying busy.
func TestTheBotStreamCarriesAWorkersStartAndFinish(t *testing.T) {
	store, root := newTestStore(t)
	p := startJaz(t, testContext(t), store, root, map[string]string{})
	bot, err := p.bots.Create(p.ctx, bots.CreateBot{Name: "Research"})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := p.manager.Spawn(p.ctx, acp.SpawnRequest{Slug: "worker", ParentID: bot.ID, SourceType: storage.SourceBotWorker, SourceID: bot.ID, Commitment: owe(t, store, bot.ID)})
	if err != nil {
		t.Fatal(err)
	}
	busy := make(chan bool, 16)
	events := p.events.Subscribe(p.ctx, "", sessionevents.TypeSession, "acp")
	go func() {
		for event := range events {
			if slices.Contains(p.bots.ChangedBots(event), bot.ID) {
				loaded, err := p.bots.Load(bot.ID)
				if err == nil {
					busy <- loaded.Busy
				}
			}
		}
	}()
	p.busy(worker.SessionID)
	if got := <-busy; !got {
		t.Fatal("a worker's turn starting reached the bot as idle")
	}
	p.release()
	for got := range busy {
		if !got {
			return
		}
	}
	t.Fatal("a worker's turn finishing never reached the bot")
}

// A routine run in a thread of its own is the bot's work too.
func TestABotReadsBusyWhileARoutineRuns(t *testing.T) {
	store, root := newTestStore(t)
	p := startJaz(t, testContext(t), store, root, map[string]string{})
	bot := p.bot("Gimli")
	if err := p.store.SaveBot(storage.BotRecord{ThreadID: bot.ID, Kind: "bot"}); err != nil {
		t.Fatal(err)
	}
	routine := p.routine(loops.CreateLoop{Name: "Inbox", Prompt: "sync until released", BotID: bot.ID})
	reading := busy(t, p.bots, bot.ID)
	if reading() {
		t.Fatal("a bot with no routine running reads busy")
	}
	run, err := p.loops.RunNow(p.ctx, routine.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitForBlockingTool(t, p.ctx, p.manager, p.waitRun(run.ID, loops.RunStatusRunning).ThreadID)
	if session, err := store.LoadSession(bot.ID); err != nil || session.Status == storage.StatusRunning || !reading() {
		t.Fatalf("bot = %+v, %v; want it busy while its routine runs and its own thread free", session, err)
	}
}
