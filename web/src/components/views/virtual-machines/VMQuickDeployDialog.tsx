import type { Dispatch, ReactNode, SetStateAction } from 'react'
import { ModalOverlay } from '../../ui/ModalOverlay'
import type { QuickDeployPreset } from './vmFormState'

type VMQuickDeployDialogProps = {
  open: boolean
  preset: QuickDeployPreset
  setPreset: Dispatch<SetStateAction<QuickDeployPreset>>
  quickDeploying: boolean
  nextName: (preset: QuickDeployPreset) => string
  isReady: (preset: QuickDeployPreset) => boolean
  onClose: () => void
  onDeploy: () => void
  children: ReactNode
}

export function VMQuickDeployDialog({
  open,
  preset,
  setPreset,
  quickDeploying,
  nextName,
  isReady,
  onClose,
  onDeploy,
  children
}: VMQuickDeployDialogProps) {
  if (!open) return null

  return (
    <ModalOverlay onBackdropClick={() => { if (!quickDeploying) onClose() }}>
      <div className="w-[min(680px,100%)] bg-panel border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem] max-h-[90vh] overflow-auto">
        <div className="flex justify-between items-center">
          <div>
            <h3 className="text-[1.2rem]">Quick Deploy Preset</h3>
            <p className="m-0 text-ink-soft text-[0.82rem]">Next VM: <code>{preset.name.trim() ? nextName(preset) : '-'}</code></p>
          </div>
          <button
            aria-label="Close"
            className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!"
            disabled={quickDeploying}
            onClick={onClose}
          >x</button>
        </div>

        <div className="grid gap-[0.55rem]">
          <div className="grid grid-cols-1 sm:grid-cols-[minmax(0,1fr)_130px] gap-[0.55rem]">
            <label className="text-[0.84rem] min-w-0">
              Name
              <input required value={preset.name} onChange={(e) => setPreset((current) => ({ ...current, name: e.target.value }))} placeholder="e.g. devvm" />
            </label>
            <label className="text-[0.84rem] min-w-0">
              Count
              <input type="number" min="1" value={preset.count} onChange={(e) => setPreset((current) => ({ ...current, count: e.target.value }))} />
            </label>
          </div>

          {children}

          <p className="m-0 text-[0.78rem] text-ink-soft">
            Cloud-Init user-data is not saved with the preset because it may contain secrets. Template name and mode are saved.
          </p>

          <div className="flex justify-between gap-[0.45rem] pt-[0.2rem]">
            <button type="button" onClick={() => setPreset((current) => ({ ...current, count: '1' }))} disabled={quickDeploying}>Reset Count</button>
            <div className="flex justify-end gap-[0.45rem]">
              <button type="button" onClick={onClose} disabled={quickDeploying}>Close</button>
              <button
                type="button"
                className="bg-brand border-brand-strong text-white"
                disabled={quickDeploying || !isReady(preset)}
                onClick={() => {
                  onClose()
                  onDeploy()
                }}
              >
                {quickDeploying ? 'Deploying...' : 'Deploy Now'}
              </button>
            </div>
          </div>
        </div>
      </div>
    </ModalOverlay>
  )
}
