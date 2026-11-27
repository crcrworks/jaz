# Devin as a local ACP agent

Devin is a builtin ACP agent that runs its own `devin acp` stdio server. Jaz
registers the command, reads the login Devin already owns, and lets Devin's
session config options own the model and thought level.

## What Devin owns

- **Auth.** The CLI writes `$XDG_DATA_HOME/devin/credentials.toml`. Jaz reads
  only whether `windsurf_api_key` is present, never the value, and never copies
  it. Sign-in runs `devin auth login`; sign-out runs `devin auth logout`. There
  is no Jaz profile: Devin's session store travels with its own data directory,
  so a separate profile would fork it.
- **Prompt.** Devin advertises no session system-prompt channel. Its always-on
  rules are `$WINDSURF_CODEIUM_DIR/memories/global_rules.md`, so each launch gets
  a temporary Codeium directory holding the user's rules with Jaz's prompt
  appended, as Devin's own rule files are composed, beside links to the user's
  other global rules. Nothing rides session `_meta`.
- **Model and effort.** Devin advertises `model`, `thought_level`, `speed`, and
  its five modes through `session/new`. Jaz sends the configured model and
  thought level through `session/config/set_option` and refuses one Devin does
  not list; an account that advertises no models reports an empty catalogue
  rather than a guessed list.
- **Modes.** Devin's own mode set is used unchanged, so the baseline is its
  `accept-edits` and Plan mode is used because Devin advertises `plan`. `bypass`
  is offered by Devin but never selected unless the mode policy picks it.
- **Tools.** Devin's `exec` runs commands itself: the client's `terminal/*`
  methods only exist for hosts that advertise the ACP terminal capability, which
  Jaz does not, so a tool call never waits on Jaz for a terminal.
- **Compaction.** `compact` rides the command Devin advertises.

## Not in v1

`--cloud`, `--agent-type`, Devin Desktop, multiple workspace roots, and Devin's
own mention/rule/plan UI stay out. Session `list`/`delete` are used only to
release a throwaway discovery session.
