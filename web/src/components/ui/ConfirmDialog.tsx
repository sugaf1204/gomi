export type ConfirmDialogContext = {
  osFamily?: string
  osVersion?: string
  currentPhase?: string
}

export type ConfirmDialogProps = {
  open: boolean
  title: string
  machineName: string
  targets: string[]
  input: string
  running: boolean
  confirmLabel: string
  context?: ConfirmDialogContext
  onInputChange: (value: string) => void
  onCancel: () => void
  onConfirm: () => void
}

export function ConfirmDialog({
  open,
  title,
  machineName,
  targets,
  input,
  running,
  confirmLabel,
  context,
  onInputChange,
  onCancel,
  onConfirm
}: ConfirmDialogProps) {
  if (!open) return null

  const isRedeploy = confirmLabel === 'Redeploy'
  const requiresNameInput = confirmLabel === 'Delete'
  const isRunning = context?.currentPhase?.toLowerCase() === 'running' || context?.currentPhase?.toLowerCase() === 'ready'

  return (
    <div className="fixed inset-0 bg-[rgba(30,28,24,0.38)] grid place-items-center z-20 p-4" role="dialog" aria-modal="true">
      <div className="w-[min(430px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[0.95rem] grid gap-[0.6rem]">
        <h3 className="text-[1.3rem]">{title}</h3>

        {isRedeploy && context && (
          <div className="grid gap-[0.3rem] bg-[#f9f7f4] border border-line p-[0.6rem] text-[0.84rem]">
            {context.osFamily && (
              <p className="m-0 text-ink-soft">OS Preset: <strong className="text-ink">{context.osFamily} {context.osVersion}</strong></p>
            )}
            {context.currentPhase && (
              <p className="m-0 text-ink-soft">Current Phase: <strong className="text-ink">{context.currentPhase}</strong></p>
            )}
            {isRunning && (
              <p className="m-0 text-[#8a4f23] font-medium mt-[0.15rem]">This machine is currently active. Redeploying will disrupt services.</p>
            )}
          </div>
        )}

        <p className="m-0 text-ink-soft text-[0.84rem]">Targets ({targets.length || 1})</p>
        <div className="max-h-[160px] overflow-auto border border-line p-[0.55rem] bg-[#f9f7f4]">
          <ul className="m-0 pl-[1.1rem]">
            {(targets.length > 0 ? targets : [machineName]).map((target) => (
              <li key={target}><code>{target}</code></li>
            ))}
          </ul>
        </div>

        {requiresNameInput && (
          <>
            <p className="m-0 text-ink-soft">Type <strong>{machineName}</strong> to continue.</p>
            <input
              aria-label="Machine confirmation"
              value={input}
              onChange={(e) => onInputChange(e.target.value)}
              placeholder={machineName}
            />
          </>
        )}
        <div className="flex justify-end gap-[0.45rem]">
          <button onClick={onCancel} disabled={running}>Cancel</button>
          <button
            className="bg-[#d86b6b] border-[#be5252] text-white"
            onClick={onConfirm}
            disabled={running || (requiresNameInput && input.trim() !== machineName)}
          >
            {running ? 'Running...' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
