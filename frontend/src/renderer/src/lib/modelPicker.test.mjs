import { expect, test } from 'bun:test'
import { pickerEffortOptions, parseModelSelections } from './modelPicker'

const options = (values) => values.map((value) => ({ value, label: value }))

test('picker exposes only the effort stops it offers without changing provider IDs', () => {
  const available = options(['', 'none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultracode'])
  expect(pickerEffortOptions(available).map(({ value }) => value)).toEqual(['none', 'low', 'medium', 'high', 'xhigh', 'ultracode'])
  expect(available).toHaveLength(9)
  expect(pickerEffortOptions(options(['ultra', 'max', 'xhigh', 'high', 'medium', 'low']))).toEqual([
    { value: 'low', label: 'low' },
    { value: 'medium', label: 'medium' },
    { value: 'high', label: 'high' },
    { value: 'xhigh', label: 'Xhigh' },
    { value: 'ultra', label: 'Ultracode' },
  ])
  expect(pickerEffortOptions(options(['ultra', 'ultracode']))).toEqual([{ value: 'ultracode', label: 'Ultracode' }])
})

test('picker only offers efforts the selected model supports, with Max on top where there is no xhigh', () => {
  expect(pickerEffortOptions(options(['max', 'high', 'low'])).map(({ value }) => value)).toEqual(['low', 'high', 'max'])
  expect(pickerEffortOptions(options(['none', 'high'])).map(({ value }) => value)).toEqual(['none', 'high'])
  // "None" turns thinking off, so it needs a level beside it to turn off.
  expect(pickerEffortOptions(options(['none']))).toEqual([])
  // pi names the same switch "off", which its own slider keeps as the first stop.
  expect(pickerEffortOptions(options(['off', 'low', 'medium', 'high', 'xhigh', 'max'])).map(({ value }) => value))
    .toEqual(['off', 'low', 'medium', 'high', 'xhigh'])
  expect(pickerEffortOptions(options(['off']))).toEqual([])
  expect(pickerEffortOptions([])).toEqual([])
})

test('saved picker choices keep agents and providers separate and discard obsolete modes', () => {
  const selections = {
    'codex/openai': { model: 'gpt-6-astra', effort: 'medium', fastMode: true },
    'codex/openrouter': { model: 'qwen/qwen3', effort: '', fastMode: false },
    'claude/': { model: 'opus[1m]', effort: 'ultracode' },
  }
  expect(parseModelSelections(JSON.stringify(selections))).toEqual(selections)
  expect(parseModelSelections(JSON.stringify({ 'claude/': { ...selections['claude/'], mode: 'recommended' } }))).toEqual({ 'claude/': selections['claude/'] })
  for (const raw of [null, 'null', '{bad json', '{"claude/":null}', '{"codex/openai":{"model":2}}']) {
    expect(parseModelSelections(raw)).toEqual({})
  }
})
