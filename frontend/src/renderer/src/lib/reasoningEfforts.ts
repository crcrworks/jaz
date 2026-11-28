import type { AgentSettings, ReasoningEffortOption } from './api/types'
import { modelSuggestionFor, type ModelSuggestion } from './modelSuggestion'

function acpReasoningEffortOptions(
  settings: AgentSettings | undefined,
  agent: string,
): ReasoningEffortOption[] {
  // An explicit empty list means the agent has no reasoning efforts of its own —
  // e.g. Antigravity, whose levels come with each model. Honor that; only fall
  // back to the vocabulary when the agent's options haven't loaded.
  return settings?.acp_options?.[agent]?.reasoning_efforts ?? settings?.reasoning ?? []
}

// effortLabel names a level through the options the caller has, then through the
// full vocabulary, and only then falls back to the value itself.
function effortLabel(value: string, options: ReasoningEffortOption[], vocabulary: ReasoningEffortOption[] = []): string {
  const named = options.find((option) => option.value === value)
    ?? vocabulary.find((option) => option.value === value)
  return named?.label ?? (value || 'Default')
}

export function reasoningEffortLabel(
  value: string | undefined,
  options: ReasoningEffortOption[],
  vocabulary?: ReasoningEffortOption[],
): string {
  return effortLabel(value ?? '', options, vocabulary)
}

export interface ModelReasoningSelection {
  options: ReasoningEffortOption[]
  effectiveEffort: string
  supported: boolean
  status: 'pending' | 'error' | 'ready'
  blocked: boolean
  // why is what the picker says when the model leaves no level to choose.
  why: string
}

const NO_LEVELS = 'This model has no thinking levels'
const LOADING_LEVELS = 'Loading thinking levels…'

export type ModelReasoningCatalog =
  | { status: 'pending' | 'error' }
  | { status: 'ready'; suggestions: ModelSuggestion[] }

// modelReasoningSelection answers what to offer for a model and what effort the
// turn will actually use. Only a model the catalog describes in full narrows the
// agent's own levels: anything unknown keeps the configured effort and the
// agent's options, so a missing catalog never silently drops a saved choice.
export function modelReasoningSelection({
  settings,
  agent,
  model,
  requested,
  catalog,
}: {
  settings: AgentSettings | undefined
  agent: string
  model: string
  requested: string
  catalog: ModelReasoningCatalog
}): ModelReasoningSelection {
  const effort = requested.trim()
  const agentOptions = acpReasoningEffortOptions(settings, agent)
  if (catalog.status !== 'ready') {
    return { options: agentOptions, effectiveEffort: effort, supported: true, status: catalog.status, blocked: effort !== '', why: LOADING_LEVELS }
  }
  const suggestion = modelSuggestionFor(catalog.suggestions, model)
  if (suggestion?.reasoning.status === 'pending') {
    return { options: agentOptions, effectiveEffort: effort, supported: true, status: 'pending', blocked: effort !== '', why: LOADING_LEVELS }
  }
  if (!suggestion || suggestion.reasoning.status === 'unknown') {
    return { options: agentOptions, effectiveEffort: effort, supported: true, status: 'ready', blocked: false, why: '' }
  }
  if (suggestion.reasoning.status !== 'ready') {
    // The provider knows the model has no reasoning, so the agent's levels do
    // not apply to it and the turn runs without one.
    return { options: [], effectiveEffort: '', supported: supportedReasoningEffort(effort, []), status: 'ready', blocked: false, why: NO_LEVELS }
  }
  // A model that reasons on its own has no level to choose.
  const options = suggestion.reasoning.automatic ? [] : reasoningOptions(suggestion, agentOptions, settings?.reasoning ?? [])
  // A saved Antigravity id such as gemini-4-neon-high names the level it runs at.
  const named = suggestion.reasoning.efforts?.find((level) => model === `${suggestion.value}-${level}`)
  const wanted = effort || named || ''
  // A level this model cannot run gives way to the level it recommends, and
  // failing that to following its own default, never to a level it lacks.
  const usable = supportedReasoningEffort(wanted, options)
  return {
    options,
    effectiveEffort: usable ? wanted : suggestion.reasoning.default_effort ?? '',
    // The request names no level, so the model's own level stands in and there
    // is nothing to report as unsupported even when it had to give way.
    supported: usable || effort === '',
    status: 'ready',
    blocked: false,
    why: options.length ? '' : NO_LEVELS,
  }
}

export function supportedReasoningEffort(value: string, options: ReasoningEffortOption[]): boolean {
  const effort = value.trim()
  // `none` clears Jaz's inherited session effort; an agent that advertises the
  // level turns its thinking off with it instead.
  return effort === '' || effort === 'none' || options.some((option) => option.value === effort)
}

// withConfiguredEffort keeps a saved effort on the list when the current model
// does not name it, so a picker shows the level a turn actually asks for
// instead of falling back to another row and hiding the difference.
export function withConfiguredEffort(
  effort: string,
  options: ReasoningEffortOption[],
  vocabulary?: ReasoningEffortOption[],
): ReasoningEffortOption[] {
  const value = effort.trim()
  if (value === '' || options.some((option) => option.value === value)) return options
  return [...options, { value, label: reasoningEffortLabel(value, options, vocabulary) }]
}

// reasoningOptions reads a model's own levels through the labels the agent and
// the vocabulary supply, so a level keeps its own name wherever it is known.
function reasoningOptions(
  suggestion: ModelSuggestion,
  agentOptions: ReasoningEffortOption[],
  vocabulary: ReasoningEffortOption[],
): ReasoningEffortOption[] {
  const values = suggestion.reasoning.efforts ?? []
  if (values.length === 0) return []
  return [
    { value: '', label: 'Default' },
    ...values.map((value): ReasoningEffortOption => ({ value, label: effortLabel(value, agentOptions, vocabulary) })),
  ]
}
