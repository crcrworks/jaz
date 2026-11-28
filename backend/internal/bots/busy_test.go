package bots

import (
	"testing"
	"time"

	"github.com/wins/jaz/backend/internal/storage"
)

// A bot reads busy while one of its child threads runs, not only while its own
// turn does, and reads idle again when the child finishes.
func TestABotIsBusyWhileAChildThreadRuns(t *testing.T) {
	service, store, _ := newConfiguredBotService(t, t.TempDir())
	bot, err := service.Create(t.Context(), CreateBot{Name: "Research"})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := store.CreateSession(storage.CreateSession{Slug: "worker", ParentID: bot.ID, SourceType: storage.SourceBotWorker, SourceID: bot.ID})
	if err != nil {
		t.Fatal(err)
	}
	busy := func() bool {
		t.Helper()
		loaded, err := service.Load(bot.ID)
		if err != nil {
			t.Fatal(err)
		}
		return loaded.Busy
	}
	if busy() {
		t.Fatal("a bot with nothing running reads busy")
	}
	if err := store.StartSessionTurn(worker.ID, storage.Turn{StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if !busy() {
		t.Fatal("a bot whose child thread runs reads idle")
	}
	if err := store.FinishSessionTurn(worker.ID, storage.StatusIdle, "", time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	if busy() {
		t.Fatal("a bot whose child thread finished reads busy")
	}
}
