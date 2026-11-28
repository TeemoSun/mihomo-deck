import { create } from 'zustand'
import { api, ApiError } from '../api/client'
import { useAuthStore } from './useAuthStore'
import type { ActiveConnection, CollectorStatus, LiveEvent, LogEntry, TrafficSample } from '../types/api'

interface State {
  connected: boolean
  status: CollectorStatus | null
  traffic: { up: number; down: number }
  memory: { inuse: number; oslimit: number }
  samples: TrafficSample[]
  liveSync: { serverTs: number; syncedAtMs: number } | null
  connections: ActiveConnection[]
  logs: LogEntry[]
  logsPaused: boolean
  connect: () => void
  disconnect: () => void
  setLogsPaused: (paused: boolean) => void
}

const initialLiveData = {
  connected: false,
  status: null,
  traffic: { up: 0, down: 0 },
  memory: { inuse: 0, oslimit: 0 },
  samples: [],
  liveSync: null,
  connections: [],
  logs: [],
  logsPaused: false,
}

const nowSeconds = (nowMs = Date.now()) => Math.floor(nowMs / 1000)

function recentSamples(samples: TrafficSample[], windowEnd: number) {
  const byTs = new Map<number, TrafficSample>()
  for (const sample of samples) {
    if (sample.ts > windowEnd - 60 && sample.ts <= windowEnd) byTs.set(sample.ts, sample)
  }
  return [...byTs.values()].sort((a, b) => a.ts - b.ts).slice(-60)
}

function upsertSample(samples: TrafficSample[], sample: TrafficSample, windowEnd: number) {
  return recentSamples([...samples, sample], windowEnd)
}

export const useLiveStore = create<State>((set, get) => {
  let socket: WebSocket | null = null
  let timer: ReturnType<typeof setTimeout> | null = null
  let attempts = 0
  let generation = 0
  let connecting = false

  const scheduleReconnect = () => {
    timer = setTimeout(() => {
      timer = null
      get().connect()
    }, Math.min(30000, 1000 * 2 ** attempts++))
  }

  return {
    ...initialLiveData,
    setLogsPaused: logsPaused => set({ logsPaused }),
    connect: async () => {
      if (socket || timer || connecting) return
      const current = ++generation
      if (attempts > 0) {
        connecting = true
        try {
          await api.me()
        } catch (error) {
          if (current !== generation) return
          connecting = false
          if (error instanceof ApiError && error.status === 401) {
            useAuthStore.getState().clearSession()
          } else {
            scheduleReconnect()
          }
          return
        }
        if (current !== generation) return
        connecting = false
      }

      const ws = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/ws/live`)
      socket = ws
      ws.onopen = () => {
        if (socket !== ws) return
        attempts = 0
        set({ connected: true })
      }
      ws.onmessage = message => {
        if (socket !== ws) return
        let event: LiveEvent
        try {
          event = JSON.parse(message.data) as LiveEvent
        } catch {
          return
        }
        switch (event.type) {
          case 'init': {
            const { status, traffic, memory, connections, logs } = event.data
            const liveHistory = event.data.liveTrafficHistory ?? []
            const trafficHistory = event.data.trafficHistory ?? []
            const syncedAtMs = Date.now()
            const serverTs = Math.max(
              status?.lastSeenAt ?? 0,
              ...liveHistory.map(sample => sample.ts),
              ...trafficHistory.map(sample => sample.ts),
            ) || nowSeconds(syncedAtMs)
            set(state => ({
              status,
              traffic,
              memory,
              connections: connections.connections ?? [],
              samples: recentSamples(liveHistory.length ? liveHistory : trafficHistory, serverTs),
              liveSync: { serverTs, syncedAtMs },
              logs: state.logsPaused ? state.logs : (logs ?? []).slice(-500),
            }))
            break
          }
          case 'traffic': {
            const { samples, memory, connections, liveSync } = get()
            const { up, down } = event.data
            const syncedAtMs = Date.now()
            const ts = event.data.ts ?? event.data.sample?.ts ?? (liveSync
              ? liveSync.serverTs + Math.max(0, Math.floor((syncedAtMs - liveSync.syncedAtMs) / 1000))
              : nowSeconds(syncedAtMs))
            const sample = event.data.sample ?? {
              ts,
              upBps: up,
              downBps: down,
              memInuse: memory.inuse,
              memOslimit: memory.oslimit,
              activeConns: connections.length,
            }
            set({
              traffic: { up, down },
              samples: upsertSample(samples, sample, ts),
              liveSync: { serverTs: ts, syncedAtMs },
            })
            break
          }
          case 'memory':
            set({ memory: event.data })
            break
          case 'connections':
            set({ connections: event.data.connections ?? [] })
            break
          case 'status':
            set({ status: event.data })
            break
          case 'log':
            if (!get().logsPaused) set(state => ({ logs: [...state.logs, event.data].slice(-500) }))
            break
        }
      }
      ws.onclose = () => {
        if (socket !== ws) return
        socket = null
        set({ connected: false })
        scheduleReconnect()
      }
      ws.onerror = () => ws.close()
    },
    disconnect: () => {
      generation++
      connecting = false
      attempts = 0
      if (timer) clearTimeout(timer)
      timer = null
      const ws = socket
      socket = null
      ws?.close()
      set(initialLiveData)
    },
  }
})
