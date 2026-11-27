# An OpenCode effort reaches the model whatever provider serves it

OpenCode names a model's thinking levels after the variants in its own
configuration, so Jaz reached a chosen effort only on OpenRouter: the variant
was written for that provider alone. On a custom OpenAI-compatible provider
(commandcode, opencode-go) OpenCode advertised only the levels its own model
entry carried, and Jaz then read the model as lacking the configured level and
started the session on the model default. Nothing said so, and because OpenCode
reports the level a model runs at by default as `default`, that name was stored
on the thread too — a worker inherited its bot's effort and then looked like it
had never been given one.

- `addOpenCodeReasoningVariant` writes the variant into every model entry Jaz
  itself describes, whatever provider serves it. A provider or model Jaz does
  not describe is the user's own OpenCode configuration and stays untouched, as
  the agent's own metadata does. Verified against the real adapter: a model
  without the variant advertises `low, high, max, default`; with it, `medium`
  appears and `session/set_config_option(effort=medium)` is accepted.
- An agent's own `default` level normalizes to no effort. It is the agent saying
  "nothing chosen", which is Jaz's empty effort, so it never reaches the stored
  session as a level of its own.
- A level the agent starts without, which is what happens when the model does
  not offer it, is no longer silent: the session carries a notice naming the
  agent, the model, the level that could not be applied and the levels the model
  does offer, and the thread shows it above the composer. The notice is the
  agent's session state, so it survives a relaunch with the session.

Nothing here invents a level or a model capability: the variant carries only the
effort the user chose, and a level OpenCode does not advertise is still not sent.
