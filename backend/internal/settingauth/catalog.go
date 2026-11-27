// Package settingauth serves a bot's view of Jaz settings: what it may read,
// what it may change on its own, and what needs the user's approval. A bot's
// device permission says only which client reached Jaz, never what it may
// change here.
package settingauth

import (
	"strings"
)

const (
	ScopeBot       = "bot-local"
	ScopeAppGlobal = "app-global"

	RiskStandard   = "standard"
	RiskSensitive  = "sensitive"
	RiskPermission = "permission"
	RiskCredential = "credential"
)

// Entry is one setting a bot can see: where it lives, how far it reaches, how
// dangerous it is to change, and how the tool reports it.
type Entry struct {
	Key         string
	Scope       string
	Risk        string
	Secret      bool
	Editable    bool
	Description string
}

func (e Entry) appWide() bool {
	return e.Scope == ScopeAppGlobal
}

// scoped is whether the entry lives on the calling bot itself instead of in
// the settings store.
func (e Entry) scoped() bool {
	return e.Scope == ScopeBot
}

// entries is every setting a bot may see. A setting missing here is invisible
// and unchangeable, so new settings stay private until they are described.
var entries = []Entry{
	{Key: "memory.settings", Scope: ScopeAppGlobal, Risk: RiskStandard, Editable: true,
		Description: "Memory on/off, and which agent and model run memory search and dreams."},
	{Key: "browser.settings", Scope: ScopeAppGlobal, Risk: RiskSensitive, Editable: true,
		Description: "Whether bots may drive the user's browser."},
	{Key: "computer.settings", Scope: ScopeAppGlobal, Risk: RiskSensitive, Editable: true,
		Description: "Whether bots may control the user's computer."},
	{Key: "devices.settings", Scope: ScopeAppGlobal, Risk: RiskSensitive,
		Description: "The public address devices reach this Jaz server at. Device access is the user's to change."},
	{Key: "agents.defaults", Scope: ScopeAppGlobal, Risk: RiskCredential, Secret: true,
		Description: "Per-agent defaults: enabled agents, model, reasoning effort and auth. Credentials stay the user's to change."},
	{Key: "providers.custom", Scope: ScopeAppGlobal, Risk: RiskCredential, Secret: true,
		Description: "User-defined model providers. Credentials stay the user's to change."},
	{Key: "mcp.servers", Scope: ScopeAppGlobal, Risk: RiskCredential, Secret: true,
		Description: "MCP servers bots and threads can connect to. Credentials stay the user's to change."},
	{Key: "bot.name", Scope: ScopeBot, Risk: RiskStandard, Editable: true,
		Description: "The bot's name."},
	{Key: "bot.prompt", Scope: ScopeBot, Risk: RiskStandard, Editable: true,
		Description: "The bot's own instructions."},
	{Key: "bot.agent", Scope: ScopeBot, Risk: RiskStandard, Editable: true,
		Description: "The agent the bot runs on."},
	{Key: "bot.model", Scope: ScopeBot, Risk: RiskStandard, Editable: true,
		Description: "The model and reasoning effort the bot runs with."},
	{Key: "bot.plugins", Scope: ScopeBot, Risk: RiskPermission, Editable: true,
		Description: "The plugin ids the bot may use. Empty inherits every installed plugin, which the bot cannot restore once narrowed."},
	{Key: "bot.workspaces", Scope: ScopeBot, Risk: RiskPermission, Editable: true,
		Description: "The server directories the bot may work in. The bot can only remove paths, never add them."},
}

func entry(key string) (Entry, bool) {
	key = strings.TrimSpace(key)
	for _, candidate := range entries {
		if candidate.Key == key {
			return candidate, true
		}
	}
	return Entry{}, false
}

func (e Entry) namespaceKey() (string, string) {
	namespace, key, _ := strings.Cut(e.Key, ".")
	return namespace, key
}
