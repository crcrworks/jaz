import { del, get, post, put } from './client'
import type {
  ModelProviderOption,
  ModelProviderStatus,
  ModelProviderStatusResponse,
  ProviderInput,
} from './types'

// Trims text fields and drops a blank api_key so an edit that doesn't touch the
// key leaves it unchanged. The endpoint URL keeps its path (unlike MCP's
// origin-collapsing normalize) — providers legitimately have paths like /openai/v1.
//
// Every other field is carried through as it is: this is the wire shape of the
// provider, so naming fields here would silently drop each new one.
function normalizeProviderInput(input: ProviderInput): ProviderInput {
  const apiKey = input.api_key?.trim()
  const icon = input.icon?.trim()
  return {
    ...input,
    label: input.label.trim(),
    base_url: input.base_url.trim().replace(/\/+$/, ''),
    capabilities: input.capabilities.length ? [...input.capabilities] : ['chat_completions'],
    default_model: input.default_model?.trim() || undefined,
    icon: icon || undefined,
    ...(apiKey ? { api_key: apiKey } : { api_key: undefined }),
  }
}

export function createProvider(input: ProviderInput): Promise<ModelProviderOption> {
  return post<ModelProviderOption>('/v1/providers', normalizeProviderInput(input))
}

export function updateProvider(id: string, input: ProviderInput): Promise<ModelProviderOption> {
  return put<ModelProviderOption>(`/v1/providers/${encodeURIComponent(id)}`, normalizeProviderInput(input))
}

export function getProviderStatus(id: string): Promise<ModelProviderStatus> {
  return get<ModelProviderStatus>(`/v1/providers/${encodeURIComponent(id)}/status`)
}

export function deleteProvider(id: string): Promise<{ ok: boolean }> {
  return del<{ ok: boolean }>(`/v1/providers/${encodeURIComponent(id)}`)
}

export function getProviderStatuses(): Promise<ModelProviderStatusResponse> {
  return get<ModelProviderStatusResponse>('/v1/providers/status')
}
