import type { AgentSettings, ReasoningEffortOption } from './api/types'
import { modelSuggestionFor, type ModelSuggestion } from './modelSuggestion'

const REASONING_LABELS: Record<string, string> = {
  '': 'Default',
  none: 'None',
  minimal: 'Minimal',
  low: 'Low',
  medium: 'Medium',
  high: 'High',
  xhigh: 'Extra high',
  max: 'Max',
  ultra: 'Ultra',
  ultracode: 'Ultracode',
}

export const REASONING_EFFORT_OPTIONS: ReasoningEffortOption[] = [
  { value: '', label: 'Default' },
  { value: 'minimal', label: 'Minimal' },
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
  { value: 'xhigh', label: 'Extra high' },
]

function acpReasoningEffortOptions(
  settings: AgentSettings | undefined,
  agent: string,
): ReasoningEffortOption[] {
  // An explicit empty list means the agent has no reasoning efforts of its own —
  // e.g. Antigravity, whose levels come with each model. Honor that; only fall
  // back to a generic list when the agent's options haven't loaded.
  return settings?.acp_options?.[agent]?.reasoning_efforts ?? REASONING_EFFORT_OPTIONS
}

export function reasoningEffortLabel(
  value: string | undefined,
  options: ReasoningEffortOption[],
): string {
  const effort = value ?? ''
  return options.find((option) => option.value === effort)?.label ?? REASONING_LABELS[effort] ?? (effort || 'Default')
}

export interface ModelReasoningSelection {
  options: ReasoningEffortOption[]
  effectiveEffort: string
  supported: boolean
  status: 'pending' | 'error' | 'ready'
  blocked: boolean
}

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
    return { options: agentOptions, effectiveEffort: effort, supported: true, status: catalog.status, blocked: effort !== '' }
  }
  const suggestion = modelSuggestionFor(catalog.suggestions, model)
  if (suggestion?.reasoning.status === 'pending') {
    return { options: agentOptions, effectiveEffort: effort, supported: true, status: 'pending', blocked: effort !== '' }
  }
  if (!suggestion || suggestion.reasoning.status === 'unknown') {
    return { options: agentOptions, effectiveEffort: effort, supported: true, status: 'ready', blocked: false }
  }
  if (suggestion.reasoning.status !== 'ready') {
    // The provider knows the model has no reasoning, so the agent's levels do
    // not apply to it and the turn runs without one.
    return { options: [], effectiveEffort: '', supported: supportedReasoningEffort(effort, []), status: 'ready', blocked: false }
  }
  // A model that reasons on its own has no level to choose.
  const options = suggestion.reasoning.automatic ? [] : reasoningOptions(suggestion)
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
): ReasoningEffortOption[] {
  const value = effort.trim()
  if (value === '' || options.some((option) => option.value === value)) return options
  return [...options, { value, label: reasoningEffortLabel(value, options) }]
}

function reasoningOptions(suggestion: ModelSuggestion): ReasoningEffortOption[] {
  const values = suggestion.reasoning.efforts ?? []
  if (values.length === 0) return []
  return [
    { value: '', label: 'Default' },
    ...values.map((value): ReasoningEffortOption => ({ value, label: REASONING_LABELS[value] ?? value })),
  ]
}
