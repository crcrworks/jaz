package settingauth

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wins/jaz/backend/internal/sessionevents"
	"github.com/wins/jaz/backend/internal/storage"
	sqlitestore "github.com/wins/jaz/backend/internal/storage/sqlite"
)

type fakeApprovals struct {
	approve bool
	asked   []string
}

func (f *fakeApprovals) RequestApproval(_ context.Context, _, title, _ string, _ []sessionevents.ACPPermissionLocation) (bool, error) {
	f.asked = append(f.asked, title)
	return f.approve, nil
}

type settingsFixture struct {
	t       *testing.T
	store   *sqlitestore.Store
	bots    *fakeBots
	service *Service
	bot     Bot
	thread  storage.Session
	events  *sessionevents.Bus
}

// fakeBots stands in for the bot service: storage boundaries are faked, the
// settings policy under test is real.
type fakeBots struct {
	bot Bot
}

func (b *fakeBots) Load(string) (Bot, error) {
	return b.bot, nil
}

func (b *fakeBots) Update(_ context.Context, _ string, input BotUpdate) (Bot, error) {
	if input.Name != nil {
		b.bot.Name = strings.TrimSpace(*input.Name)
	}
	if input.Prompt != nil {
		b.bot.Prompt = strings.TrimSpace(*input.Prompt)
	}
	if input.Agent != nil {
		b.bot.Agent = strings.TrimSpace(*input.Agent)
	}
	if input.Model != nil {
		b.bot.Model = strings.TrimSpace(*input.Model)
		b.bot.ReasoningEffort = input.ReasoningEffort
	}
	if input.Plugins != nil {
		b.bot.Plugins = input.Plugins
	}
	if input.Workspaces != nil {
		for _, path := range *input.Workspaces {
			if !filepath.IsAbs(path) {
				return Bot{}, errors.New("workspace must be an absolute server path")
			}
		}
		b.bot.Workspaces = *input.Workspaces
	}
	return b.bot, nil
}

func newSettingsFixture(t *testing.T, approvals *fakeApprovals) *settingsFixture {
	t.Helper()
	store, err := sqlitestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	bot, err := store.CreateSession(storage.CreateSession{Slug: "gimli", Title: "Gimli", SourceType: storage.SourceBot})
	if err != nil {
		t.Fatal(err)
	}
	bots := &fakeBots{bot: Bot{ID: bot.ID, Name: "Gimli", Prompt: "Hold the line"}}
	events := sessionevents.New()
	service := New(Dependencies{
		Settings: store, Changes: store, Bots: bots,
		Events: store, Bus: events, Sessions: store, Approvals: approvals,
	})
	thread, err := store.CreateSession(storage.CreateSession{Slug: "worker", SourceType: storage.SourceBotWorker, SourceID: bot.ID})
	if err != nil {
		t.Fatal(err)
	}
	return &settingsFixture{
		t: t, store: store, bots: bots, service: service, thread: thread, events: events, bot: bots.bot,
	}
}

func (f *settingsFixture) update(input UpdateInput) (UpdateOutput, error) {
	f.t.Helper()
	return f.service.Update(context.Background(), f.thread.ID, input)
}

func (f *settingsFixture) audit() []storage.SettingChange {
	f.t.Helper()
	rows, err := f.store.ListSettingChanges(f.bot.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return rows
}

func TestBotLocalSettingAppliesAndIsRecorded(t *testing.T) {
	approvals := &fakeApprovals{}
	fixture := newSettingsFixture(t, approvals)
	out, err := fixture.update(UpdateInput{Key: "bot.prompt", Value: `"Speak in short sentences"`})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Applied || out.Denied {
		t.Fatalf("bot-local update = %+v", out)
	}
	if len(approvals.asked) != 0 {
		t.Fatalf("bot-local update asked for approval: %v", approvals.asked)
	}
	if bot, _ := fixture.bots.Load(fixture.bot.ID); bot.Prompt != "Speak in short sentences" {
		t.Fatalf("prompt = %q", bot.Prompt)
	}
	rows := fixture.audit()
	if len(rows) != 1 || rows[0].ApprovalRequired || rows[0].Error != "" || rows[0].SettingKey != "bot.prompt" {
		t.Fatalf("audit = %+v", rows)
	}
	events, err := fixture.store.LoadSessionEvents(fixture.bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.BotActivity != nil && event.BotActivity.Kind == sessionevents.BotActivitySettingChanged && event.BotActivity.Label == "bot.prompt" {
			found = true
		}
	}
	if !found {
		t.Fatal("a bot-local change left no activity row")
	}
}

func TestAppWideSettingWaitsForApproval(t *testing.T) {
	for _, approve := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "approved"}[approve], func(t *testing.T) {
			approvals := &fakeApprovals{approve: approve}
			fixture := newSettingsFixture(t, approvals)
			out, err := fixture.update(UpdateInput{Key: "memory.settings", Value: `{"enabled":false}`, Reason: "the user asked for quiet"})
			if err != nil {
				t.Fatal(err)
			}
			if len(approvals.asked) != 1 {
				t.Fatalf("approval cards = %v", approvals.asked)
			}
			if out.Applied != approve || out.Denied != !approve || out.Approved != approve {
				t.Fatalf("app-wide update = %+v", out)
			}
			_, err = fixture.store.LoadSetting("memory", "settings")
			if applied := err == nil; applied != approve {
				t.Fatalf("memory.settings stored = %v, want %v", applied, approve)
			}
			rows := fixture.audit()
			if len(rows) != 1 || !rows[0].ApprovalRequired {
				t.Fatalf("audit = %+v", rows)
			}
			want := storage.SettingApprovalDenied
			if approve {
				want = storage.SettingApprovalApproved
			}
			if rows[0].ApprovalResult != want {
				t.Fatalf("approval result = %q, want %q", rows[0].ApprovalResult, want)
			}
		})
	}
}

