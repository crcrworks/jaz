import { describe, expect, test } from 'bun:test'
import { filterModelSuggestions, modelSuggestionLabel } from './modelSuggestion'
import { modelReasoningSelection, withConfiguredEffort } from './reasoningEfforts'

const input = {
  settings: undefined,
  agent: 'codex',
  model: 'gpt-test',
  requested: 'high',
}

const openCodeSettings = {
  providers: [],
  acp_auth: {},
  acp: {},
  acp_options: {
    opencode: {
      reasoning_efforts: [
        { value: '', label: 'Default' },
        { value: 'none', label: 'None' },
        { value: 'high', label: 'High' },
      ],
    },
  },
  agents: [],
}

describe('modelReasoningSelection', () => {
  test.each(['pending', 'error'])('preserves effort while the catalog is %s', (status) => {
    const selection = modelReasoningSelection({ ...input, catalog: { status } })

    expect(selection.effectiveEffort).toBe('high')
    expect(selection.supported).toBeTrue()
    expect(selection.status).toBe(status)
    expect(selection.blocked).toBeTrue()
  })

  test('carries the configured effort when the provider knows no levels', () => {
    const selection = modelReasoningSelection({
      ...input,
      catalog: {
        status: 'ready',
        suggestions: [{ value: 'gpt-test', label: 'Test', reasoning: { status: 'unavailable' } }],
      },
    })

    // The provider knows nothing about this model's thinking, which is not the
    // same as knowing it has none: the effort is carried for the agent to judge.
    expect(selection.effectiveEffort).toBe('high')
    expect(selection.supported).toBeTrue()
    expect(selection.status).toBe('ready')
    expect(selection.blocked).toBeFalse()
  })

  test('uses advertised model efforts when capabilities are ready', () => {
    const catalog = {
      status: 'ready',
      suggestions: [{
        value: 'gpt-test',
        label: 'Test',
        reasoning: { status: 'ready', efforts: ['low', 'high'] },
      }],
    }

    expect(modelReasoningSelection({ ...input, catalog }).supported).toBeTrue()
    expect(modelReasoningSelection({ ...input, requested: 'minimal', catalog }).supported).toBeFalse()
  })

  test('lets a thinking model without effort controls use its automatic default', () => {
    const catalog = {
      status: 'ready',
      suggestions: [{ value: 'gpt-test', label: 'Test', reasoning: { status: 'ready', automatic: true } }],
    }
    const selection = modelReasoningSelection({ ...input, catalog })

    expect(selection.options).toEqual([])
    expect(selection.effectiveEffort).toBe('')
    expect(selection.blocked).toBeFalse()
    expect(modelReasoningSelection({ ...input, requested: 'none', catalog }).supported).toBeTrue()
  })

  test('uses server-provided aliases across providers', () => {
    const suggestions = [{
      value: 'openai/gpt-test',
      label: 'Test',
      aliases: ['gpt-test'],
      reasoning: { status: 'ready', efforts: ['high'] },
    }]
    const selection = modelReasoningSelection({
      ...input,
      catalog: { status: 'ready', suggestions },
    })

    expect(selection.supported).toBeTrue()
    expect(selection.effectiveEffort).toBe('high')
    expect(modelSuggestionLabel(suggestions, 'gpt-test')).toBe('Test')
    expect(filterModelSuggestions(suggestions, 'gpt-test')).toEqual(suggestions)
  })

  test('runs a saved per-level model id at the level it names until another is picked', () => {
    const catalog = {
      status: 'ready',
      suggestions: [{
        value: 'gemini-4-neon',
        label: 'Gemini 4 Neon',
        aliases: ['gemini-4-neon-high', 'gemini-4-neon-xhigh'],
        reasoning: { status: 'ready', efforts: ['high', 'xhigh'] },
      }],
    }
    const saved = { ...input, agent: 'antigravity', model: 'gemini-4-neon-high', catalog }

    expect(modelReasoningSelection({ ...saved, requested: '' }).effectiveEffort).toBe('high')
    expect(modelReasoningSelection({ ...saved, requested: 'xhigh' }).effectiveEffort).toBe('xhigh')
    expect(modelReasoningSelection({ ...saved, model: 'gemini-4-neon', requested: '' }).effectiveEffort).toBe('')
    // The picker names no level of its own; the id does. Nothing the user asked
    // for is unsupported, so a model change keeps the effort instead of clearing
    // it, while the value shown is still the level the id runs.
    const named = modelReasoningSelection({ ...saved, requested: '' })
    expect(named.effectiveEffort).toBe('high')
    expect(named.supported).toBeTrue()
  })

  test('preserves effort when a ready catalog still marks the model pending', () => {
    const selection = modelReasoningSelection({
      ...input,
      catalog: {
        status: 'ready',
        suggestions: [{ value: 'gpt-test', label: 'Test', reasoning: { status: 'pending' } }],
      },
    })

    expect(selection.effectiveEffort).toBe('high')
    expect(selection.status).toBe('pending')
    expect(selection.blocked).toBeTrue()
  })

  // A model the catalog never described keeps the agent's options: dropping the
  // configured effort there would silently start the thread on the default.
  test('keeps the configured effort for a model the catalog does not describe', () => {
    const selection = modelReasoningSelection({
      ...input,
      settings: openCodeSettings,
      agent: 'opencode',
      model: 'z-ai/glm-5.2',
      requested: 'low',
      catalog: {
        status: 'ready',
        suggestions: [{ value: 'other-model', label: 'Other', reasoning: { status: 'ready', efforts: ['high'] } }],
      },
    })

    expect(selection.options.map((option) => option.value)).toEqual(['', 'none', 'high'])
    expect(selection.effectiveEffort).toBe('low')
    expect(selection.supported).toBeTrue()
    expect(selection.blocked).toBeFalse()
  })

  test('offers and keeps none when the model advertises it next to real levels', () => {
    const catalog = {
      status: 'ready',
      suggestions: [{
        value: 'z-ai/glm-5.2',
        label: 'GLM 5.2',
        reasoning: { status: 'ready', efforts: ['none', 'high', 'xhigh'] },
      }],
    }
    const settings = {
      acp_options: { opencode: { reasoning_efforts: [{ value: '', label: 'Default' }, { value: 'none', label: 'None' }, { value: 'high', label: 'High' }, { value: 'xhigh', label: 'Extra high' }] } },
    }
    const off = modelReasoningSelection({ ...input, settings, agent: 'opencode', model: 'z-ai/glm-5.2', requested: 'none', catalog })

    expect(off.options.map((option) => option.value)).toEqual(['', 'none', 'high', 'xhigh'])
    expect(off.effectiveEffort).toBe('none')
    expect(off.supported).toBeTrue()
  })

  // "none" turns thinking off and "" follows the model default; the UI must not
  // fold one into the other the way Settings' "None" label used to.
  test('keeps none apart from unset', () => {
    const catalog = { status: 'ready', suggestions: [] }

    expect(modelReasoningSelection({ ...input, settings: openCodeSettings, agent: 'opencode', catalog, requested: 'none' }).effectiveEffort).toBe('none')
    expect(modelReasoningSelection({ ...input, settings: openCodeSettings, agent: 'opencode', catalog, requested: '' }).effectiveEffort).toBe('')
  })
})

describe('withConfiguredEffort', () => {
  test('keeps a saved effort the current model does not list', () => {
    expect(withConfiguredEffort('none', [{ value: '', label: 'Default' }])).toEqual([
      { value: '', label: 'Default' },
      { value: 'none', label: 'None' },
    ])
  })

  test('leaves a list that already names the effort alone', () => {
    const options = [{ value: '', label: 'Default' }, { value: 'none', label: 'None' }]
    expect(withConfiguredEffort('none', options)).toBe(options)
    expect(withConfiguredEffort('', options)).toBe(options)
  })
})
