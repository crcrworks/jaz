# Pi's thinking levels reach the picker as pi states them

A pi thread showed `No effort levels` for
`commandcode/deepseek/deepseek-v4.1-flash` although pi runs that model with
`off, low, medium, high, xhigh, max`. Pi's own RPC mode advertises the levels
(`get_available_thinking_levels`), and its `set_thinking_level` reaches the
model, so the gap was in how Jaz read pi's model list rather than in pi.

Two readings were wrong.

- `piModel.Levels` kept only the levels a model's `thinkingLevelMap` named a
  value for. Pi keeps a level whose map entry is `null` when it is one of
  `minimal`, `low`, `medium` or `high`; only `xhigh` and `max` are dropped when
  the map does not name them, and `off` is dropped by a `null` of its own. The
  list is now pi's own `getSupportedThinkingLevels`: every level except the ones
  the map closes, and `off` alone for a model that does not reason.
- `piModelCatalog` reported every pi model as `unavailable`, the status that
  means the provider said the model cannot reason. A model pi marks
  `reasoning: true` is now `ready` with pi's own levels, and one it marks
  otherwise stays `unavailable` with no level. Jaz still invents no level and no
  `default_effort`: the effort a session starts at is the one pi reports in
  `get_state`, which the bridge already applies.

The picker's own wording changes with it. A model that leaves no level to
choose no longer reads `No effort levels`, which sounded like a Jaz failure;
it now says the model has no thinking levels, and a catalog that has not
answered yet says it is still loading. The reason comes from the reasoning
selection the picker already consults, so nothing is guessed for a model the
catalog does not describe.

Nothing here changes another agent: OpenCode, Codex, Claude and the
provider-catalog paths keep their own effort contracts, and a pi level that a
model does not offer is still not sent — the bridge applies the ACP choice only
after pi reports the model's own state.

## What pi does and does not describe itself

pi's model list is pi's own answer, but for a provider pi does not ship, that
answer only knows what Jaz declared in `<root>/acp/pi/models.json`
(decision 0009). A declaration whose model entries carry no `reasoning` field
leaves pi reading the model as one that cannot think, so pi reports `off` alone
and the picker has no level to offer. Jaz does not fill the field in: whether
`commandcode/deepseek/deepseek-v4.1-flash` reasons is not something the
provider's `/models` list says, and a level invented here would be exactly the
guess this decision refuses. A model pi describes itself — or one a person
declared with `reasoning` and `thinkingLevelMap` in that profile's
`models.json` — keeps the levels pi reports for it; decision 0011 keeps that
declaration intact when Jaz redeclares the provider.

## A level a picker offers is a level a routine stores

A routine's `reasoning_effort` is accepted from the same list of levels agents
advertise, now including pi's `off`. Before this, the picker could offer `Off`
for a pi model while the routine refused to store it, so creating the routine
failed outright. `off` and `none` stay distinct names: pi runs its own level
called `off`, and `none` is Jaz's clear-the-effort value. Whether a run actually
takes the stored level is still the agent's own session check at spawn, so a
level the model does not offer is refused there rather than guessed here.
