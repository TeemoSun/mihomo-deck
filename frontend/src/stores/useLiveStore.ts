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
  connections: [],
  logs: [],
  logsPaused: false,
}

const nowSeconds = () => Math.floor(Date.now() / 1000)
const recentSamples = (samples: TrafficSample[], now = nowSeconds()) =>
  samples.filter(sample => sample.ts > now - 60).slice(-60)

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
          case 'init':
            set(state => ({
              status: event.data.status,
              traffic: event.data.traffic,
              memory: event.data.memory,
              connections: event.data.connections.connections ?? [],
              samples: recentSamples(event.data.trafficHistory ?? []),
              logs: state.logsPaused ? state.logs : (event.data.logs ?? []).slice(-500),
            }))
            break
          case 'traffic': {
            const { samples, memory, connections } = get()
            const ts = nowSeconds()
            set({
              traffic: event.data,
              samples: recentSamples([...samples, {
                ts,
                upBps: event.data.up,
                downBps: event.data.down,
                memInuse: memory.inuse,
                memOslimit: memory.oslimit,
                activeConns: connections.length,
              }], ts),
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
