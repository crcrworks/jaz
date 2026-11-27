import type { ReasoningEffortOption } from '@/lib/api/types'
import type { ModelSuggestion } from '@/lib/modelSuggestion'

export type ModelPickerOption = Pick<ModelSuggestion, 'value' | 'label' | 'aliases'> & { reasoning?: ModelSuggestion['reasoning'] }

export interface ModelSelection {
  model: string
  effort: string
  fastMode?: boolean
}

export function pickerEffortOptions(options: ReasoningEffortOption[]): ReasoningEffortOption[] {
  // Max stands in as the top stop for a model that offers it without xhigh.
  const top = options.some((option) => option.value === 'xhigh') ? 'xhigh' : 'max'
  const stops = ['low', 'medium', 'high', top, 'ultracode'].flatMap((value) => {
    const option = options.find((option) => option.value === value)
      ?? (value === 'ultracode' ? options.find((option) => option.value === 'ultra') : undefined)
    return option ? [{ ...option, label: value === 'xhigh' ? 'Xhigh' : value === 'ultracode' ? 'Ultracode' : option.label }] : []
  })
  // Turning thinking off is a stop only beside a level to turn off. An agent
  // that names the switch "off", as pi does, keeps that name.
  const off = stops.length === 0 ? undefined : options.find((option) => option.value === 'none') ?? options.find((option) => option.value === 'off')
  return off ? [off, ...stops] : stops
}

export function parseModelSelections(raw: string | null): Record<string, ModelSelection> {
  try {
    const parsed = JSON.parse(raw ?? '{}')
    return Object.fromEntries(Object.entries(parsed).flatMap(([key, entry]) => {
      const value = entry as Partial<ModelSelection> | null
      return value != null && typeof value.model === 'string' && typeof value.effort === 'string'
        ? [[key, { model: value.model, effort: value.effort, ...(typeof value.fastMode === 'boolean' ? { fastMode: value.fastMode } : {}) }]] : []
    }))
  } catch {
    return {}
  }
}
