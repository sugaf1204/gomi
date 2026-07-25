import { ModalOverlay } from '../../ui/ModalOverlay'
import { ConsoleTab } from '../machine-tabs/ConsoleTab'
import type { Machine } from '../../../types'

type Props = {
  machine: Machine | null
  onClose: () => void
}

/**
 * The console moved out of the tab bar into a dialog opened from the detail
 * header, so the three remaining tabs stay about the machine's own state.
 */
export function MachineConsoleDialog({ machine, onClose }: Props) {
  if (!machine) return null

  return (
    <ModalOverlay onBackdropClick={onClose}>
      <div className="w-[min(1000px,100%)] max-h-[90vh] overflow-auto bg-panel border border-line-strong p-[14px_16px]">
        <div className="flex items-baseline gap-3 mb-3">
          <p className="m-0 font-mono font-semibold text-[10px] tracking-[0.14em] text-ink-soft">CONSOLE</p>
          <p className="m-0 font-mono font-medium text-[12.5px]">{machine.name}</p>
          <button
            className="ml-auto border-0 shadow-none bg-transparent py-1 px-2 font-mono text-[11px] text-ink-soft hover:transform-none! hover:shadow-none!"
            onClick={onClose}
          >
            close
          </button>
        </div>
        <ConsoleTab machine={machine} />
      </div>
    </ModalOverlay>
  )
}
