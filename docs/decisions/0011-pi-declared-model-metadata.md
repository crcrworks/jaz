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
declared by hand with `reasoning: true` and its levels, the same pi answered
`["off", "low", "medium", "high", "xhigh", "max"]` after `set_model`. Nothing
else differed, so the entry itself was the whole difference.

pi documents this: a `models` entry field defaults `reasoning` to `false`, and
"levels the model does not map use the provider's default value through high;
xhigh and max are unsupported". An entry Jaz writes with only an `id`,
`name` and `contextWindow` is therefore a model pi runs at `off` alone.

Jaz's own sources say nothing about reasoning: an OpenAI-compatible provider's
`/models` response lists ids, names and context lengths, and a level invented
from a model name would be exactly the guess decisions 0007 and 0010 refuse.

## What Jaz writes now

- A model entry the profile already declares is carried over verbatim, so its
  `reasoning`, `thinkingLevelMap`, `input`, `cost`, `maxTokens` and anything
  else stay pi's own word about that model.
- A model only the profile lists is left alone. Jaz never removes a model it
  did not add, and pi still runs it.
- A model Jaz adds gets its id, name and context window, and nothing more: pi
  reads that as a model without thinking, which is what Jaz actually knows.

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

A test pins the redeclaration: a profile that already declares
`deepseek/deepseek-v4.1-flash` with `reasoning` and a level map keeps them after
`PreparePiProfile` declares the same provider from Jaz's Model Providers. Real
pi 1.5.2 answers the six levels above once the entry survives.
