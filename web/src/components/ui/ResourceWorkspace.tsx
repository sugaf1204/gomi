import type { ReactNode } from 'react'

/** Shared index/detail geometry for compute resources. Each pane owns its scroll. */
export function ResourceWorkspace({ index, children }: { index: ReactNode; children: ReactNode }) {
  return <section className="h-full min-h-0 grid grid-cols-1 md:grid-cols-[352px_minmax(0,1fr)]">
    {index}
    {children}
  </section>
}
