package settingauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/wins/jaz/backend/internal/acp"
	"github.com/wins/jaz/backend/internal/sessionevents"
	jazsettings "github.com/wins/jaz/backend/internal/settings"
	"github.com/wins/jaz/backend/internal/storage"
)

// ErrNotAllowed refuses a change before any approval card: the bot is asking
// for something only the user may do, whatever it has already been granted.
var ErrNotAllowed = errors.New("only the user can change this setting")

// Dependencies is what the settings tool reads and writes through.
type Dependencies struct {
	Settings  storage.SettingsStorage
	Changes   storage.SettingChangeStore
	Bots      BotSettings
	Events    storage.SessionEventAppender
	Bus       *sessionevents.Bus
	Sessions  SessionReader
	Approvals ApprovalRequester
	// Agents and Models are what a memory change is checked against: the
	// agents Jaz can actually start, and the reasoning efforts their models
	// accept. The HTTP route checks the same way through the server's own
	// catalog.
	Agents func() (jazsettings.AgentDefaults, error)
	Models acp.ModelCatalog
	// Applied runs the work Jaz does after an app-global setting is saved:
	// memory's scheduler and tool sync, browser and computer tool resync.
	// The settings tool writes the value itself, so whoever owns that work
	// supplies it here.
	Applied func(ctx context.Context, key string) error
}

type SessionReader interface {
	LoadSession(id string) (storage.Session, error)
}

// BotSettings reads and changes a bot through the bot service, so settings
// changes keep the service's validation.
type BotSettings interface {
	Load(id string) (Bot, error)
	Update(ctx context.Context, id string, input BotUpdate) (Bot, error)
}

// Bot carries only what the settings tool may read and change.
type Bot struct {
	ID              string
	Name            string
	Prompt          string
	Agent           string
	Model           string
	ReasoningEffort string
	Plugins         *[]string
	Workspaces      []string
}

type BotUpdate struct {
	Name            *string
	Prompt          *string
	Agent           *string
	Model           *string
	ReasoningEffort string
	// InheritPlugins restores the bot's inherited plugin set: a plugins value
	// of null means every installed plugin, not an empty choice.
	InheritPlugins bool
	Plugins        *[]string
	Workspaces     *[]string
}

type ApprovalRequester interface {
	RequestApproval(ctx context.Context, sessionID, title, body string, locations []sessionevents.ACPPermissionLocation) (bool, error)
}

// Service serves the settings tools.
type Service struct {
	deps Dependencies
}

func New(deps Dependencies) *Service {
	return &Service{deps: deps}
}

// Info is one setting as the tools report it. NeedsReview means the change
// opens an approval card; Editable means a bot may change it at all.
type Info struct {
	Key         string `json:"key"`
	Scope       string `json:"scope"`
	Risk        string `json:"risk"`
	Secret      bool   `json:"secret,omitempty"`
	Editable    bool   `json:"editable"`
	NeedsReview bool   `json:"needs_review,omitempty"`
	Description string `json:"description"`
}

// View is a setting's metadata and, unless it is secret, its value as JSON
// text.
type View struct {
	Info
	Value string `json:"value,omitempty"`
}

