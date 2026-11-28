import { create } from 'zustand'
import { api } from '../api/client'
import type { Preferences } from '../types/api'

interface State {
  prefs: Preferences | null
  loading: boolean
  loadPrefs: () => Promise<void>
  savePrefs: (prefs: Partial<Preferences>) => Promise<void>
  applyTheme: (theme: Preferences['theme']) => void
}

export const usePrefsStore = create<State>((set, get) => {
  let requestSequence = 0
  let systemTheme: MediaQueryList | undefined
  const applySystemTheme = (event: MediaQueryListEvent) =>
    document.documentElement.classList.toggle('dark', event.matches)

  const updatePrefs = (prefs: Preferences, sequence: number) => {
    if (sequence !== requestSequence) return
    set({
      prefs: { ...prefs, pinnedProxies: prefs.pinnedProxies ?? [], hiddenGroups: prefs.hiddenGroups ?? [] },
      loading: false,
    })
    get().applyTheme(prefs.theme)
  }

  return {
    prefs: null,
    loading: false,
    applyTheme: theme => {
      try {
        window.localStorage.setItem('mihomo-deck-theme', theme)
      } catch { /* Storage may be unavailable in restricted contexts. */ }

      if (theme === 'system') {
        if (!systemTheme) {
          systemTheme = window.matchMedia('(prefers-color-scheme: dark)')
          systemTheme.addEventListener('change', applySystemTheme)
        }
      } else {
        systemTheme?.removeEventListener('change', applySystemTheme)
        systemTheme = undefined
      }
      document.documentElement.classList.toggle('dark', theme === 'dark' || (systemTheme?.matches ?? false))
    },
    loadPrefs: async () => {
      const sequence = ++requestSequence
      set({ loading: true })
      try {
        updatePrefs(await api.preferences(), sequence)
      } finally {
        if (sequence === requestSequence) set({ loading: false })
      }
    },
    savePrefs: async prefs => {
      const sequence = ++requestSequence
      set({ loading: false })
      updatePrefs(await api.savePreferences(prefs), sequence)
    },
  }
})
