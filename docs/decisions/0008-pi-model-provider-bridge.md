# Jaz feeds its Model Providers to pi

pi is a native RPC runtime (decision 0001) whose credentials live in its own
agent directory, and it only runs a model whose provider it can describe. Jaz's
Model Providers, though, is where a person enters provider API keys, and it
offers providers pi does not ship — `commandcode` is one. With pi pointed at
Jaz's isolated profile, `<root>/acp/pi`, nothing bridged the two, so a saved
`commandcode` key left `acp_auth.pi` at `authenticated: false` and pi could not
run `commandcode/deepseek/deepseek-v4.1-flash` at all.

## What Jaz writes

`PreparePiProfile` binds the key of every provider Jaz holds into the pi
process's environment, and declares in `<root>/acp/pi/models.json` the providers
pi does not describe itself. It returns the providers it handed pi a key for.

- The key always travels in the provider's own variable (`OPENAI_API_KEY`,
  `JAZ_PROVIDER_COMMANDCODE_API_KEY`, …), which is the one pi reads. That is
  true for a built-in provider too: `openai` and `openrouter` are told nothing
  else, and pi keeps its own api and model list.
- Only providers pi does not describe itself are declared. A declaration for a
  built-in provider would replace pi's own `api` and model list with a narrower
  copy.
- A declaration carries `baseUrl`, `api: "openai-completions"`, the provider's
  headers, and Jaz's model list for it; an endpoint that needs no key gets the
  dummy `none` pi documents.
- A provider stays declared when Jaz has no key for it, because pi's own
  `pi auth login <provider>` needs the declaration to know the provider exists.
  `pi`'s stored credential outranks `models.json`, so both sources coexist.

## What Jaz never writes

- The key itself. `apiKey` holds `"$NAME"` and the value travels in the
  environment Jaz already builds for the pi process, so no secret reaches a
  file next to the session transcripts.

## What stays pi's

pi resolves `--model` against its own profile and answers `ready`,
`not_ready` (`credentials_not_configured`) or `invalid` (model not in the
profile). Jaz reports that answer and nothing else; `invalid` becomes "add the
provider in Settings > Model Providers" rather than a wrong-model guess. Model
metadata, prompt construction, thinking levels and compaction stay pi's.

## Settings

The sign-in panel no longer shows a primary button that cannot work. When pi
needs its own terminal login, Settings shows the command with a copy button —
the same text pi's own login needs — and keeps the Model Providers key path as
the first option whenever a provider declares a key variable.
