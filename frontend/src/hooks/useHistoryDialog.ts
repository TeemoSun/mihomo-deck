import { useEffect, useRef, useState } from 'react'
import { useToastStore } from '../stores/useToastStore'

export function useHistoryDialog<T>() {
  const [history, setHistory] = useState<{ name: string; items: T[] } | null>(null)
  const request = useRef<AbortController | null>(null)

  useEffect(() => () => request.current?.abort(), [])

  const openHistory = (name: string, load: (signal: AbortSignal) => Promise<T[]>, errorMessage: string) => {
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    void load(controller.signal).then(items => {
      if (!controller.signal.aborted) setHistory({ name, items: items ?? [] })
    }).catch(() => {
      if (!controller.signal.aborted) useToastStore.getState().error(errorMessage)
    })
  }

  const closeHistory = () => {
    request.current?.abort()
    setHistory(null)
  }

  return { history, openHistory, closeHistory }
}
