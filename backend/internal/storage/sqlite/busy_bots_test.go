package sqlite

import (
	"testing"
	"time"

	"github.com/wins/jaz/backend/internal/storage"
)

// A bot is busy while a thread of its work runs, whichever kind started it,
// and stops when the last one does.
func TestBusyBotsFollowTheThreadsABotWorksIn(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	create := func(in storage.CreateSession) string {
		t.Helper()
		session, err := store.CreateSession(in)
		if err != nil {
			t.Fatal(err)
		}
		return session.ID
	}
	bot := create(storage.CreateSession{Slug: "bot", SourceType: storage.SourceBot})
	if err := store.SaveBot(storage.BotRecord{ThreadID: bot, Kind: "bot"}); err != nil {
		t.Fatal(err)
	}
	chat := create(storage.CreateSession{Slug: "chat"})
	worker := create(storage.CreateSession{ParentID: bot, SourceType: storage.SourceBotWorker, SourceID: bot})
	// A thread a subtask starts carries the bot it works for, as spawning one
	// under it does.
	grandchild := create(storage.CreateSession{ParentID: worker, SourceType: storage.SourceBotWorker, SourceID: bot})
	seat := create(storage.CreateSession{SourceType: storage.SourceBotMember, SourceID: bot})
	run := create(storage.CreateSession{ParentID: bot, SourceType: storage.SourceLoopRun, SourceID: "routine"})
	search := create(storage.CreateSession{ParentID: worker, SourceType: storage.SourceMemorySearch, SourceID: "query"})
	busy := func() []string {
		t.Helper()
		busy, err := store.BusyBots()
		if err != nil {
			t.Fatal(err)
		}
		if busy[bot] {
			return []string{bot}
		}
		return nil
	}
	if got := busy(); got != nil {
		t.Fatalf("an idle bot reads busy: %v", got)
	}
	for _, thread := range []string{bot, worker, grandchild, seat, run} {
		if err := store.StartSessionTurn(thread, storage.Turn{StartedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if got := busy(); len(got) != 1 {
			t.Fatalf("%s running leaves the bot idle", thread)
		}
		if err := store.FinishSessionTurn(thread, storage.StatusIdle, "", time.Now().UTC(), nil); err != nil {
			t.Fatal(err)
		}
		if got := busy(); got != nil {
			t.Fatalf("%s finishing leaves the bot busy", thread)
		}
	}
	// A memory worker is not the bot's work, and another chat's thread is
	// nobody's.
	for _, thread := range []string{search, chat} {
		if err := store.StartSessionTurn(thread, storage.Turn{StartedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	if got := busy(); got != nil {
		t.Fatalf("a memory worker or another chat reads busy: %v", got)
	}
	// A turn a restart left interrupted is not running.
	if err := store.StartSessionTurn(worker, storage.Turn{StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSessionStatus(worker, storage.StatusInterrupted, "", time.Time{}); err != nil {
		t.Fatal(err)
	}
	if got := busy(); got != nil {
		t.Fatalf("an interrupted turn reads busy: %v", got)
	}
	// An archived thread does not.
	if err := store.StartSessionTurn(grandchild, storage.Turn{StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetArchived(grandchild, true); err != nil {
		t.Fatal(err)
	}
	if got := busy(); got != nil {
		t.Fatalf("an archived thread reads busy: %v", got)
	}
}
