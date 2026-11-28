import { useQuery } from '@tanstack/react-query'
import { acpAgentModelsReady, acpUsesModelProvider } from './agentRuntimes'
import type { AgentSettings, ReasoningEffortOption } from './api/types'
import {
  modelCatalogQuery,
  type ModelSuggestion,
} from './models'
import {
  modelReasoningSelection,
  type ModelReasoningCatalog,
  type ModelReasoningSelection,
} from './reasoningEfforts'

export interface ModelReasoningState {
  modelSuggestions: ModelSuggestion[]
  modelsLoading: boolean
  modelsReady: boolean
  defaultModel?: string
  modelsError?: string
  reasoningOptions: ReasoningEffortOption[]
  // reasoningLabels is the server's vocabulary, so a level the picker does not
  // show as a stop still reads with its own name.
  reasoningLabels: ReasoningEffortOption[]
  effectiveReasoningEffort: string
  reasoningEffortSupported: boolean
  reasoningStatus: ModelReasoningSelection['status']
  reasoningBlocked: boolean
  // reasoningWhy says why the model offers no level to choose.
  reasoningWhy: string
  reasoningForModel: (model: string, effort: string) => ModelReasoningSelection
}

export function useModelReasoningState({
  settings,
  agent,
  model,
  reasoningEffort,
  provider,
}: {
  settings: AgentSettings | undefined
  agent: string
  model: string
  reasoningEffort: string
  provider: string | undefined
}): ModelReasoningState {
  const usesProvider = acpUsesModelProvider(settings, agent)
  const enabled = acpAgentModelsReady(settings, agent, provider)
  const query = useQuery({
    ...modelCatalogQuery(agent, usesProvider ? provider : undefined),
    enabled,
    staleTime: 0,
  })
  const ready = enabled && query.isSuccess
  const modelSuggestions = ready ? query.data.models : []
  const defaultModel = ready ? query.data.defaultModel : undefined
  const catalog: ModelReasoningCatalog = ready && modelSuggestions.length > 0
    ? { status: 'ready', suggestions: modelSuggestions }
    : { status: query.isError || ready ? 'error' : 'pending' }
  const reasoningForModel = (nextModel: string, effort: string) => modelReasoningSelection({
    settings,
    agent,
    model: nextModel || defaultModel || '',
    requested: effort,
    catalog,
  })
  const reasoning = reasoningForModel(model, reasoningEffort)
  return {
    modelSuggestions,
    modelsLoading: enabled && (query.isPending || query.isFetching),
    modelsReady: ready && !query.isFetching && modelSuggestions.length > 0,
    defaultModel,
    modelsError: !enabled ? undefined : query.error?.message || (ready && modelSuggestions.length === 0
      ? 'No models reported by this agent.'
      : undefined),
    reasoningOptions: reasoning.options,
    reasoningLabels: settings?.reasoning ?? [],
    effectiveReasoningEffort: reasoning.effectiveEffort,
    reasoningEffortSupported: reasoning.supported,
    reasoningStatus: reasoning.status,
    reasoningBlocked: reasoning.blocked,
    reasoningWhy: reasoning.why,
    reasoningForModel,
  }
}
