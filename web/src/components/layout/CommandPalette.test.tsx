import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { CommandPalette } from './CommandPalette'
import type { PaletteEntry } from '../../lib/palette'

function makeEntries(): PaletteEntry[] {
  return [
    { id: 'nav:machines', label: 'Machines', kind: 'nav', view: 'machines' },
    { id: 'nav:virtual-machines', label: 'Virtual Machines', kind: 'nav', view: 'virtual-machines' },
    { id: 'machine:gpu-machine-01', label: 'gpu-machine-01', kind: 'machine', view: 'machines', target: 'gpu-machine-01' },
    { id: 'vm:vm-alpha', label: 'vm-alpha', kind: 'virtual machine', view: 'virtual-machines', target: 'vm-alpha' },
    { id: 'subnet:lan-1', label: 'lan-1', kind: 'subnet', view: 'network', target: 'lan-1' },
  ]
}

describe('CommandPalette', () => {
  it('renders nothing when closed', () => {
    const { container } = render(
      <CommandPalette open={false} onClose={vi.fn()} entries={makeEntries()} onSelect={vi.fn()} />
    )
    expect(container).toBeEmptyDOMElement()
  })

  it('filters entries as the user types', async () => {
    const user = userEvent.setup()
    render(<CommandPalette open onClose={vi.fn()} entries={makeEntries()} onSelect={vi.fn()} />)

    expect(screen.getAllByRole('option')).toHaveLength(5)

    await user.type(screen.getByPlaceholderText('jump to…'), 'alpha')

    const options = screen.getAllByRole('option')
    expect(options).toHaveLength(1)
    expect(options[0]).toHaveTextContent('vm-alpha')
  })

  it('moves the highlighted row with arrow keys', async () => {
    const user = userEvent.setup()
    render(<CommandPalette open onClose={vi.fn()} entries={makeEntries()} onSelect={vi.fn()} />)

    const input = screen.getByPlaceholderText('jump to…')
    const options = screen.getAllByRole('option')
    expect(options[0]).toHaveAttribute('aria-selected', 'true')

    await user.type(input, '{ArrowDown}')
    expect(screen.getAllByRole('option')[1]).toHaveAttribute('aria-selected', 'true')
    expect(screen.getAllByRole('option')[0]).toHaveAttribute('aria-selected', 'false')

    await user.type(input, '{ArrowUp}')
    expect(screen.getAllByRole('option')[0]).toHaveAttribute('aria-selected', 'true')
  })

  it('does not wrap around past the first or last row', async () => {
    const user = userEvent.setup()
    render(<CommandPalette open onClose={vi.fn()} entries={makeEntries()} onSelect={vi.fn()} />)

    const input = screen.getByPlaceholderText('jump to…')

    // At the top already; ArrowUp should not wrap to the bottom.
    await user.type(input, '{ArrowUp}')
    expect(screen.getAllByRole('option')[0]).toHaveAttribute('aria-selected', 'true')

    // Move to the last row, then try to go past it.
    await user.type(input, '{ArrowDown}{ArrowDown}{ArrowDown}{ArrowDown}')
    const lastIndex = screen.getAllByRole('option').length - 1
    expect(screen.getAllByRole('option')[lastIndex]).toHaveAttribute('aria-selected', 'true')

    await user.type(input, '{ArrowDown}')
    expect(screen.getAllByRole('option')[lastIndex]).toHaveAttribute('aria-selected', 'true')
  })

  it('Enter selects the highlighted entry and closes', async () => {
    const user = userEvent.setup()
    const onSelect = vi.fn()
    const onClose = vi.fn()
    render(<CommandPalette open onClose={onClose} entries={makeEntries()} onSelect={onSelect} />)

    const input = screen.getByPlaceholderText('jump to…')
    await user.type(input, '{ArrowDown}{Enter}')

    expect(onSelect).toHaveBeenCalledTimes(1)
    expect(onSelect).toHaveBeenCalledWith(makeEntries()[1])
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('Escape closes', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(<CommandPalette open onClose={onClose} entries={makeEntries()} onSelect={vi.fn()} />)

    await user.type(screen.getByPlaceholderText('jump to…'), '{Escape}')
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('clicking a row selects it', async () => {
    const user = userEvent.setup()
    const onSelect = vi.fn()
    const onClose = vi.fn()
    render(<CommandPalette open onClose={onClose} entries={makeEntries()} onSelect={onSelect} />)

    await user.click(screen.getByText('lan-1'))

    expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ id: 'subnet:lan-1' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('clicking the backdrop closes it', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(<CommandPalette open onClose={onClose} entries={makeEntries()} onSelect={vi.fn()} />)

    await user.click(screen.getByRole('dialog'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('resets the query each time it opens', () => {
    const { rerender } = render(
      <CommandPalette open={false} onClose={vi.fn()} entries={makeEntries()} onSelect={vi.fn()} />
    )
    rerender(<CommandPalette open onClose={vi.fn()} entries={makeEntries()} onSelect={vi.fn()} />)
    expect(screen.getByPlaceholderText('jump to…')).toHaveValue('')
  })

  it('caps the rendered list at 12 rows', () => {
    const manyEntries: PaletteEntry[] = Array.from({ length: 30 }, (_, i) => ({
      id: `machine:host-${i}`,
      label: `host-${i}`,
      kind: 'machine',
      view: 'machines',
      target: `host-${i}`,
    }))

    render(<CommandPalette open onClose={vi.fn()} entries={manyEntries} onSelect={vi.fn()} />)

    expect(screen.getAllByRole('option')).toHaveLength(12)
  })
})
