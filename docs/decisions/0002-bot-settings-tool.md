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

`SettingsUpdateInput` resolves `{"key": …, "value": "<json>"}` first and falls
back to the typed fields (`prompt`, `name`, `agent`, `model`,
`reasoning_effort`, `plugins`, `workspaces`). Arguments that disagree are read
from `value`, which is what the caller spelled out.

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

Approval option kinds are `approve`/`deny`, which is deliberately outside the
`allow*`/`reject*` kinds the thread's plan-approval surface keys on: a settings
approval is not a plan and must not take over the plan panel.

## Secrets

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
