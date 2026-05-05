import { useEffect, useRef, useState, useCallback } from 'react'
import RFB from '@novnc/novnc'
import type { VirtualMachine } from '../../types'

type Props = {
  vm: VirtualMachine
  onClose: () => void
}

type Status = 'idle' | 'connecting' | 'connected' | 'disconnected' | 'error'

export function VMConsolePanel({ vm, onClose }: Props) {
  const containerRef = useRef<HTMLDivElement>(null)
  const rfbRef = useRef<RFB | null>(null)
  const [status, setStatus] = useState<Status>('idle')
  const [errorMsg, setErrorMsg] = useState('')

  const isRunning = vm.phase === 'Running' || vm.phase === 'Provisioning'

  const connect = useCallback(() => {
    if (!containerRef.current || !isRunning) return

    if (rfbRef.current) {
      rfbRef.current.disconnect()
      rfbRef.current = null
    }

    containerRef.current.innerHTML = ''
    setStatus('connecting')
    setErrorMsg('')

    const apiBase = import.meta.env.VITE_API_BASE ?? 'http://localhost:8080/api/v1'
    const wsBase = apiBase.replace(/^http/, 'ws')
    const name = vm.name
    const wsUrl = `${wsBase}/virtual-machines/${encodeURIComponent(name)}/vnc`

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
  }, [isRunning, vm.name])

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
  }, [vm.name])

  if (!isRunning) {
    return (
      <div className="p-[1rem] bg-surface-raised border border-edge rounded-lg">
        <div className="flex items-center justify-between mb-[0.5rem]">
          <h4 className="m-0 text-ink">Console: {vm.name}</h4>
          <button
            type="button"
            onClick={onClose}
            className="px-[0.5rem] py-[0.15rem] text-[0.82rem] bg-surface-raised text-ink border border-edge rounded cursor-pointer"
          >
            Close
          </button>
        </div>
        <p className="m-0 text-ink-soft text-[0.88rem]">
          Console is not available &mdash; VM is not running (phase: {vm.phase}).
        </p>
      </div>
    )
  }

  return (
    <div className="p-[1rem] bg-surface-raised border border-edge rounded-lg grid gap-[0.5rem]">
      <div className="flex items-center justify-between">
        <h4 className="m-0 text-ink">Console: {vm.name}</h4>
        <button
          type="button"
          onClick={onClose}
          className="px-[0.5rem] py-[0.15rem] text-[0.82rem] bg-surface-raised text-ink border border-edge rounded cursor-pointer"
        >
          Close
        </button>
      </div>

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
          {status === 'connected' && `Connected to ${vm.name} via hypervisor`}
          {status === 'disconnected' && 'Disconnected.'}
          {status === 'error' && (errorMsg || 'Connection error.')}
          {status === 'idle' && `VNC target: ${vm.name} (via ${vm.hypervisorName ?? vm.hypervisorRef})`}
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
