import type {
  AuthUser, ConnectionsSnapshot, DBStats, DelayRecord, DNSQueryResponse, MihomoConfig, OverviewResponse,
  PagedConnections, PagedLogs, Preferences, ProviderSnapshot, ProxyItem, ProxyProvider, RuleItem,
  RuleProvider, TrafficPoint,
} from '../types/api'
import { useAuthStore } from '../stores/useAuthStore'

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

let refreshFlight: { sessionSignal: AbortSignal; promise: Promise<void> } | undefined

function ensureActive(signal?: AbortSignal | null) {
  if (signal?.aborted) throw signal.reason ?? new DOMException('请求已取消', 'AbortError')
}

function isDeckAuthError(response: Response) {
  return response.status === 401 && response.headers.get('X-Deck-Auth-Error') === '1'
}

function refreshSession(sessionSignal: AbortSignal) {
  if (refreshFlight?.sessionSignal === sessionSignal) return refreshFlight.promise

  const promise = request<AuthUser>('/api/auth/refresh', { method: 'POST' }, false)
    .then(auth => {
      ensureActive(sessionSignal)
      useAuthStore.getState().setCsrfToken(auth.csrfToken)
    })
    .finally(() => {
      if (refreshFlight?.promise === promise) refreshFlight = undefined
    })
  refreshFlight = { sessionSignal, promise }
  return promise
}

async function request<T>(path: string, options: RequestInit = {}, recoverSession = true): Promise<T> {
  const { sessionSignal, csrfToken } = useAuthStore.getState()
  const method = (options.method ?? 'GET').toUpperCase()
  const headers = new Headers(options.headers)
  if (options.body) headers.set('Content-Type', 'application/json')
  const requiresCsrf = !['GET', 'HEAD', 'OPTIONS'].includes(method)
  if (requiresCsrf && csrfToken) {
    headers.set('X-CSRF-Token', csrfToken)
  }

  const ensureCurrent = () => {
    ensureActive(sessionSignal)
    ensureActive(options.signal)
  }
  const send = () => {
    ensureCurrent()
    return fetch(path, {
      ...options, method, headers, signal: options.signal ?? sessionSignal, credentials: 'include',
    })
  }
  let res = await send()
  ensureCurrent()

  if (recoverSession && isDeckAuthError(res)) {
    try {
      // A concurrent request may already have replaced the token.
      if (useAuthStore.getState().csrfToken === csrfToken) await refreshSession(sessionSignal)
    } catch {
      ensureActive(sessionSignal)
      useAuthStore.getState().clearSession()
      throw new ApiError(401, '登录已过期，请重新登录')
    }
    const token = useAuthStore.getState().csrfToken
    if (requiresCsrf && token) headers.set('X-CSRF-Token', token)
    res = await send()
    ensureCurrent()
    if (isDeckAuthError(res)) {
      useAuthStore.getState().clearSession()
      throw new ApiError(401, '登录已过期，请重新登录')
    }
  }

  if (!res.ok) {
    const raw = await res.text()
    ensureCurrent()
    let message = raw.trim() || res.statusText
    try {
      const json = JSON.parse(raw)
      message = json.message || json.error || message
    } catch { /* plain text */ }
    throw new ApiError(res.status, message)
  }
  if (res.status === 204) return undefined as T
  const data = await res.json() as T
  ensureCurrent()
  return data
}
const body = (value: unknown) => JSON.stringify(value)
const enc = encodeURIComponent
const query = (values: Record<string, string | number | undefined>) => {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(values)) {
    if (value !== undefined) params.set(key, String(value))
  }
  return params.toString()
}

