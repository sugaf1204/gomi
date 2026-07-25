import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { DeployCard } from './DeployCard'
import { deriveDeployTimeline, type DeployTimeline } from '../../lib/deploy-timeline'
import { NOT_STARTED_COLOR, PHASE_COLORS, RUNNING_COLORS, FAILED_COLOR } from './phase-colors'
import type { ProvisionProgress } from '../../types'

const T0 = Date.parse('2026-07-26T10:00:00.000Z')
const at = (offsetMs: number) => new Date(T0 + offsetMs).toISOString()

/** An attempt that has reached image-apply and is still running there. */
function inProgressProvision(): ProvisionProgress {
  return {
    active: true,
    startedAt: at(0),
    lastSignalAt: at(180_000),
    timings: [
      { source: 'server', name: 'server.pxe.boot_script', timestamp: at(30_000) },
      { source: 'server', name: 'server.inventory', timestamp: at(90_000) },
      { source: 'server', name: 'server.artifact_transfer', timestamp: at(150_000) },
    ],
  }
}

function completedProvision(): ProvisionProgress {
  return {
    active: false,
    startedAt: at(0),
    completedAt: at(300_000),
    timings: [
      { source: 'server', name: 'server.pxe.boot_script', timestamp: at(30_000) },
      { source: 'server', name: 'server.inventory', timestamp: at(90_000) },
      { source: 'server', name: 'server.artifact_transfer', timestamp: at(150_000) },
      { source: 'server', name: 'server.reboot_to_os', timestamp: at(260_000) },
    ],
  }
}

function failedProvision(): ProvisionProgress {
  return {
    active: false,
    startedAt: at(0),
    finishedAt: at(200_000),
    failureReason: 'artifact transfer aborted',
    timings: [
      { source: 'server', name: 'server.pxe.boot_script', timestamp: at(30_000) },
      { source: 'server', name: 'server.inventory', timestamp: at(90_000) },
      { source: 'server', name: 'server.artifact_transfer', timestamp: at(150_000), result: 'failure' },
    ],
  }
}

const cells = () => screen.getAllByTestId('deploy-band-cell')

function fillOf(element: HTMLElement) {
  return element.style.backgroundColor
}

function rgb(hex: string) {
  const value = hex.replace('#', '')
  const n = parseInt(value, 16)
  return `rgb(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255})`
}

