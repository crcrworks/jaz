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
	jazsettings "github.com/wins/jaz/backend/internal/settings"
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
	applied []string
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
	bots := &fakeBots{bot: Bot{ID: bot.ID, Name: "Gimli", Prompt: "Hold the line", Model: "gpt-5", ReasoningEffort: "high"}}
	events := sessionevents.New()
	fixture := &settingsFixture{t: t, store: store, bots: bots, events: events, bot: bots.bot}
	fixture.service = New(Dependencies{
		Settings: store, Changes: store, Bots: bots,
		Events: store, Bus: events, Sessions: store, Approvals: approvals,
		Applied: func(_ context.Context, key string) error {
			fixture.applied = append(fixture.applied, key)
			return nil
		},
	})
	thread, err := store.CreateSession(storage.CreateSession{Slug: "worker", SourceType: storage.SourceBotWorker, SourceID: bot.ID})
	if err != nil {
		t.Fatal(err)
	}
	fixture.thread = thread
	return fixture
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

// The round trip the review found broken: reading a bot's model and writing it
// back keeps both the model and its reasoning effort.
func TestBotModelReadRoundTripsThroughValue(t *testing.T) {
	fixture := newSettingsFixture(t, &fakeApprovals{})
	view, err := fixture.service.Read(fixture.thread.ID, "bot.model")
	if err != nil {
		t.Fatal(err)
	}
	out, err := fixture.update(UpdateInput{Key: "bot.model", Value: view.Value})
	if err != nil || !out.Applied {
		t.Fatalf("round trip = %+v, %v", out, err)
	}
	if bot, _ := fixture.bots.Load(fixture.bot.ID); bot.Model != "gpt-5" || bot.ReasoningEffort != "high" {
		t.Fatalf("round trip changed the bot: model=%q effort=%q", bot.Model, bot.ReasoningEffort)
	}

	out, err = fixture.update(UpdateInput{Key: "bot.model", Value: `{"model":"o3","reasoning_effort":"low"}`})
	if err != nil || !out.Applied {
		t.Fatalf("value change = %+v, %v", out, err)
	}
	if bot, _ := fixture.bots.Load(fixture.bot.ID); bot.Model != "o3" || bot.ReasoningEffort != "low" {
		t.Fatalf("value change = model=%q effort=%q", bot.Model, bot.ReasoningEffort)
	}

	// Naming a model alone keeps the effort the bot already runs with.
	if _, err := fixture.update(UpdateInput{Key: "bot.model", Value: `{"model":"o4"}`}); err != nil {
		t.Fatal(err)
	}
	if bot, _ := fixture.bots.Load(fixture.bot.ID); bot.Model != "o4" || bot.ReasoningEffort != "low" {
		t.Fatalf("model only = model=%q effort=%q", bot.Model, bot.ReasoningEffort)
	}
}

// A value Jaz could not read back never reaches the approval card.
func TestAppGlobalValueIsValidatedBeforeApproval(t *testing.T) {
	for _, key := range []string{"memory.settings", "browser.settings", "computer.settings"} {
		approvals := &fakeApprovals{approve: true}
		fixture := newSettingsFixture(t, approvals)
		t.Run(key, func(t *testing.T) {
			if _, err := fixture.update(UpdateInput{Key: key, Value: `{"enabled":"maybe"}`}); err == nil || errors.Is(err, ErrNotAllowed) {
				t.Fatalf("%s mistyped value = %v, want a validation error", key, err)
			}
			if len(approvals.asked) != 0 {
				t.Fatalf("%s opened an approval card for an invalid value: %v", key, approvals.asked)
			}
			rows := fixture.audit()
			if len(rows) != 1 || rows[0].Error == "" || rows[0].ApprovalRequired {
				t.Fatalf("%s audit = %+v", key, rows)
			}
			if _, err := fixture.store.LoadSetting(strings.SplitN(key, ".", 2)[0], strings.SplitN(key, ".", 2)[1]); err == nil {
				t.Fatalf("%s stored the invalid value", key)
			}
		})
	}
}

func TestUnknownKeyIsAudited(t *testing.T) {
	fixture := newSettingsFixture(t, &fakeApprovals{})
	if _, err := fixture.update(UpdateInput{Key: "nope.nope", Value: `1`}); err == nil {
		t.Fatal("unknown setting accepted")
	}
	rows := fixture.audit()
	if len(rows) != 1 || rows[0].SettingKey != "nope.nope" || rows[0].Error == "" {
		t.Fatalf("audit = %+v", rows)
	}
}

// An approved app-global change runs the follow-up Jaz needs for it to take
// effect; a bot-local one does not.
func TestAppliedRunsOnlyForAppGlobalChanges(t *testing.T) {
	approvals := &fakeApprovals{approve: true}
	fixture := newSettingsFixture(t, approvals)
	if _, err := fixture.update(UpdateInput{Key: "bot.prompt", Value: `"short"`}); err != nil {
		t.Fatal(err)
	}
	if len(fixture.applied) != 0 {
		t.Fatalf("bot-local change ran follow-up work: %v", fixture.applied)
	}
	if _, err := fixture.update(UpdateInput{Key: "memory.settings", Value: `{"enabled":false}`}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fixture.applied, []string{"memory.settings"}) {
		t.Fatalf("follow-up work = %v", fixture.applied)
	}
}

// A value the tool accepts loads back through the typed loader Jaz reads.
func TestAcceptedAppGlobalValueLoadsBack(t *testing.T) {
	approvals := &fakeApprovals{approve: true}
	fixture := newSettingsFixture(t, approvals)
	if _, err := fixture.update(UpdateInput{Key: "memory.settings", Value: `{"enabled":false,"agent":"codex","model":"gpt-5"}`}); err != nil {
		t.Fatal(err)
	}
	settings, err := jazsettings.LoadMemorySettings(fixture.store)
	if err != nil || settings.Enabled || settings.Agent != "codex" || settings.Model != "gpt-5" {
		t.Fatalf("stored memory settings = %+v, %v", settings, err)
	}
	if _, err := fixture.update(UpdateInput{Key: "devices.settings", Value: `{"public_url":"https://jaz.example.com"}`}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("device access change = %v, want ErrNotAllowed", err)
	}
}

func TestNeedsReviewMeansAnApprovalCard(t *testing.T) {
	fixture := newSettingsFixture(t, &fakeApprovals{})
	list, err := fixture.service.List(fixture.thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range list {
		want := setting.Editable && setting.Scope == ScopeAppGlobal
		if setting.NeedsReview != want {
			t.Fatalf("%s needs_review = %v, want %v", setting.Key, setting.NeedsReview, want)
		}
	}
}