export const api = {
  login: (username: string, password: string) =>
    request<AuthUser>('/api/auth/login', { method: 'POST', body: body({ username, password }) }, false),
  me: () => request<AuthUser>('/api/auth/me'),
  logout: () => request<void>('/api/auth/logout', { method: 'POST' }, false),
  password: (currentPassword: string, newPassword: string) =>
    request<{ csrfToken: string }>('/api/auth/password', { method: 'POST', body: body({ currentPassword, newPassword }) }),

  preferences: () => request<Preferences>('/api/preferences'),
  savePreferences: (prefs: Partial<Preferences>) =>
    request<Preferences>('/api/preferences', { method: 'PUT', body: body(prefs) }),
  overview: (signal?: AbortSignal) => request<OverviewResponse>('/api/overview', { signal }),
  traffic: (range: string, signal?: AbortSignal) =>
    request<TrafficPoint[]>(`/api/telemetry/traffic?${query({ range })}`, { signal }),
  connections: (params: Record<string, string | number | undefined> = {}, signal?: AbortSignal) =>
    request<PagedConnections>(`/api/telemetry/connections?${query(params)}`, { signal }),
  clearConnections: () => request<void>('/api/telemetry/connections', { method: 'DELETE' }),
  logs: (params: Record<string, string | number | undefined> = {}, signal?: AbortSignal) =>
    request<PagedLogs>(`/api/telemetry/logs?${query(params)}`, { signal }),
  clearLogs: () => request<void>('/api/telemetry/logs', { method: 'DELETE' }),
  delays: (proxyName: string, providerName?: string, signal?: AbortSignal) =>
    request<DelayRecord[]>(`/api/telemetry/delays?${query({ proxyName, providerName })}`, { signal }),
  providerHistory: (providerName: string, signal?: AbortSignal) =>
    request<ProviderSnapshot[]>(`/api/telemetry/providers?${query({ providerName })}`, { signal }),
  stats: (signal?: AbortSignal) => request<DBStats>('/api/telemetry/stats', { signal }),
  prune: () => request<void>('/api/telemetry/prune', { method: 'POST' }),

  version: () => request<{ version: string }>('/api/mihomo/version'),
  configs: (signal?: AbortSignal) => request<MihomoConfig>('/api/mihomo/configs', { signal }),
  patchConfig: (config: Partial<MihomoConfig>) =>
    request<void>('/api/mihomo/configs', { method: 'PATCH', body: body(config) }),
  reloadConfig: (force = false) => request<void>(`/api/mihomo/configs/reload?force=${force}`, { method: 'PUT' }),
  updateGeo: () => request<void>('/api/mihomo/configs/geo', { method: 'POST' }),
  restart: () => request<void>('/api/mihomo/restart', { method: 'POST' }),
  proxies: (signal?: AbortSignal) =>
    request<{ proxies: Record<string, ProxyItem> }>('/api/mihomo/proxies', { signal }),
  selectProxy: (group: string, name: string) =>
    request<void>(`/api/mihomo/proxies/${enc(group)}`, { method: 'PUT', body: body({ name }) }),
  unfixProxy: (group: string) => request<void>(`/api/mihomo/proxies/${enc(group)}`, { method: 'DELETE' }),
  testProxy: (name: string, provider?: string) =>
    request<{ delay: number }>(
      `/api/mihomo/proxies/${enc(name)}/delay${provider ? '?' + query({ provider }) : ''}`, { method: 'POST' },
    ),
  testGroup: (name: string) => request<Record<string, number>>(`/api/mihomo/group/${enc(name)}/delay`, { method: 'POST' }),
  proxyProviders: () => request<{ providers: Record<string, ProxyProvider> }>('/api/mihomo/providers/proxies'),
  updateProvider: (name: string) => request<void>(`/api/mihomo/providers/proxies/${enc(name)}`, { method: 'PUT' }),
  healthProvider: (name: string) =>
    request<void>(`/api/mihomo/providers/proxies/${enc(name)}/healthcheck`, { method: 'POST' }),
  ruleProviders: () => request<{ providers: Record<string, RuleProvider> }>('/api/mihomo/providers/rules'),
  updateRuleProvider: (name: string) => request<void>(`/api/mihomo/providers/rules/${enc(name)}`, { method: 'PUT' }),
  rules: () => request<{ rules: RuleItem[] }>('/api/mihomo/rules'),
  activeConnections: () => request<ConnectionsSnapshot>('/api/mihomo/connections'),
  closeConnections: () => request<void>('/api/mihomo/connections', { method: 'DELETE' }),
  closeConnection: (id: string) => request<void>(`/api/mihomo/connections/${enc(id)}`, { method: 'DELETE' }),
  dns: (name: string, type = 'A') => request<DNSQueryResponse>(`/api/mihomo/dns/query?${query({ name, type })}`),
  flushFakeIP: () => request<void>('/api/mihomo/cache/fakeip/flush', { method: 'POST' }),
  flushDNS: () => request<void>('/api/mihomo/cache/dns/flush', { method: 'POST' }),
}
