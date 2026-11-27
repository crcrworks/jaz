# Missing reasoning metadata stays unknown

A provider that lists a model without describing its reasoning used to be read
as a model that cannot reason, and an agent that asked for an effort on it could
not start: OpenCode with `deepseek/deepseek-v4.1-flash` on an OpenAI-compatible
provider failed with "reasoning capabilities are unavailable" before its session
existed. Listing a model is not a statement about its thinking, so Jaz now tells
three cases apart.

- `Model.Reasoning.Status` gains `unknown` beside `unavailable`. A `/models`
  entry, a provider `default_model` and a curated model the OpenRouter catalog
  does not cover are `unknown`; `unavailable` means the provider said the model
  has no reasoning, as Ollama's `/api/show` does when it reports no `thinking`
  capability.
- `ValidateReasoningEffort` defers an `unknown` model to the agent's own session
  contract, so the model's native check decides, and still refuses an effort on a
  model the provider knows does not reason.
- An agent whose levels are per model (`agentPolicy.effortsPerModel`, OpenCode)
  starts on the model's own default when the catalog says the model lacks the
  configured level, rather than failing the start, and the session skips setting
  a level the model did not advertise. The picker already falls back that way.
- The UI renders an `unknown` model with the agent's levels and an `unavailable`
  one with none, so a model without reasoning shows no effort control instead of
  a level that could never be sent.

Nothing here guesses a level or a capability: a value the provider does not state
stays unknown, and an agent's advertised levels are never invented for a model it
did not describe.
