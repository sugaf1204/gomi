import { Sidebar, type SidebarProps } from './Sidebar'
import { WorkspaceContent, type WorkspaceContentProps } from './WorkspaceContent'
import { ConfirmDialog, type ConfirmDialogProps } from '../ui/ConfirmDialog'

type Props = {
  sidebar: SidebarProps
  workspace: WorkspaceContentProps
  loading: boolean
  confirmDialog: ConfirmDialogProps
}

export function AppWorkspaceShell({ sidebar, workspace, loading, confirmDialog }: Props) {
  return (
    <main className="app-shell min-h-screen h-screen overflow-hidden grid grid-cols-1 md:grid-cols-[240px_minmax(0,1fr)]">
      <Sidebar {...sidebar} />
      <WorkspaceContent {...workspace} />
      {loading && <div className="loading-bar" />}
      <ConfirmDialog {...confirmDialog} />
    </main>
  )
}