func TestCredentialSettingsNeverReachTheModel(t *testing.T) {
	fixture := newSettingsFixture(t, &fakeApprovals{})
	if _, err := fixture.store.SaveSetting("providers", "custom", json.RawMessage(`{"providers":[{"id":"work","api_key":"sk-live-1234"}]}`)); err != nil {
		t.Fatal(err)
	}
	list, err := fixture.service.List(fixture.thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range list {
		if setting.Key == "providers.custom" && (!setting.Secret || setting.Editable || setting.NeedsReview) {
			t.Fatalf("providers.custom = %+v", setting)
		}
	}
	view, err := fixture.service.Read(fixture.thread.ID, "providers.custom")
	if err != nil {
		t.Fatal(err)
	}
	if view.Value != "" {
		t.Fatalf("a secret setting read its value back: %q", view.Value)
	}
	out, err := fixture.update(UpdateInput{Key: "providers.custom", Value: `{"providers":[]}`})
	if err == nil {
		t.Fatalf("a bot changed provider credentials without approval: %+v", out)
	}
	rows := fixture.audit()
	if len(rows) != 1 || rows[0].Error == "" {
		t.Fatalf("refusal audit = %+v", rows)
	}
	if strings.Contains(rows[0].Error, "sk-live-1234") || strings.Contains(rows[0].OldValue, "sk-live-1234") || strings.Contains(rows[0].NewValue, "sk-live-1234") {
		t.Fatalf("audit leaked a secret: %+v", rows[0])
	}
}

func TestBotCannotWidenItsOwnReach(t *testing.T) {
	fixture := newSettingsFixture(t, &fakeApprovals{approve: true})
	dir := t.TempDir()
	for key, value := range map[string]string{
		"bot.workspaces": `["` + dir + `"]`,
		"bot.plugins":    `["jira"]`,
	} {
		_, err := fixture.update(UpdateInput{Key: key, Value: value})
		if !errors.Is(err, ErrNotAllowed) {
			t.Fatalf("%s widened a bot's reach: %v", key, err)
		}
	}
	rows := fixture.audit()
	if len(rows) != 2 {
		t.Fatalf("refusals were not audited: %+v", rows)
	}
	bot, _ := fixture.bots.Load(fixture.bot.ID)
	if len(bot.Workspaces) != 0 || bot.Plugins != nil {
		t.Fatalf("a refused widening changed the bot: %+v", bot)
	}
}

func TestValidationErrorsComeBeforeAnyApproval(t *testing.T) {
	approvals := &fakeApprovals{approve: true}
	fixture := newSettingsFixture(t, approvals)
	if _, err := fixture.update(UpdateInput{Key: "memory.settings", Value: `not json`}); err == nil {
		t.Fatal("invalid JSON was accepted")
	}
	if _, err := fixture.update(UpdateInput{Key: "nope.nope", Value: `1`}); err == nil {
		t.Fatal("an unknown setting was accepted")
	}
	if len(approvals.asked) != 0 {
		t.Fatalf("invalid changes opened approval cards: %v", approvals.asked)
	}
	for _, row := range fixture.audit() {
		if row.Error == "" {
			t.Fatalf("validation error not audited: %+v", row)
		}
	}
}

func TestListReportsEverySettingWithMetadata(t *testing.T) {
	fixture := newSettingsFixture(t, &fakeApprovals{})
	list, err := fixture.service.List(fixture.thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	scopes := map[string]bool{}
	for _, setting := range list {
		if setting.Key == "" || setting.Description == "" || setting.Scope == "" || setting.Risk == "" {
			t.Fatalf("setting without metadata: %+v", setting)
		}
		scopes[setting.Scope] = true
	}
	if !reflect.DeepEqual(scopes, map[string]bool{ScopeBot: true, ScopeAppGlobal: true}) {
		t.Fatalf("scopes = %v", scopes)
	}
	view, err := fixture.service.Read(fixture.thread.ID, "bot.name")
	if err != nil || view.Value != `"Gimli"` {
		t.Fatalf("bot.name = %+v, %v", view, err)
	}
}