// UpdateInput is one change a bot asks for.
type UpdateInput struct {
	Key             string    `json:"key"`
	Value           string    `json:"value,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
	Name            *string   `json:"name,omitempty"`
	Prompt          *string   `json:"prompt,omitempty"`
	Agent           *string   `json:"agent,omitempty"`
	Model           *string   `json:"model,omitempty"`
	Plugins         *[]string `json:"plugins,omitempty"`
	Workspaces      *[]string `json:"workspaces,omitempty"`
}

// UpdateOutput reports what happened: applied now, waiting on nothing, or
// settled by the user's answer.
type UpdateOutput struct {
	Key      string `json:"key"`
	Applied  bool   `json:"applied"`
	Preview  string `json:"preview,omitempty"`
	Approved bool   `json:"approved,omitempty"`
	Denied   bool   `json:"denied,omitempty"`
}

// List reports every setting a bot may see.
func (s *Service) List(sessionID string) ([]Info, error) {
	if _, err := s.bot(sessionID); err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(entries))
	for _, candidate := range entries {
		out = append(out, info(candidate))
	}
	return out, nil
}

// Read reports one setting. A secret setting shows its metadata only.
func (s *Service) Read(sessionID, key string) (View, error) {
	bot, err := s.bot(sessionID)
	if err != nil {
		return View{}, err
	}
	target, ok := entry(key)
	if !ok {
		return View{}, fmt.Errorf("unknown setting: %s", key)
	}
	view := View{Info: info(target)}
	if target.Secret {
		return view, nil
	}
	value, err := s.current(bot, target)
	if err != nil {
		return View{}, err
	}
	view.Value = string(value)
	return view, nil
}

// Update applies a change. Only a change that widens a bot's reach is refused
// outright; every other change either applies now or waits for the user, and
// every attempt is audited.
func (s *Service) Update(ctx context.Context, sessionID string, input UpdateInput) (UpdateOutput, error) {
	bot, err := s.bot(sessionID)
	if err != nil {
		return UpdateOutput{}, err
	}
	target, ok := entry(input.Key)
	if !ok {
		err := fmt.Errorf("unknown setting: %s", input.Key)
		s.audit(bot, Entry{Key: strings.TrimSpace(input.Key)}, storage.SettingChange{}, err)
		return UpdateOutput{Key: strings.TrimSpace(input.Key)}, err
	}
	out := UpdateOutput{Key: target.Key}
	if !target.Editable {
		err := fmt.Errorf("%w: %s holds credentials or device access, so only the user changes it in Jaz's settings", ErrNotAllowed, target.Key)
		s.audit(bot, target, storage.SettingChange{}, err)
		return out, err
	}
	next, err := s.next(bot, target, input)
	if err != nil {
		s.audit(bot, target, storage.SettingChange{}, err)
		return out, err
	}
	// validate is what keeps a value Jaz could not read back from ever being
	// stored, so it runs before the approval card and its answer is the value
	// that gets written.
	if next, err = s.validate(target, next); err != nil {
		s.audit(bot, target, storage.SettingChange{}, err)
		return out, err
	}
	oldValue, err := s.current(bot, target)
	if err != nil {
		s.audit(bot, target, storage.SettingChange{}, err)
		return out, err
	}
	change := storage.SettingChange{
		ThreadID: bot.ID, Actor: bot.Name, SettingKey: target.Key, Scope: target.Scope, Risk: target.Risk,
		OldValue: display(target, oldValue), NewValue: display(target, next),
	}
	if target.appWide() {
		change.ApprovalRequired = true
		allowed, err := s.deps.Approvals.RequestApproval(ctx, sessionID,
			"Change "+target.Key, s.approvalBody(bot, target, change, input.Reason), nil)
		if err != nil {
			s.audit(bot, target, change, err)
			return out, err
		}
		if !allowed {
			change.ApprovalResult = storage.SettingApprovalDenied
			s.audit(bot, target, change, nil)
			out.Denied = true
			return out, nil
		}
		change.ApprovalResult = storage.SettingApprovalApproved
		out.Approved = true
	}
	// An app-wide change stores its audit row in the same transaction, so only
	// a failure or a bot-local change records one here.
	// An app-wide change stores its audit row with the value in one
	// transaction, so a failure has no row yet and records one here.
	if err := s.apply(ctx, bot, target, next, change); err != nil {
		s.audit(bot, target, change, err)
		return out, err
	}
	if !target.appWide() {
		s.audit(bot, target, change, nil)
	}
	out.Applied = true
	out.Preview = change.NewValue
	if target.appWide() && s.deps.Applied != nil {
		if err := s.deps.Applied(ctx, target.Key); err != nil {
			// The value and its audit row are stored; this is the follow-up
			// work failing, not the change.
			return out, fmt.Errorf("%s saved, but it has not taken effect yet: %w", target.Key, err)
		}
	}
	s.note(bot, target, change)
	return out, nil
}

func (s *Service) bot(sessionID string) (Bot, error) {
	session, err := s.deps.Sessions.LoadSession(strings.TrimSpace(sessionID))
	if err != nil {
		return Bot{}, err
	}
	bot, err := s.deps.Bots.Load(session.Owner())
	if err != nil {
		return Bot{}, fmt.Errorf("settings need a Jaz bot thread: %w", err)
	}
	return bot, nil
}

// current is a setting's value as JSON, redacted when it carries a secret.
func (s *Service) current(bot Bot, target Entry) (json.RawMessage, error) {
	if target.scoped() {
		return currentBotValue(bot, target)
	}
	setting, err := s.deps.Settings.LoadSetting(target.namespaceKey())
	if errors.Is(err, storage.ErrSettingNotFound) {
		return json.RawMessage("null"), nil
	}
	if err != nil {
		return nil, err
	}
	return setting.Value, nil
}

func currentBotValue(bot Bot, target Entry) (json.RawMessage, error) {
	var value any
	switch target.Key {
	case "bot.name":
		value = bot.Name
	case "bot.prompt":
		value = bot.Prompt
	case "bot.agent":
		value = bot.Agent
	case "bot.model":
		value = map[string]string{"model": bot.Model, "reasoning_effort": bot.ReasoningEffort}
	case "bot.plugins":
		value = bot.Plugins
	case "bot.workspaces":
		value = bot.Workspaces
	default:
		return nil, fmt.Errorf("unknown setting: %s", target.Key)
	}
	return json.Marshal(value)
}

// next is what the setting will hold once the change applies: the requested
// value, kept in the shape apply stores. It refuses anything that would widen a
// bot's reach over other bots.
func (s *Service) next(bot Bot, target Entry, input UpdateInput) (json.RawMessage, error) {
	value, err := requestedValue(input, target)
	if err != nil {
		return nil, err
	}
	switch target.Key {
	case "bot.plugins":
		if isNull(value) {
			// null is the inherited set, which a narrowed bot may not take
			// back: that would add plugins rather than drop them.
			if bot.Plugins != nil {
				return nil, fmt.Errorf("%w: bot.plugins can only drop plugin ids; null would restore every installed plugin", ErrNotAllowed)
			}
			return value, nil
		}
		wanted, err := stringList(value)
		if err != nil {
			return nil, errors.New("bot.plugins needs an array of plugin ids, or null to inherit every installed plugin")
		}
		if added := missingFrom(wanted, currentPlugins(bot)); len(added) > 0 {
			return nil, fmt.Errorf("%w: a bot cannot give itself plugins (%s)", ErrNotAllowed, strings.Join(added, ", "))
		}
	case "bot.workspaces":
		wanted, err := stringList(value)
		if err != nil {
			return nil, errors.New("bot.workspaces needs an array of paths")
		}
		if added := missingFrom(wanted, bot.Workspaces); len(added) > 0 {
			return nil, fmt.Errorf("%w: a bot cannot widen its workspaces (%s)", ErrNotAllowed, strings.Join(added, ", "))
		}
	case "bot.model":
		settings, err := modelValue(value)
		if err != nil {
			return nil, err
		}
		// Naming only the effort, or nothing at all, keeps the model the bot
		// already runs with: this setting is never a way to clear a model.
		if settings.Model == "" {
			settings.Model = bot.Model
		}
		if settings.ReasoningEffort == "" {
			settings.ReasoningEffort = bot.ReasoningEffort
		}
		data, err := json.Marshal(settings)
		return data, err
	}
	return value, nil
}

// modelValue is what a bot.model change asks for: the model it names and,
// optionally, the effort to run it with.
type modelValueSettings struct {
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
}

func modelValue(value json.RawMessage) (modelValueSettings, error) {
	var settings modelValueSettings
	if err := json.Unmarshal(value, &settings); err != nil {
		return modelValueSettings{}, errors.New(`bot.model needs {"model": "...", "reasoning_effort": "..."}`)
	}
	settings.Model = strings.TrimSpace(settings.Model)
	settings.ReasoningEffort = strings.TrimSpace(settings.ReasoningEffort)
	return settings, nil
}

func stringList(value json.RawMessage) ([]string, error) {
	var list []string
	err := json.Unmarshal(value, &list)
	return list, err
}

// validate reads an app-global value the way Jaz reads it back, and returns it
// in the shape that will be stored. A value Jaz could not load and use never
// reaches the approval card, and what the user approves is what gets written.
func (s *Service) validate(target Entry, value json.RawMessage) (json.RawMessage, error) {
	if target.appWide() && isNull(value) {
		// null is what an untouched setting reads as; writing it back would
		// store a zero value nobody asked for.
		return nil, fmt.Errorf("%s has no value yet; send the settings Jaz should store", target.Key)
	}
	switch target.Key {
	case "memory.settings":
		settings, err := jazsettings.ParseMemorySettings(value)
		if err != nil {
			return nil, fmt.Errorf("%s does not accept that value: %w", target.Key, err)
		}
		agents, err := s.deps.Agents()
		if err != nil {
			return nil, err
		}
		if settings, err = jazsettings.ValidateMemorySettings(settings, agents, acp.ModelCapabilities{Catalog: s.deps.Models}, true); err != nil {
			return nil, err
		}
		return json.Marshal(settings)
	case "browser.settings":
		var settings jazsettings.BrowserSettings
		return value, typedValue(value, &settings, target.Key)
	case "computer.settings":
		var settings jazsettings.ComputerSettings
		return value, typedValue(value, &settings, target.Key)
	case "devices.settings":
		var settings jazsettings.DeviceSettings
		if err := typedValue(value, &settings, target.Key); err != nil {
			return nil, err
		}
		settings, err := jazsettings.NormalizeDeviceSettings(settings)
		if err != nil {
			return nil, err
		}
		return json.Marshal(settings)
	}
	return value, nil
}

func typedValue(value json.RawMessage, out any, key string) error {
	if err := json.Unmarshal(value, out); err != nil {
		return fmt.Errorf("%s does not accept that value: %w", key, err)
	}
	return nil
}

func isNull(value json.RawMessage) bool {
	return strings.TrimSpace(string(value)) == "null"
}

func requestedValue(input UpdateInput, target Entry) (json.RawMessage, error) {
	value := strings.TrimSpace(input.Value)
	switch {
	case value != "":
	case input.Prompt != nil:
		value, _ = marshalString(*input.Prompt)
	case input.Name != nil:
		value, _ = marshalString(*input.Name)
	case input.Agent != nil:
		value, _ = marshalString(*input.Agent)
	case input.Model != nil:
		data, _ := json.Marshal(modelValueSettings{Model: strings.TrimSpace(*input.Model), ReasoningEffort: strings.TrimSpace(input.ReasoningEffort)})
		value = string(data)
	case input.Plugins != nil:
		data, _ := json.Marshal(*input.Plugins)
		value = string(data)
	case input.Workspaces != nil:
		data, _ := json.Marshal(*input.Workspaces)
		value = string(data)
	}
	if value == "" {
		return nil, fmt.Errorf("%s needs a value", target.Key)
	}
	if !json.Valid([]byte(value)) {
		return nil, fmt.Errorf("%s needs a JSON value, for example \"text\", 12, true or [\"a\"]", target.Key)
	}
	return json.RawMessage(value), nil
}

func marshalString(value string) (string, error) {
	data, err := json.Marshal(value)
	return string(data), err
}

func currentPlugins(bot Bot) []string {
	if bot.Plugins == nil {
		return nil
	}
	return *bot.Plugins
}

func missingFrom(wanted, current []string) []string {
	var missing []string
	for _, value := range wanted {
		if !slices.Contains(current, value) {
			missing = append(missing, value)
		}
	}
	return missing
}

// apply writes the change: bot fields through the bot service, app settings
// with their audit row in one transaction.
func (s *Service) apply(ctx context.Context, bot Bot, target Entry, value json.RawMessage, change storage.SettingChange) error {
	if target.scoped() {
		return s.applyBot(ctx, bot, target, value)
	}
	namespace, key := target.namespaceKey()
	return s.deps.Changes.ApplySettingChange(change, namespace, key, value)
}

func (s *Service) applyBot(ctx context.Context, bot Bot, target Entry, value json.RawMessage) error {
	update := BotUpdate{}
	var text string
	switch target.Key {
	case "bot.name", "bot.prompt", "bot.agent":
		if err := json.Unmarshal(value, &text); err != nil {
			return fmt.Errorf("%s needs JSON text", target.Key)
		}
		switch target.Key {
		case "bot.name":
			update.Name = &text
		case "bot.prompt":
			update.Prompt = &text
		case "bot.agent":
			update.Agent = &text
		}
	case "bot.model":
		model, err := modelValue(value)
		if err != nil {
			return err
		}
		if model.Model == "" {
			// A bot on its agent's default model reads with no model, so
			// writing that back changes nothing; an effort alone cannot be set.
			if model.ReasoningEffort == "" {
				return nil
			}
			return errors.New("bot.model needs a model name to go with that reasoning effort")
		}
		update.Model = &model.Model
		update.ReasoningEffort = model.ReasoningEffort
	case "bot.plugins":
		if isNull(value) {
			update.InheritPlugins = true
			break
		}
		var plugins []string
		if err := json.Unmarshal(value, &plugins); err != nil {
			return errors.New("bot.plugins needs an array of plugin ids")
		}
		update.Plugins = &plugins
	case "bot.workspaces":
		var workspaces []string
		if err := json.Unmarshal(value, &workspaces); err != nil {
			return errors.New("bot.workspaces needs an array of paths")
		}
		update.Workspaces = &workspaces
	default:
		return fmt.Errorf("unknown setting: %s", target.Key)
	}
	_, err := s.deps.Bots.Update(ctx, bot.ID, update)
	return err
}

func (s *Service) approvalBody(bot Bot, target Entry, change storage.SettingChange, reason string) string {
	var body strings.Builder
	fmt.Fprintf(&body, "%s asks to change a Jaz-wide setting.\n\n", bot.Name)
	fmt.Fprintf(&body, "- scope: %s\n- risk: %s\n\n", target.Scope, target.Risk)
	fmt.Fprintf(&body, "Before: `%s`\n\nAfter: `%s`\n", orUnset(change.OldValue), orUnset(change.NewValue))
	if reason = strings.TrimSpace(reason); reason != "" {
		fmt.Fprintf(&body, "\nWhy: %s\n", reason)
	}
	return body.String()
}

func orUnset(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(unset)"
	}
	return value
}

// audit records an attempt, with the failure that stopped it when there is
// one, so refusals and validation errors stay as visible as changes.
func (s *Service) audit(bot Bot, target Entry, change storage.SettingChange, err error) {
	change.ThreadID, change.Actor, change.SettingKey = bot.ID, bot.Name, target.Key
	change.Scope, change.Risk = target.Scope, target.Risk
	if err != nil {
		change.Error = err.Error()
	}
	_ = s.deps.Changes.RecordSettingChange(change)
}

// note puts a successful change in the bot's chat as activity.
func (s *Service) note(bot Bot, target Entry, change storage.SettingChange) {
	if s.deps.Bus == nil {
		return
	}
	activity := sessionevents.BotActivityEvent{
		Kind:  sessionevents.BotActivitySettingChanged,
		Label: target.Key,
		Text:  change.NewValue,
	}
	event := sessionevents.Event{SessionID: bot.ID, Type: sessionevents.TypeBotActivity, BotActivity: &activity}
	if s.deps.Events != nil {
		if err := s.deps.Events.AppendSessionEvents(bot.ID, event); err != nil {
			return
		}
	}
	s.deps.Bus.Publish(event)
}

// display renders a value for people: secrets stay unread, JSON text reads
// without its quotes, and long values are clipped.
func display(target Entry, value json.RawMessage) string {
	if target.Secret {
		return "(secret)"
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		return clip(text)
	}
	return clip(string(value))
}

func clip(value string) string {
	const limit = 400
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

// info reports a setting. NeedsReview is what actually opens an approval card:
// an app-global change. A permission-risk bot field stays visible as that risk
// while narrowing it applies at once, because it can only reduce the bot's
// reach.
func info(candidate Entry) Info {
	return Info{
		Key: candidate.Key, Scope: candidate.Scope, Risk: candidate.Risk, Secret: candidate.Secret,
		Editable: candidate.Editable, NeedsReview: candidate.Editable && candidate.appWide(),
		Description: candidate.Description,
	}
}
