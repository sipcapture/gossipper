import { useEffect, useRef, useState, startTransition } from 'react'

import { liveWebSocketURL, type LiveFrame } from '@/api/gossipper'

type LiveStatus = 'idle' | 'connecting' | 'open' | 'closed' | 'error'

export function useGossipperLive(enabled: boolean, token?: string) {
  const [status, setStatus] = useState<LiveStatus>('idle')
  const [last, setLast] = useState<LiveFrame | null>(null)
  const [lastError, setLastError] = useState<string | null>(null)
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    if (!enabled) {
      if (wsRef.current) {
        wsRef.current.close()
        wsRef.current = null
      }
      startTransition(() => {
        setLast(null)
        setLastError(null)
        setStatus('idle')
      })
      return
    }
    const t = token?.trim()
    const url = liveWebSocketURL(t === '' ? undefined : t)
    startTransition(() => {
      setStatus('connecting')
      setLastError(null)
    })
    const ws = new WebSocket(url)
    wsRef.current = ws
    ws.onopen = () => {
      setStatus('open')
    }
    ws.onclose = () => {
      setStatus('closed')
      wsRef.current = null
    }
    ws.onerror = () => {
      setLastError('WebSocket error')
      setStatus('error')
    }
    ws.onmessage = (ev) => {
      try {
        const data = JSON.parse(String(ev.data)) as LiveFrame
        setLast(data)
      } catch {
        setLastError('Invalid JSON in live frame')
      }
    }
    return () => {
      ws.close()
      wsRef.current = null
    }
  }, [enabled, token])

  return { status, last, lastError }
}
