# Pi keeps what its profile declares about its own models

A pi thread showed "This model has no thinking levels" for
`commandcode/deepseek/deepseek-v4.1-flash`, a model the same picker had been
reporting six levels for before decision 0009 bridged Jaz's Model Providers into
pi's own profile. Neither pi nor the picker was wrong: Jaz replaced the model
entry the profile declared with the little Jaz knows about that model.

## What the real pi process answers

`/v1/acp/agents/pi/models` reads pi's own list, so the bug is visible there
rather than in Jaz's translation. With the `models.json` Jaz writes, `get_state`
for the model reported `reasoning: false` and `thinkingLevelMap: null`, and
`get_available_thinking_levels` reported `["off"]`. With the same provider
declared with `reasoning: true`, the same pi answered
`["off", "minimal", "low", "medium", "high"]` after `set_model`, its own
default levels — `reasoning` alone is the whole difference.

pi documents this: a `models` entry field defaults `reasoning` to `false`, and
"levels the model does not map use the provider's default value through high;
xhigh and max are unsupported". An entry Jaz writes with only an `id`,
`name` and `contextWindow` is therefore a model pi runs at `off` alone.

Jaz's own sources say nothing about reasoning: an OpenAI-compatible provider's
`/models` response lists ids, names and context lengths (`commandcode`'s returns
`supported_endpoints` and nothing about thinking), and a level invented from a
model name would be exactly the guess decisions 0007 and 0010 refuse. So the
fact travels from where a person states it.

## What Jaz writes now

A model provider may declare, in Settings > Model Providers, the models its
endpoint does not describe as thinking but that do think (`reasoning_models`).
That is a statement by a person, not a guess: Jaz writes `reasoning: true` for
exactly those models and nothing else about them.

- An entry already in `models.json` that Jaz did not add is carried over as it
  is, so its `reasoning`, `thinkingLevelMap`, `input`, `cost` and anything else
  stay the declaration it is. Jaz never drops one either.
- An entry Jaz added before is rewritten from the provider's current list, so a
  renamed model takes effect and a retired one leaves. `jazModels` in the file
  records which ids are Jaz's own.
- A model Jaz adds for the first time gets its id, name and context window, and
  — when declared — `reasoning: true`. pi fills in that model's levels itself.

`reasoning` is the one field that does not simply survive a refresh. Jaz writes
it from the declaration, so withdrawing the declaration has to stop it, or the
picker keeps offering levels nothing backs any more. A `reasoning` written into
`models.json` by hand is a person's word, though, and stays; `jazThinking` in the
file records which ids Jaz itself called thinking, so a refresh can tell the two
apart. The same distinction runs through a file older than the index: only Jaz
wrote it, so its entries stay Jaz's, but a `reasoning` in it was nobody's
declaration by Jaz and is left alone.

A `models.json` that names no providers, or names `null`, is read as none: the
file is Jaz's own declaration point and a blank one must not stop a thread.

`models.json` remains Jaz's declaration file, so a provider Jaz has no models
for still keeps its declaration — pi's own `auth login` needs it, and a stored
pi credential outranks the file.

## What the picker says

`This model has no thinking levels` answered two different situations with one
sentence. A model the agent reports as unable to think now reads
`The agent reports this model cannot think, so it has no thinking level`, and
one that thinks but leaves no level to choose reads
`This model offers no thinking levels`. Both follow the agent's answer; the
picker guesses nothing.

## Verification

Tests pin the four behaviours: a declared thinking model reaches `models.json`
as `reasoning: true`; withdrawing that declaration removes it again; a model Jaz
added is refreshed from the provider's later answer and a retired one leaves,
while an untouched entry keeps its fields and so does a hand-written
`reasoning`; and a `{"providers": null}` file no longer panics the refresh.

Against real pi 1.5.2 with the declaration Jaz now writes, `set_model` on
`commandcode/deepseek/deepseek-v4.1-flash` reports `reasoning: true` and
`get_available_thinking_levels` answers `["off","minimal","low","medium","high"]`
— pi's own defaults for a model it was told thinks, not levels Jaz states.
