# One reasoning effort vocabulary, one wire map

Reasoning effort was described five times and the copies disagreed. `acp` had a
base list plus five per-agent variants, `provider` accepted up to `max` but not
`ultra`, `off` or `ultracode`, `loops` had its own copy in `schedule.go` and its
own strings in the two `loop_create`/`loop_update` schema fields, and the
frontend had `REASONING_EFFORT_OPTIONS` and the picker's own stop list. Each new
level was a five-file change, and forgetting one was silent: decision 0010 had to
patch the routine normalizer because the picker offered pi's `off` while the
routine refused it.

`backend/internal/reasoning` is now the one vocabulary: `Levels`, the graded
subset an agent encodes in a model id, the endpoint subset an OpenAI-compatible
provider takes, `Label`, `Normalize`, `NormalizeIn`, `Rank` and `Sort`. Everything
that accepted, validated, ordered or labelled a level reads it instead of keeping
a list, so a new level is added in one place.

What stays per agent is the one thing that is genuinely the harness's: which
levels it offers and in what order. `acp.effortLevels` is that table, keyed by
agent, and `agentEffortOptions` derives the labelled options from it. A test
asserts every level in that table is in `Levels`, which is the assertion that
would have caught the routine/picker split.

The wire spelling is data too, not three scattered translations: `off` is pi's
own switch and stays distinct from `none`, which is Jaz's clear-the-effort value;
`none` is a real level only for the agents that name it (OpenCode, Muse); an
agent's own `default` normalizes to no effort; Claude reaches `ultracode` through
its own setting while its effort config receives `xhigh`; and an agent whose
levels Jaz does not know keeps its own spelling for the agent to accept or refuse.

The endpoint boundary is explicit rather than a guess. `provider.NormalizeReasoningEffort`
accepts `reasoning.Endpoint` — the graded levels plus `none`, which it sends as
no value — so a level an endpoint does not understand is refused at that boundary
instead of being silently translated.

The frontend no longer carries a list. `GET /v1/settings/agents` returns
`reasoning`, the server's labelled vocabulary in display order, so a picker
cannot offer a level the backend would then refuse. The picker's stops remain a
deliberate UI subset — low through the top of the list — but their names come from
that vocabulary, and a saved level the track does not show still reads with its
own label.

Nothing here changes what a stored level means or which levels an agent offers:
the per-agent orderings, the `ultra`/`ultracode` special cases and the unknown
model behaviour of decisions 0007, 0008 and 0010 are preserved, and the `acp`
option lists are byte-identical to before.
