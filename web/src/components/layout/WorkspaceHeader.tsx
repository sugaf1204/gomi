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

const VIEW_COPY: Record<View, { title: string; description: string }> = {
  machines: { title: 'Bare-metal machines', description: 'Deploy, inspect, and control physical hosts' },
  hypervisors: { title: 'Hypervisors', description: 'Capacity and placement hosts for virtual machines' },
  'virtual-machines': { title: 'Virtual machines', description: 'Create and operate guest workloads' },
  activity: { title: 'Activity', description: 'Review recent operator and system changes' },
  network: { title: 'Subnets', description: 'Address space, leases, and machine assignment' },
  'dns-records': { title: 'DNS records', description: 'Names managed by the control plane' },
  'cloud-init': { title: 'Cloud-init', description: 'Reusable first-boot configuration' },
  'os-images': { title: 'OS images', description: 'Deployment artifacts available to managed machines' },
  users: { title: 'Users and access', description: 'Credentials used to reach managed systems' },
  settings: { title: 'Settings', description: 'Workspace appearance and account preferences' },
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
  const copy = VIEW_COPY[view]

  return (
    <header className="min-h-[68px] shrink-0 flex items-center gap-4 px-5 py-3 border-b border-line bg-panel-2">
      <div className="min-w-0 grid gap-[2px]">
        {selectedObject ? (
          <nav aria-label="Breadcrumb" className="flex items-center font-mono text-[10px] min-w-0 uppercase tracking-[0.1em]">
            {crumbs.map((crumb, index) => (
              <span key={`${crumb.label}-${index}`} className="flex items-center min-w-0">
                {index > 0 && <span className="text-line-strong px-[6px]">/</span>}
                <span className={clsx('truncate', crumbClass[crumb.kind])}>{crumb.label}</span>
              </span>
            ))}
          </nav>
        ) : (
          <h1 className="text-[17px] leading-tight font-semibold">{copy.title}</h1>
        )}
        <p className="m-0 text-[11.5px] text-ink-soft truncate">{selectedObject ? copy.title : copy.description}</p>
      </div>

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
