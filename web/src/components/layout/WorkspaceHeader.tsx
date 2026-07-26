export type WorkspaceHeaderProps = {
  quickDeploying: boolean
  onNewVM: () => void
  onOpenVMSettings: () => void
}

// VM deployment is reachable from every view, so it lives in the shared header
// rather than in the Virtual Machines toolbar.
export function WorkspaceHeader({ quickDeploying, onNewVM, onOpenVMSettings }: WorkspaceHeaderProps) {
  return (
    <header className="flex justify-end items-center gap-[0.45rem]">
      <button
        className="bg-brand border-brand-strong text-white py-[0.35rem] px-[0.55rem] text-[0.82rem]"
        disabled={quickDeploying}
        onClick={onNewVM}
      >
        {quickDeploying ? 'Deploying...' : 'New VM'}
      </button>
      <button
        className="py-[0.35rem] px-[0.55rem] text-[0.82rem]"
        disabled={quickDeploying}
        onClick={onOpenVMSettings}
      >
        VM Settings
      </button>
    </header>
  )
}
