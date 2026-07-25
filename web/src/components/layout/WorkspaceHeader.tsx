import clsx from 'clsx'
import type { Appearance, View } from '../../app-types'
import { buildBreadcrumb } from '../../lib/breadcrumb'
import { AppearanceToggle } from './AppearanceToggle'

export type DeployChip = {
  count: number
  machineName: string
  elapsed: string
}

export type WorkspaceHeaderProps = {
  view: View
  selectedObject?: string
  syncedLabel?: string
  deploying?: DeployChip
  appearance: Appearance
  onAppearanceChange: (appearance: Appearance) => void
}

const crumbClass = {
  ancestor: 'text-ink-soft',
  current: 'text-ink font-medium',
  object: 'text-brand-accent font-medium',
} as const

export function WorkspaceHeader({
  view,
  selectedObject,
  syncedLabel,
  deploying,
  appearance,
  onAppearanceChange,
}: WorkspaceHeaderProps) {
  const crumbs = buildBreadcrumb(view, selectedObject)

  return (
    <header className="h-[52px] shrink-0 flex items-center gap-3 px-5 border-b border-line bg-panel-2">
      <nav aria-label="Breadcrumb" className="flex items-center font-mono text-[11.5px] min-w-0">
        {crumbs.map((crumb, index) => (
          <span key={`${crumb.label}-${index}`} className="flex items-center min-w-0">
            {index > 0 && <span className="text-line-strong px-[6px]">/</span>}
            <span className={clsx('truncate', crumbClass[crumb.kind])}>{crumb.label}</span>
          </span>
        ))}
      </nav>

      {/* Static dot, no animation: progress is expressed by tone only. */}
      {deploying && (
        <span className="flex items-center gap-[6px] shrink-0 border border-warn-line bg-panel px-[7px] py-[3px] font-mono font-medium text-[10.5px] text-warn">
          <span className="w-[6px] h-[6px] shrink-0 bg-warn" aria-hidden="true" />
          {deploying.count} deploying · {deploying.machineName} · {deploying.elapsed}
        </span>
      )}

      <div className="ml-auto flex items-center gap-3 shrink-0">
        {syncedLabel && <span className="font-mono text-[10.5px] text-ink-soft">{syncedLabel}</span>}
        <AppearanceToggle appearance={appearance} onAppearanceChange={onAppearanceChange} />
      </div>
    </header>
  )
}
