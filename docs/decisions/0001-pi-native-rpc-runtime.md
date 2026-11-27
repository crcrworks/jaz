# Pi runs on its own RPC mode

Pi has no ACP server. `pi-acp` bridges it, but it drops the system prompt, MCP
servers, steering and the session map, so a pi thread through it is a lesser
agent than pi is. Jaz therefore runs `pi --mode rpc` itself and puts an ACP
bridge in front of it: `backend/internal/pirpc` owns pi's records, and
`internal/acp` sees an ordinary ACP agent. The bridge is the only place pi's
record shapes appear; the runtime contract above it stays transport-neutral.

## What pi owns

- **Prompt.** pi builds its own system prompt; Jaz appends its session
  extension with `--append-system-prompt`, never replacing pi's prompt and never
  rebuilding the provider prompt itself. The text is rewritten per launch
  (resume included), so a thread's instructions never go stale.
- **Models and thinking levels.** `session/new` and `set_config_option` report
  what `get_available_models`, `get_state` and `get_available_thinking_levels`
  answer, with ids as pi spells them (`provider/id`). The model list, context
  window, max tokens and thinking levels in Settings come from the same
  answers. A value pi does not state stays unknown instead of being guessed.
  A model pi does not offer is refused, not silently swapped.
- **Compaction.** Jaz's compact turn sends pi's own `compact` command. Jaz never
  writes a summary of its own. (`/compact` as prompt text does nothing inRPC
  mode: built-in commands are TUI-only.)
- **Credentials.** pi reads `auth.json` in its own agent directory.
  `PI_CODING_AGENT_DIR` points at `<root>/acp/pi`, so a Jaz thread never reads
  or writes the user's `~/.pi`. Jaz forwards no provider key to pi beyond the
  provider environment variables every agent already receives.
- **Session.** Each thread runs one pi process on its own session file under
  `<root>/acp/pi/sessions/<jaz session id>/session.jsonl`. pi reopens that file
  when a thread resumes, so provider history and the thread's cwd survive a
  restart.

## What Jaz owns

- **Tools.** pi's built-in MCP client is given Jaz's servers through a
  generated extension (`pi.registerMcpServer`) with `direct` exposure, because
  pi takes its servers from its own client rather than ACP's `mcpServers`
  field. Jaz's prompt names the tools by the names pi shows (`mcp__jaztools__…`),
  so `ask_user`, thread operations, memory and connected accounts work in a pi
  thread.
- **Turn shape.** pi's `message_update`, tool execution and `agent_settled`
  records become ACP session updates and one prompt response, so streaming,
  tool cards, cancellation and turn completion go through the existing paths.

## Deliberately absent in v1

- No routine or memory worker: pi is not in `DefaultWorkerAgent`.
- No mid-turn steering: the bridge reports no steering capability, so a
  message written during a running turn follows the same unsupported path as
  any agent without it.
- No token/cost usage: pi's RPC reports none of it per turn, so the thread
  shows no usage rather than a guessed figure.
- No managed installer: pi is a deployment-provided executable, found on
  `PATH` or named by `JAZ_PI_COMMAND`.
- No sign-in from the Jaz UI: `pi auth login` needs a terminal, so Settings
  shows the exact command to run in Jaz's profile instead of pretending to run
  it.
