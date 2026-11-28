package bots

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/wins/jaz/backend/internal/acp"
	botcore "github.com/wins/jaz/backend/internal/bots"
	"github.com/wins/jaz/backend/internal/loops"
	"github.com/wins/jaz/backend/internal/sessionevents"
	"github.com/wins/jaz/backend/internal/storage"
	sqlitestore "github.com/wins/jaz/backend/internal/storage/sqlite"
)

// botStream is the bots SSE endpoint with its handler's own service, store and
// bus, and next reads the next event it sends.
type botStream struct {
	store   *sqlitestore.Store
	service *botcore.Service
	bus     *sessionevents.Bus
	next    func(kind string, into any)
}

func openBotStream(t *testing.T) botStream {
	t.Helper()
	root := t.TempDir()
	store, err := sqlitestore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	logger := log.New(io.Discard)
	bus := sessionevents.New()
	manager := acp.NewManager(store, acp.Config{Root: root, Agents: map[string]acp.AgentConfig{"fixture": {Command: "unused"}}}, logger)
	t.Cleanup(manager.Close)
	service := botcore.NewService(store, filepath.Join(root, "bots"), manager, nil, nil, loops.NewService(store, nil, logger), bus, logger)
	httpServer := httptest.NewServer(http.HandlerFunc(NewHandler(service, bus).Events))
	t.Cleanup(httpServer.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	request, err := http.NewRequestWithContext(ctx, "GET", httpServer.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	scanner := bufio.NewScanner(response.Body)
	next := func(kind string, into any) {
		t.Helper()
		if !scanner.Scan() || scanner.Text() != "event: "+kind || !scanner.Scan() {
			t.Fatalf("expected %s, got %q: %v", kind, scanner.Text(), scanner.Err())
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), into); err != nil {
			t.Fatal(err)
		}
		if !scanner.Scan() || scanner.Text() != "" {
			t.Fatal("missing event boundary")
		}
	}
	return botStream{store: store, service: service, bus: bus, next: next}
}

func TestBotStreamSnapshotsAndPublishesChangesAcrossThreads(t *testing.T) {
	stream := openBotStream(t)
	store, service, bus, next := stream.store, stream.service, stream.bus, stream.next
	var initial listResponse
	next("snapshot", &initial)
	if len(initial.Bots) != 0 {
		t.Fatalf("initial = %+v", initial)
	}
	bot, err := service.Create(t.Context(), botcore.CreateBot{Name: "Research"})
	if err != nil {
		t.Fatal(err)
	}
	var changed botcore.Bot
	changed = botcore.Bot{}
	next("changed", &changed)
	if changed.ID != bot.ID {
		t.Fatalf("created = %+v", changed)
	}
	if err := service.Pin([]string{bot.ID}); err != nil {
		t.Fatal(err)
	}
	changed = botcore.Bot{}
	next("changed", &changed)
	if changed.Pinned != 1 {
		t.Fatalf("pin = %+v", changed)
	}
	worker, err := store.CreateSession(storage.CreateSession{Slug: "worker", ParentID: bot.ID, SourceType: storage.SourceBotWorker, SourceID: bot.ID})
	if err != nil {
		t.Fatal(err)
	}
	// Transcript bursts are irrelevant to this projection and cannot overflow it.
	for range 1000 {
		bus.Publish(sessionevents.Event{SessionID: worker.ID, Type: sessionevents.TypeACPThought})
	}
	bus.Publish(sessionevents.Event{SessionID: worker.ID, Type: "permission_request"})
	changed = botcore.Bot{}
	next("changed", &changed)
	if changed.ID != bot.ID {
		t.Fatalf("worker question refreshed %s, want %s", changed.ID, bot.ID)
	}
	peer, err := service.Create(t.Context(), botcore.CreateBot{Name: "Reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	changed = botcore.Bot{}
	next("changed", &changed)
	group, err := service.CreateGroup("Team", []string{bot.ID, peer.ID})
	if err != nil {
		t.Fatal(err)
	}
	changed = botcore.Bot{}
	next("changed", &changed)
	seat, err := store.CreateSession(storage.CreateSession{Slug: "seat", ParentID: group.ID, SourceType: storage.SourceBotMember, SourceID: bot.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMembership(storage.BotMembership{GroupID: group.ID, BotID: bot.ID, ThreadID: seat.ID}); err != nil {
		t.Fatal(err)
	}
	if err := store.StartSessionTurn(seat.ID, storage.Turn{StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	bus.Publish(sessionevents.Event{SessionID: seat.ID, Type: sessionevents.TypeSession})
	changed = botcore.Bot{}
	next("changed", &changed)
	changed = botcore.Bot{}
	next("changed", &changed)
	if changed.ID != group.ID || len(changed.Working) != 1 || changed.Working[0] != bot.ID {
		t.Fatalf("group work = %+v", changed)
	}
	if err := store.FinishSessionTurn(seat.ID, storage.StatusIdle, "", time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	bus.Publish(sessionevents.Event{SessionID: seat.ID, Type: sessionevents.TypeSession})
	changed = botcore.Bot{}
	next("changed", &changed)
	changed = botcore.Bot{}
	next("changed", &changed)
	if changed.ID != group.ID || len(changed.Working) != 0 || changed.CompletedAt.IsZero() {
		t.Fatalf("group completion = %+v", changed)
	}
	if err := service.Delete(bot.ID); err != nil {
		t.Fatal(err)
	}
	changed = botcore.Bot{}
	next("changed", &changed)
	if changed.ID != group.ID || len(changed.Members) != 1 || changed.Members[0] != peer.ID {
		t.Fatalf("group after deleting its member = %+v", changed)
	}
	var removed string
	next("removed", &removed)
	if removed != bot.ID {
		t.Fatalf("removed = %q", removed)
	}
}

// A child thread's turn starting and finishing changes its bot's one row, so
// its list entry animates and stops without a snapshot in between.
func TestBotStreamCarriesAChildTurnsStartAndFinish(t *testing.T) {
	stream := openBotStream(t)
	store, service, bus, next := stream.store, stream.service, stream.bus, stream.next
	var snapshot listResponse
	next("snapshot", &snapshot)
	bot, err := service.Create(t.Context(), botcore.CreateBot{Name: "Research"})
	if err != nil {
		t.Fatal(err)
	}
	var created botcore.Bot
	next("changed", &created)
	worker, err := store.CreateSession(storage.CreateSession{Slug: "worker", ParentID: bot.ID, SourceType: storage.SourceBotWorker, SourceID: bot.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range []struct {
		start bool
		busy  bool
		kind  string
	}{{true, true, "acp"}, {false, false, sessionevents.TypeSession}} {
		if turn.start {
			err = store.StartSessionTurn(worker.ID, storage.Turn{StartedAt: time.Now()})
		} else {
			err = store.FinishSessionTurn(worker.ID, storage.StatusIdle, "", time.Now().UTC(), nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		// A turn's start announces it in the thread it runs in, and its finish
		// reacts as the server does.
		bus.Publish(sessionevents.Event{SessionID: worker.ID, Type: turn.kind})
		var changed botcore.Bot
		next("changed", &changed)
		if changed.ID != bot.ID || changed.Busy != turn.busy {
			t.Fatalf("after start=%v changed = %+v, want bot %s busy %v", turn.start, changed, bot.ID, turn.busy)
		}
	}
}
