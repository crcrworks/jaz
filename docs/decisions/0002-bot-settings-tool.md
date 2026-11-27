# Bot settings tool and its permission model

Bots needed to change Jaz's settings the way their user would, without every
change becoming a confirmation card and without a bot being able to grant
itself more reach. This decision records the boundary.

## What a bot sees

`settings_list`, `settings_read` and `settings_update` are mounted only on a
bot's own tool surface (`internal/jaztools` → `botSurface`), so a plain chat, a
sidebar thread, a step worker and a routine owned by no bot never get them.
The catalogue in `internal/settingauth/catalog.go` is the whole surface: a
setting it does not describe is invisible and unchangeable.

Each entry carries `scope`, `risk`, `secret`, `editable` and a description:

| key | scope | risk | secret | editable |
| --- | --- | --- | --- | --- |
| `bot.name`, `bot.prompt`, `bot.agent`, `bot.model` | bot-local | standard | no | yes |
| `bot.plugins`, `bot.workspaces` | bot-local | permission | no | yes |
| `memory.settings` | app-global | standard | no | yes |
| `browser.settings`, `computer.settings` | app-global | sensitive | no | yes |
| `agents.defaults`, `providers.custom`, `mcp.servers` | app-global | credential | yes | no |
| `devices.settings` | app-global | sensitive | no | no |

`UpdateInput` resolves `{"key": …, "value": "<json>"}` first and falls back to
the typed fields (`prompt`, `name`, `agent`, `model`, `reasoning_effort`,
`plugins`, `workspaces`); arguments that disagree are read from `value`, which
is what the caller spelled out. `next` produces the one canonical value the
change applies, so `settings_read` -> `settings_update` round trips every
setting, `bot.model` included. A `bot.model` change may name the model alone;
the reasoning effort it already runs with is then kept instead of cleared, and
a bot running its agent's default model reads with no model and writes that
back as a no-op rather than an error.

`needs_review` means the change opens an approval card, i.e. it is an editable
app-global setting. `bot.plugins` and `bot.workspaces` carry `risk: permission`
but apply at once when narrowed, because narrowing only reduces reach.

An app-global setting nobody has written yet reads as `null` and runs on Jaz's
own default; `settings_read` says so rather than repeating the default.

## The three answers

1. **Apply now.** A bot-local change (its own name, prompt, agent, model,
   plugins, workspaces) is the bot tuning itself and lands immediately.
2. **Ask first.** An app-global setting that a bot may edit opens an approval
   card through `acp.Manager.RequestApproval`: who asked, which setting, its
   scope and risk, the redacted before/after, and the bot's stated reason.
   Approve applies; deny leaves the setting alone. Either way the answer goes
   back to the bot as the tool result.
3. **Refuse before any card.** Credentials, MCP servers and device access are
   not a bot's to change even with approval, and no change may widen a bot's own
   reach: `bot.plugins` and `bot.workspaces` can only lose entries, so a bot
   cannot give itself a plugin, a workspace, or a wider seat for another or
   child bot. Those requests come back as errors, with no approval UI.

Before a change is offered, `validate` reads an app-global value the way Jaz
reads it back and returns the exact value that will be stored. Memory goes
through `settings.ParseMemorySettings`, which is the same legacy
`search_agent`/`dream_agent` merge `LoadMemorySettings` does, so a value saved
by an older Jaz keeps its agent across a read and write. Memory goes
through the same `settings.ValidateMemorySettings` the HTTP route uses, so an
agent Jaz cannot start (unknown, disabled, the built-in `jaz`) and a reasoning
effort its model does not accept are refused here too; devices run their URL
normalization. A value Jaz could not load back fails there, so a mistyped
`{"enabled": "maybe"}` never reaches an approval card and never breaks the
readers that load the setting on every turn. What the user approves on the card
is what gets written.

An app-global change replaces the stored value whole, not per field: `{}` stores
a memory setting with `enabled: false`, because that is what an empty
`MemorySettings` means. `settings_read` reports the stored value, so a caller
that wants to keep a field reads first and sends it back. `null` is what an
untouched setting reads as, and writing that `null` back is refused: it would
store a zero value nobody asked for.

After an approved app-global change is stored, the settings tool runs the same
follow-up the HTTP settings routes do — memory's scheduler start/stop, the
computer backend closing when it is turned off, the jaztools sync, and an MCP
refresh — through a `Dependencies.Applied` hook the app layer supplies.

Approval option kinds are `approve`/`deny`, which is deliberately outside the
`allow*`/`reject*` kinds the thread's plan-approval surface keys on: a settings
approval is not a plan and must not take over the plan panel.

## Secrets

`bot.plugins` reads `null` while the bot inherits every installed plugin. That
null round trips as long as the bot inherits; once narrowed, `null` would add
every installed plugin back, so the tool refuses it as widening and the bot has
to name the set it wants. The refusal reaches the bot service through
`settingauth.BotUpdate.InheritPlugins`, which `internal/app/settings_tool.go`
forwards as `bots.UpdateBot.InheritPlugins`; a test there runs the real bot
service rather than a fake, because the fake is what hid the missing wiring. Every attempt — including `current` read failures —
leaves an audit row, and a failure in the post-save follow-up is reported
without a second row, because the value and its row are already stored.

A `secret` entry reports its existence and metadata only. `settings_read`
returns no value, the approval body and the audit row say `(secret)`, and a
failed change records the error text without the value it carried. The change
matcher refuses secret settings before any value is handled at all.

## Audit

Migration `0093_setting_changes` adds `setting_changes`: timestamp, thread id,
actor (the bot's name), setting key, scope, risk, redacted old and new values,
whether approval was required, the approval result, and the error that stopped
a refused or failed change. `storage.SettingChangeStore` owns it:

- `ApplySettingChange` writes the setting and its audit row in one SQLite
  transaction, so an approved app-global change cannot land without its record.
- `RecordSettingChange` records denials, refusals and validation errors, which
  have no value to write.

`settings_list` is not audited; reads are not changes.

A change that applied also leaves one `bot_activity` row of kind
`setting_changed` in the bot's chat, so the user sees what moved without
opening the audit table.

## Consequences

- Device approval stays about which client may connect; it grants no setting
  permission. `settingauth` never consults it.
- Adding a user-facing setting means adding one catalogue entry (and, for a
  secret one, leaving `editable` false). Nothing else in this package changes.
- `internal/settings` keeps its typed loaders; `NormalizeMemorySettings` and
  `NormalizeDeviceSettings` were split out so `settings_read` reports the same
  value `Save*` would store.