describe('DeployCard', () => {
  it('renders nothing for an empty timeline', () => {
    const timeline = deriveDeployTimeline(undefined, T0)
    expect(timeline.status).toBe('empty')
    const { container } = render(<DeployCard timeline={timeline} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing when the timeline has no segments', () => {
    const timeline: DeployTimeline = {
      status: 'in-progress',
      startMs: T0,
      endMs: T0,
      totalMs: 0,
      segments: [],
      events: [],
      notices: [],
    }
    const { container } = render(<DeployCard timeline={timeline} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders one band child per reached phase plus a not-started child', () => {
    const timeline = deriveDeployTimeline(inProgressProvision(), T0 + 240_000)
    render(<DeployCard timeline={timeline} />)
    // Reached power-on, installer-boot, inventory-config, image-apply (4) + 1 not-started.
    expect(cells()).toHaveLength(5)
    const phases = cells().map((cell) => cell.dataset.phase)
    expect(phases).toEqual(['power-on', 'installer-boot', 'inventory-config', 'image-apply', 'not-started'])
    expect(fillOf(cells()[4])).toBe(NOT_STARTED_COLOR.fill)
  })

  it('omits the not-started child once every phase has been reached', () => {
    const timeline = deriveDeployTimeline(completedProvision(), T0 + 300_000)
    render(<DeployCard timeline={timeline} />)
    expect(cells()).toHaveLength(5)
    expect(cells().map((cell) => cell.dataset.phase)).not.toContain('not-started')
  })

  it('gives the running phase the running tone and not the done tone', () => {
    const timeline = deriveDeployTimeline(inProgressProvision(), T0 + 240_000)
    render(<DeployCard timeline={timeline} />)
    const running = cells().find((cell) => cell.dataset.phase === 'image-apply')!
    expect(running.dataset.state).toBe('running')
    expect(fillOf(running)).toBe(rgb(RUNNING_COLORS['image-apply'].fill))
    expect(fillOf(running)).not.toBe(rgb(PHASE_COLORS['image-apply'].fill))
    // An earlier phase is done, so it keeps the saturated fill.
    const done = cells().find((cell) => cell.dataset.phase === 'installer-boot')!
    expect(done.dataset.state).toBe('done')
    expect(fillOf(done)).toBe(rgb(PHASE_COLORS['installer-boot'].fill))
  })

  it('shows the failed tone and the FAILED status for a failed attempt', () => {
    const timeline = deriveDeployTimeline(failedProvision(), T0 + 240_000)
    expect(timeline.status).toBe('failed')
    render(<DeployCard timeline={timeline} />)
    expect(screen.getByText(/DEPLOY · FAILED/)).toBeInTheDocument()
    const failed = cells().find((cell) => cell.dataset.state === 'failed')
    expect(failed).toBeDefined()
    expect(fillOf(failed!)).toBe(rgb(FAILED_COLOR.fill))
  })

  it('shows the in-progress status and the current phase label', () => {
    const timeline = deriveDeployTimeline(inProgressProvision(), T0 + 240_000)
    render(<DeployCard timeline={timeline} />)
    expect(screen.getByText(/DEPLOY · IN PROGRESS/)).toBeInTheDocument()
    // The label also appears in the legend, so match the header's warn-toned copy.
    const headerLabel = screen
      .getAllByText('Image transfer & apply')
      .find((node) => node.className.includes('text-warn'))
    expect(headerLabel).toBeDefined()
  })

  it('shows the complete status without a current phase label', () => {
    const timeline = deriveDeployTimeline(completedProvision(), T0 + 300_000)
    render(<DeployCard timeline={timeline} />)
    expect(screen.getByText(/DEPLOY · COMPLETE/)).toBeInTheDocument()
  })

  it('lists five legend phases with durations', () => {
    const timeline = deriveDeployTimeline(completedProvision(), T0 + 300_000)
    render(<DeployCard timeline={timeline} />)
    const entries = screen.getAllByTestId('deploy-legend-entry')
    expect(entries).toHaveLength(5)
    for (const label of [
      'Power-on & PXE boot',
      'Installer boot',
      'Inventory & config',
      'Image transfer & apply',
      'Reboot into target OS',
    ]) {
      expect(entries.some((entry) => entry.textContent?.includes(label))).toBe(true)
    }
    // Every entry carries a duration; none is the not-started placeholder.
    for (const entry of entries) {
      expect(entry.textContent).not.toContain('—')
    }
    expect(entries[0].textContent).toContain('30.0 s')
  })

  it('marks phases the attempt never reached as having no duration', () => {
    const timeline = deriveDeployTimeline(inProgressProvision(), T0 + 240_000)
    render(<DeployCard timeline={timeline} />)
    const entries = screen.getAllByTestId('deploy-legend-entry')
    expect(entries).toHaveLength(5)
    // reboot-os is the only phase not reached.
    expect(entries[4].textContent).toContain('—')
  })

  it('renders attemptId and lastSignal when supplied', () => {
    const timeline = deriveDeployTimeline(inProgressProvision(), T0 + 240_000)
    render(
      <DeployCard
        timeline={timeline}
        attemptId="attempt-gpu01-91cd"
        lastSignal="last signal 3 s ago · server.artifact_transfer"
      />,
    )
    expect(screen.getByText(/attempt-gpu01-91cd/)).toBeInTheDocument()
    expect(screen.getByText('last signal 3 s ago · server.artifact_transfer')).toBeInTheDocument()
  })

  it('omits attemptId and lastSignal when not supplied', () => {
    const timeline = deriveDeployTimeline(inProgressProvision(), T0 + 240_000)
    render(<DeployCard timeline={timeline} />)
    expect(screen.queryByText(/attempt-gpu01-91cd/)).not.toBeInTheDocument()
    expect(screen.queryByText(/last signal/)).not.toBeInTheDocument()
  })

  it('renders the elapsed total in T+ form', () => {
    const timeline = deriveDeployTimeline(inProgressProvision(), T0 + 240_000)
    render(<DeployCard timeline={timeline} />)
    expect(screen.getByText(/T\+4m00s/)).toBeInTheDocument()
  })

  it('never renders an animated element', () => {
    for (const provision of [inProgressProvision(), completedProvision(), failedProvision()]) {
      const { container, unmount } = render(
        <DeployCard
          timeline={deriveDeployTimeline(provision, T0 + 240_000)}
          attemptId="attempt-gpu01-91cd"
          lastSignal="last signal 3 s ago · server.artifact_transfer"
        />,
      )
      expect(container.querySelectorAll('.animate-pulse')).toHaveLength(0)
      expect(container.querySelectorAll('[class*="animate-"]')).toHaveLength(0)
      expect(container.querySelectorAll('[class*="transition"]')).toHaveLength(0)
      unmount()
    }
  })
})
