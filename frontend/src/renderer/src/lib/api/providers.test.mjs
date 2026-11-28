import { describe, expect, test } from 'bun:test'

// client.ts reads the shell it runs in, so give it one before importing.
globalThis.window = { location: { origin: 'https://web.jaz.chat' }, jaz: undefined }
globalThis.localStorage = { getItem: () => null, setItem: () => {}, removeItem: () => {} }
globalThis.navigator ??= { platform: 'test' }

// A provider's fields are its wire shape: a field named in the request builder
// instead of carried through is a field silently dropped on the way.
const sent = []
globalThis.fetch = (_url, init) => {
  sent.push(JSON.parse(init.body))
  return Promise.resolve({
    ok: true,
    status: 200,
    headers: { get: () => 'application/json' },
    text: () => Promise.resolve('{"id":"p1"}'),
  })
}

const { createProvider, updateProvider } = await import('./providers')

const draft = {
  label: ' CommandCode ',
  base_url: 'https://api.commandcode.ai/provider/v1/',
  capabilities: [],
  default_model: 'deepseek/deepseek-v4.1-flash',
  headers: { 'X-Tenant': 'a' },
  reasoning_models: ['deepseek/deepseek-v4.1-flash'],
}

describe('provider requests', () => {
  test('carry the fields the settings form holds', async () => {
    await createProvider({ ...draft, api_key: ' k ' }).catch(() => {})
    await updateProvider('p1', draft).catch(() => {})

    expect(sent[0]).toMatchObject({
      label: 'CommandCode',
      base_url: 'https://api.commandcode.ai/provider/v1',
      capabilities: ['chat_completions'],
      headers: { 'X-Tenant': 'a' },
      reasoning_models: ['deepseek/deepseek-v4.1-flash'],
      api_key: 'k',
    })
  })

  test('leave the stored key alone when no new one is given', () => {
    expect(sent[1].api_key).toBeUndefined()
  })
})
