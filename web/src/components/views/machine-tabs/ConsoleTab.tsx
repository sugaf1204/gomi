import { useEffect, useRef, useState, useCallback } from 'react'
import RFB from '@novnc/novnc'
import type { Machine } from '../../../types'

type Props = {
  machine: Machine
}

type Status = 'idle' | 'connecting' | 'connected' | 'disconnected' | 'error'

export function ConsoleTab({ machine }: Props) {
  const containerRef = useRef<HTMLDivElement>(null)
  const rfbRef = useRef<RFB | null>(null)
  const [status, setStatus] = useState<Status>('idle')
  const [errorMsg, setErrorMsg] = useState('')

  const ip = machine.ip

  const connect = useCallback(() => {
    if (!containerRef.current || !ip) return

    if (rfbRef.current) {
      rfbRef.current.disconnect()
      rfbRef.current = null
    }

    containerRef.current.innerHTML = ''
    setStatus('connecting')
    setErrorMsg('')

    const apiBase = import.meta.env.VITE_API_BASE ?? 'http://localhost:5392/api/v1'
    const wsBase = apiBase.replace(/^http/, 'ws')
    const name = machine.name
    const wsUrl = `${wsBase}/machines/${encodeURIComponent(name)}/vnc`

    try {
      const rfb = new RFB(containerRef.current, wsUrl, { shared: true })
      rfb.viewOnly = false
      rfb.scaleViewport = true
      rfb.resizeSession = false

      rfb.addEventListener('connect', () => {
        setStatus('connected')
      })

      rfb.addEventListener('disconnect', (e) => {
        rfbRef.current = null
        if (e.detail.clean) {
          setStatus('disconnected')
        } else {
          setStatus('error')
          setErrorMsg('Connection lost unexpectedly.')
        }
      })

      rfb.addEventListener('securityfailure', (e) => {
        setStatus('error')
        setErrorMsg(`Security failure: ${e.detail.reason}`)
      })

      rfbRef.current = rfb
    } catch (err) {
      setStatus('error')
      setErrorMsg(err instanceof Error ? err.message : 'Failed to initialize VNC.')
    }
  }, [ip, machine.name])

  const disconnect = useCallback(() => {
    if (rfbRef.current) {
      rfbRef.current.disconnect()
      rfbRef.current = null
    }
    setStatus('idle')
  }, [])

  useEffect(() => {
    return () => {
      if (rfbRef.current) {
        rfbRef.current.disconnect()
        rfbRef.current = null
      }
    }
  }, [machine.name])

  if (!ip) {
    return (
      <div className="pt-[0.65rem]">
        <p className="m-0 text-ink-soft">Console is not available — this machine has no IP address assigned.</p>
      </div>
    )
  }

  return (
    <div className="pt-[0.65rem] grid gap-[0.5rem]">
      <div className="flex items-center gap-[0.5rem]">
        {(status === 'idle' || status === 'disconnected' || status === 'error') && (
          <button
            type="button"
            onClick={connect}
            className="px-[0.6rem] py-[0.25rem] text-[0.84rem] bg-brand text-white rounded cursor-pointer border-0"
          >
            Connect
          </button>
        )}
        {(status === 'connecting' || status === 'connected') && (
          <button
            type="button"
            onClick={disconnect}
            className="px-[0.6rem] py-[0.25rem] text-[0.84rem] bg-surface-raised text-ink border border-edge rounded cursor-pointer"
          >
            Disconnect
          </button>
        )}
        <span className="text-[0.82rem] text-ink-soft">
          {status === 'connecting' && 'Connecting...'}
          {status === 'connected' && `Connected to ${ip}:5900`}
          {status === 'disconnected' && 'Disconnected.'}
          {status === 'error' && (errorMsg || 'Connection error.')}
          {status === 'idle' && `VNC target: ${ip}:5900`}
        </span>
      </div>

      <div
        ref={containerRef}
        className="w-full bg-black rounded overflow-hidden"
        style={{ minHeight: status === 'connected' || status === 'connecting' ? '480px' : '0' }}
      />
    </div>
  )
}
