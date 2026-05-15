import { useState, useEffect, useMemo, useRef } from 'react'
import clsx from 'clsx'
import { api } from '../../api'
import { formatDate, phaseClass } from '../../lib/formatters'
import { usePersistentStringState } from '../../hooks/usePersistentStringState'
import { notifyError } from '../../lib/toast'
import type { Hypervisor } from '../../types'
import { ModalOverlay } from '../ui/ModalOverlay'

type HypervisorPrimaryAction = 'delete'

export type HypervisorsViewProps = {
  hypervisors: Hypervisor[]
  onRefresh: () => void | Promise<void>
}

type BatchDeleteConfirmState = {
  open: boolean
  targets: string[]
  running: boolean
}

const initialBatchDeleteConfirm: BatchDeleteConfirmState = {
  open: false,
  targets: [],
  running: false
}

const HYPERVISOR_SELECTION_STORAGE_KEY = 'gomi.hypervisors.selected'

export function HypervisorsView({ hypervisors, onRefresh }: HypervisorsViewProps) {
  const [selected, setSelected] = usePersistentStringState(HYPERVISOR_SELECTION_STORAGE_KEY)
  const [checkedHVs, setCheckedHVs] = useState<Set<string>>(new Set())
  const [regToken, setRegToken] = useState<{ token: string; expiresAt: string } | null>(null)
  const [regTokenOpen, setRegTokenOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const [batchDeleteConfirm, setBatchDeleteConfirm] = useState<BatchDeleteConfirmState>(initialBatchDeleteConfirm)
  const [actionsMenuOpen, setActionsMenuOpen] = useState(false)
  const actionsMenuRef = useRef<HTMLDivElement | null>(null)

  const checkedNames = useMemo(
    () => hypervisors.filter((hv) => checkedHVs.has(hv.name)).map((hv) => hv.name),
    [checkedHVs, hypervisors]
  )
  const selectedHypervisor = hypervisors.find((h) => h.name === selected) ?? null

  useEffect(() => {
    if (selected && hypervisors.length > 0 && !hypervisors.some((hypervisor) => hypervisor.name === selected)) {
      setSelected('')
    }
  }, [selected, hypervisors])

  useEffect(() => {
    if (!regTokenOpen && !batchDeleteConfirm.open) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        setRegTokenOpen(false)
        if (!batchDeleteConfirm.running) {
          setBatchDeleteConfirm(initialBatchDeleteConfirm)
        }
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [regTokenOpen, batchDeleteConfirm.open, batchDeleteConfirm.running])

  useEffect(() => {
    if (!actionsMenuOpen) return
    function onWindowMouseDown(event: MouseEvent) {
      if (!actionsMenuRef.current) return
      if (event.target instanceof Node && !actionsMenuRef.current.contains(event.target)) {
        setActionsMenuOpen(false)
      }
    }
    function onWindowKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setActionsMenuOpen(false)
      }
    }
    window.addEventListener('mousedown', onWindowMouseDown)
    window.addEventListener('keydown', onWindowKeyDown)
    return () => {
      window.removeEventListener('mousedown', onWindowMouseDown)
      window.removeEventListener('keydown', onWindowKeyDown)
    }
  }, [actionsMenuOpen])

  function runPrimaryAction(action: HypervisorPrimaryAction) {
    const targets = checkedNames.length > 0 ? checkedNames : (selectedHypervisor ? [selectedHypervisor.name] : [])
    if (targets.length === 0) return
    switch (action) {
      case 'delete':
        setBatchDeleteConfirm({ open: true, targets: [...targets].sort(), running: false })
        break
    }
  }

  async function submitBatchDeleteConfirm() {
    if (batchDeleteConfirm.targets.length === 0) return

    const targets = [...batchDeleteConfirm.targets]
    setBatchDeleteConfirm((current) => ({ ...current, running: true }))

    try {
      const failures: string[] = []
      for (const name of targets) {
        try {
          await api.deleteHypervisor(name)
          if (selected === name) setSelected('')
        } catch {
          failures.push(name)
        }
      }

      if (failures.length > 0) {
        notifyError(`Failed to delete: ${failures.join(', ')}`)
        setCheckedHVs(new Set(failures))
      } else {
        setCheckedHVs(new Set())
      }

      setBatchDeleteConfirm(initialBatchDeleteConfirm)
      void onRefresh()
    } catch {
      setBatchDeleteConfirm(initialBatchDeleteConfirm)
    }
  }

  function toggleChecked(name: string) {
    setCheckedHVs((prev) => {
      const next = new Set(prev)
      if (next.has(name)) {
        next.delete(name)
      } else {
        next.add(name)
      }
      return next
    })
  }

  function toggleAllChecked() {
    if (checkedHVs.size === hypervisors.length) {
      setCheckedHVs(new Set())
    } else {
      setCheckedHVs(new Set(hypervisors.map((hv) => hv.name)))
    }
  }

  function getRegistrationCommand(token: string): string {
    const gomiServer = window.location.origin
    return `curl -sf ${gomiServer}/api/v1/hypervisors/setup-and-register.sh | sudo GOMI_SERVER=${gomiServer} GOMI_TOKEN=${token} bash`
  }

  async function handleCopyCommand(token: string) {
    const text = getRegistrationCommand(token)
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      const textarea = document.createElement('textarea')
      textarea.value = text
      textarea.style.position = 'fixed'
      textarea.style.opacity = '0'
      document.body.appendChild(textarea)
      textarea.select()
      document.execCommand('copy')
      document.body.removeChild(textarea)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  async function handleAdd() {
    try {
      const result = await api.createRegistrationToken()
      setRegToken(result)
      setRegTokenOpen(true)
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to create registration token')
    }
  }

  return (
    <>
      {regTokenOpen && regToken && (
        <ModalOverlay onBackdropClick={() => { setRegTokenOpen(false) }}>
          <div className="w-[min(560px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
            <div className="flex justify-between items-center">
              <h3 className="text-[1.2rem]">Add Hypervisor</h3>
              <button
                aria-label="Close"
                className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!"
                onClick={() => setRegTokenOpen(false)}
              >×</button>
            </div>
            <p className="m-0 text-[0.84rem] text-ink-soft">Run the following command on the hypervisor host to register it with GoMI.</p>
            <p className="m-0 text-ink-soft text-[0.82rem]">Expires: {formatDate(regToken.expiresAt)} (1 hour)</p>
            <p className="m-0 text-[#b5762a] text-[0.82rem]">This command is single-use and cannot be retrieved again.</p>
            <div className="p-[0.65rem] border border-line bg-[#1e1e2e] rounded">
              <div className="flex justify-end mb-[0.35rem]">
                <button
                  className="bg-[#3a3a4e] border-[#555] text-[#e0e0e8] py-[0.2rem] px-[0.5rem] text-[0.76rem] shrink-0"
                  onClick={() => void handleCopyCommand(regToken.token)}
                >
                  {copied ? 'Copied!' : 'Copy'}
                </button>
              </div>
              <pre className="m-0 text-[0.78rem] font-mono text-[#c8d0e0] whitespace-pre-wrap break-all leading-[1.5]">{getRegistrationCommand(regToken.token)}</pre>
            </div>
            <div className="flex justify-end pt-[0.2rem]">
              <button onClick={() => setRegTokenOpen(false)}>Close</button>
            </div>
          </div>
        </ModalOverlay>
      )}

      {batchDeleteConfirm.open && (
        <ModalOverlay onBackdropClick={() => {
          if (!batchDeleteConfirm.running) {
            setBatchDeleteConfirm(initialBatchDeleteConfirm)
          }
        }}>
          <div className="w-[min(520px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem]">
            <h3 className="text-[1.2rem] text-[#9b2d2d]">Confirm Delete</h3>
            <p className="m-0 text-ink-soft text-[0.84rem]">Target hypervisors ({batchDeleteConfirm.targets.length}):</p>
            <div className="max-h-[180px] overflow-auto border border-line p-[0.55rem] bg-[#f9f7f4]">
              <ul className="m-0 pl-[1.1rem]">
                {batchDeleteConfirm.targets.map((target) => (
                  <li key={target}><code>{target}</code></li>
                ))}
              </ul>
            </div>
            <div className="flex justify-end gap-[0.45rem]">
              <button type="button" onClick={() => setBatchDeleteConfirm(initialBatchDeleteConfirm)} disabled={batchDeleteConfirm.running}>Cancel</button>
              <button
                type="button"
                onClick={() => void submitBatchDeleteConfirm()}
                disabled={batchDeleteConfirm.running}
                className="bg-[#d86b6b] border-[#be5252] text-white"
              >
                {batchDeleteConfirm.running ? 'Deleting...' : 'Delete'}
              </button>
            </div>
          </div>
        </ModalOverlay>
      )}

      <section className="min-h-0 grid grid-cols-1 md:grid-cols-[310px_minmax(0,1fr)] gap-[0.9rem]">
        <div className="min-h-0 grid grid-rows-[auto_minmax(0,1fr)] gap-[0.6rem]">
          <div className="grid gap-[0.45rem]">
            <div className="flex justify-between items-center gap-2">
              <h2 className="text-[1.4rem]">Hypervisors</h2>
              <button
                className="bg-brand border-brand-strong text-white py-[0.35rem] px-[0.55rem] text-[0.82rem]"
                onClick={() => void handleAdd()}
              >
                Add
              </button>
            </div>
          </div>

          <div className="overflow-auto border-t border-line pr-[0.1rem]">
            {hypervisors.length > 0 && (
              <div className="flex items-center gap-[0.45rem] py-[0.35rem] pl-[0.55rem] border-b border-line bg-[#f9f7f4]">
                <input
                  type="checkbox"
                  className="w-[0.95rem] h-[0.95rem] m-0 shrink-0 accent-[#2b7a78] cursor-pointer"
                  checked={checkedHVs.size === hypervisors.length && hypervisors.length > 0}
                  onChange={toggleAllChecked}
                />
                <span className="text-[0.78rem] text-ink-soft">Select All</span>
              </div>
            )}
            {hypervisors.map((hv) => (
              <div
                key={hv.name}
                className={clsx(
                  'flex items-start border-0 border-b border-line border-l-[3px] border-l-transparent',
                  selected === hv.name && '!border-l-brand bg-[rgba(43,122,120,0.06)]'
                )}
              >
                <div className="flex items-center pl-[0.55rem] pt-[0.72rem] shrink-0">
                  <input
                    type="checkbox"
                    className="w-[0.95rem] h-[0.95rem] m-0 accent-[#2b7a78] cursor-pointer"
                    checked={checkedHVs.has(hv.name)}
                    onChange={() => toggleChecked(hv.name)}
                  />
                </div>
                <button
                  className="text-left flex-1 min-w-0 border-0 bg-transparent flex justify-between items-start gap-[0.75rem] py-[0.62rem] pl-[0.45rem] pr-[0.1rem] shadow-none hover:transform-none!"
                  onClick={() => setSelected(hv.name)}
                >
                  <div className="min-w-0">
                    <p className="m-0 font-ui font-medium tracking-normal truncate">{hv.name}</p>
                    <p className="m-0 text-ink-soft text-[0.82rem]">{hv.connection.host} - {hv.vmCount} VMs</p>
                  </div>
                  <span className={clsx(phaseClass(hv.phase), 'shrink-0')}>{hv.phase}</span>
                </button>
              </div>
            ))}
            {hypervisors.length === 0 && <p className="m-0 text-ink-soft">No hypervisors found</p>}
          </div>
        </div>

        <div className="min-h-0 grid content-start gap-[0.85rem]">
          {!selectedHypervisor && checkedNames.length === 0 && (
            <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] grid gap-[0.45rem]">
              <h2>Select a hypervisor</h2>
              <p>Choose a hypervisor from the list to view details and actions.</p>
            </section>
          )}

          {(selectedHypervisor || checkedNames.length > 0) && (
            <>
              <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] flex justify-between items-start gap-4">
                <div>
                  <p className="m-0 font-ui font-medium text-[0.72rem] uppercase tracking-[0.08em] text-ink-soft">Hypervisor</p>
                  {checkedNames.length > 0 ? (
                    <h2 className="mt-[0.22rem] text-[1.8rem]">{checkedNames.length} selected</h2>
                  ) : selectedHypervisor && (
                    <>
                      <h2 className="mt-[0.22rem] text-[1.8rem]">{selectedHypervisor.name}</h2>
                      <p className="m-0 text-ink-soft">{selectedHypervisor.connection.host}:{selectedHypervisor.connection.port}</p>
                    </>
                  )}
                </div>
                <div className="flex flex-col items-end gap-[0.55rem]">
                  {checkedNames.length === 0 && selectedHypervisor && (
                    <span className={phaseClass(selectedHypervisor.phase)}>{selectedHypervisor.phase}</span>
                  )}
                  <div className="flex justify-end items-center flex-wrap gap-[0.35rem]" ref={actionsMenuRef}>
                    <div className="relative">
                      <button
                        className="py-[0.45rem] px-[0.72rem]"
                        disabled={checkedNames.length === 0 && !selectedHypervisor}
                        onClick={() => setActionsMenuOpen((current) => !current)}
                      >
                        Actions
                      </button>
                      {actionsMenuOpen && (
                        <div className="absolute right-0 mt-1 min-w-[180px] bg-white border border-line shadow-[0_10px_24px_rgba(52,43,34,0.16)] z-10">
                          {([
                            { value: 'delete', label: 'Delete' }
                          ] as Array<{ value: HypervisorPrimaryAction, label: string }>).map((item) => (
                            <button
                              key={item.value}
                              className={clsx(
                                'w-full text-left border-0 shadow-none rounded-none px-[0.7rem] py-[0.5rem]',
                                item.value === 'delete'
                                  ? 'text-[#9b2d2d] hover:bg-[#fff3f2]'
                                  : 'text-ink hover:bg-[#f7f3ed]'
                              )}
                              onClick={() => {
                                setActionsMenuOpen(false)
                                runPrimaryAction(item.value)
                              }}
                            >
                              {item.label}
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              </section>

              {selectedHypervisor && (
                <>
                  <section className="grid grid-cols-1 lg:grid-cols-2 gap-[0.85rem]">
                    <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                      <h3 className="mb-[0.58rem] text-[1.05rem]">Connection</h3>
                      <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                        <dt className="text-ink-soft text-[0.84rem]">Type</dt><dd className="m-0">{selectedHypervisor.connection.type.toUpperCase()}</dd>
                        <dt className="text-ink-soft text-[0.84rem]">Host</dt><dd className="m-0">{selectedHypervisor.connection.host}</dd>
                        <dt className="text-ink-soft text-[0.84rem]">Port</dt><dd className="m-0">{selectedHypervisor.connection.port}</dd>
                        {selectedHypervisor.libvirtURI && (
                          <><dt className="text-ink-soft text-[0.84rem]">Libvirt URI</dt><dd className="m-0"><code className="text-[0.82rem]">{selectedHypervisor.libvirtURI}</code></dd></>
                        )}
                      </dl>
                    </article>

                    <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                      <h3 className="mb-[0.58rem] text-[1.05rem]">Status</h3>
                      <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                        <dt className="text-ink-soft text-[0.84rem]">Phase</dt><dd className="m-0"><span className={phaseClass(selectedHypervisor.phase)}>{selectedHypervisor.phase}</span></dd>
                        <dt className="text-ink-soft text-[0.84rem]">VM Count</dt><dd className="m-0">{selectedHypervisor.vmCount}</dd>
                        <dt className="text-ink-soft text-[0.84rem]">Last Heartbeat</dt><dd className="m-0">{formatDate(selectedHypervisor.lastHeartbeat)}</dd>
                        {selectedHypervisor.lastError && (
                          <><dt className="text-ink-soft text-[0.84rem]">Last Error</dt><dd className="m-0 text-error">{selectedHypervisor.lastError}</dd></>
                        )}
                      </dl>
                    </article>

                    {selectedHypervisor.capacity && (
                      <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                        <h3 className="mb-[0.58rem] text-[1.05rem]">Capacity</h3>
                        <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                          <dt className="text-ink-soft text-[0.84rem]">CPU Cores</dt><dd className="m-0">{selectedHypervisor.capacity.cpuCores}</dd>
                          <dt className="text-ink-soft text-[0.84rem]">Memory (MB)</dt><dd className="m-0">{selectedHypervisor.capacity.memoryMB}</dd>
                          <dt className="text-ink-soft text-[0.84rem]">Storage (GB)</dt><dd className="m-0">{selectedHypervisor.capacity.storageGB ?? '-'}</dd>
                        </dl>
                      </article>
                    )}

                    {selectedHypervisor.used && (
                      <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                        <h3 className="mb-[0.58rem] text-[1.05rem]">Usage</h3>
                        <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                          <dt className="text-ink-soft text-[0.84rem]">CPU Used</dt><dd className="m-0">{selectedHypervisor.used.cpuUsedCores}</dd>
                          <dt className="text-ink-soft text-[0.84rem]">Memory Used (MB)</dt><dd className="m-0">{selectedHypervisor.used.memoryUsedMB}</dd>
                          <dt className="text-ink-soft text-[0.84rem]">Storage Used (GB)</dt><dd className="m-0">{selectedHypervisor.used.storageUsedGB ?? '-'}</dd>
                        </dl>
                      </article>
                    )}
                  </section>

                  <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                    <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                      <dt className="text-ink-soft text-[0.84rem]">Created</dt><dd className="m-0">{formatDate(selectedHypervisor.createdAt)}</dd>
                      <dt className="text-ink-soft text-[0.84rem]">Updated</dt><dd className="m-0">{formatDate(selectedHypervisor.updatedAt)}</dd>
                    </dl>
                  </section>
                </>
              )}
            </>
          )}
        </div>
      </section>
    </>
  )
}
