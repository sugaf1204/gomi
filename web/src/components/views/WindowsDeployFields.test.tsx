import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { OSImagesView } from './OSImagesView'
import { VMConfigFields } from './virtual-machines/VMConfigFields'
import { initialVMConfigForm, type VMConfigForm } from './virtual-machines/vmFormState'
import { MachineSpecFields } from './machines/MachineSpecFields'
import { createInitialMachineForm } from './machines/machineFormState'
import type { OSImage } from '../../types'

const images: OSImage[] = [
  { name: 'linux', osFamily: 'debian', osVersion: '13', arch: 'amd64', format: 'qcow2', variant: 'baremetal', source: 'url', ready: true },
  { name: 'windows', osFamily: 'windows', osVersion: '11', arch: 'amd64', format: 'wim', source: 'upload', ready: true, manifest: { windows: { imageIndex: 1, winPE: { wimboot: 'wimboot', bcd: 'BCD', bootSDI: 'boot.sdi', bootWIM: 'boot.wim' } } } },
]
function Form() {
  const [form, setForm] = useState(createInitialMachineForm([]))
  return <MachineSpecFields formState={form} updateForm={setForm} radioName="machine" osImages={images} cloudInits={[]} sshKeys={[]} subnets={[]} onRefresh={vi.fn()} />
}
describe('Windows Machine configuration', () => {
  it('switches from Linux configuration to manual Windows activation and back', async () => {
    const user = userEvent.setup()
    render(<Form />)
    expect(screen.getByText('Cloud-Init')).toBeInTheDocument()
    await user.selectOptions(screen.getByLabelText('OS Image'), 'windows')
    expect(screen.getByText(/Windows deploys without a product key/)).toBeInTheDocument()
    expect(screen.queryByText('Cloud-Init')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Firmware')).toHaveValue('uefi')
    expect(screen.getByLabelText('Firmware')).toBeDisabled()
    expect(screen.getByLabelText('Register as Hypervisor')).toBeDisabled()
    expect(screen.queryByLabelText(/Target Disk/)).not.toBeInTheDocument()
    await user.selectOptions(screen.getByLabelText('OS Image'), 'linux')
    expect(screen.getByText('Cloud-Init')).toBeInTheDocument()
    expect(screen.getByLabelText('Firmware')).not.toBeDisabled()
  })
})

function VMForm({ initial = {} }: { initial?: Partial<VMConfigForm> }) {
  const [form, setForm] = useState({ ...initialVMConfigForm, ...initial })
  return <VMConfigFields formState={form} updateForm={setForm} advancedExpanded={true} setAdvancedExpanded={vi.fn()} radioNamePrefix="vm" hypervisors={[]} vmOSImages={images} cloudInits={[]} sshKeys={[]} subnets={[]} osImageByName={new Map(images.map((image) => [image.name, image]))} onRefresh={vi.fn()} bridgePlaceholder={() => ''} />
}
describe('Windows VM configuration', () => {
  it('raises Windows resource minimums and clears unsupported Linux tuning', async () => {
    const user = userEvent.setup()
    render(<VMForm initial={{ cpuCores: '1', memoryMB: '1024', diskGB: '20', diskDriver: 'virtio', ioThreads: '2', netMultiqueue: '4' }} />)
    await user.selectOptions(screen.getByLabelText('OS Image'), 'windows')
    expect(screen.getByLabelText('CPU Cores')).toHaveValue(2)
    expect(screen.getByLabelText('CPU Cores')).toHaveAttribute('min', '2')
    expect(screen.getByLabelText('Memory (MB)')).toHaveValue(4096)
    expect(screen.getByLabelText('Memory (MB)')).toHaveAttribute('min', '4096')
    expect(screen.getByLabelText('Disk (GB)')).toHaveValue(64)
    expect(screen.getByLabelText('Disk (GB)')).toHaveAttribute('min', '64')
    for (const label of ['IO Threads', 'Net Multiqueue']) {
      expect(screen.getByLabelText(label)).toHaveValue(null)
      expect(screen.getByLabelText(label)).toBeDisabled()
    }
    expect(screen.getByLabelText('Disk Driver')).toHaveValue('')
    expect(screen.getByLabelText('Disk Driver')).toBeDisabled()
    await user.selectOptions(screen.getByLabelText('OS Image'), 'linux')
    expect(screen.getByLabelText('IO Threads')).not.toBeDisabled()
    expect(screen.getByLabelText('Net Multiqueue')).not.toBeDisabled()
    expect(screen.getByLabelText('Memory (MB)')).toHaveAttribute('min', '256')
  })
  it('preserves larger explicitly configured resources when selecting Windows', async () => {
    const user = userEvent.setup()
    render(<VMForm initial={{ cpuCores: '8', memoryMB: '16384', diskGB: '200' }} />)
    await user.selectOptions(screen.getByLabelText('OS Image'), 'windows')
    expect(screen.getByLabelText('CPU Cores')).toHaveValue(8)
    expect(screen.getByLabelText('Memory (MB)')).toHaveValue(16384)
    expect(screen.getByLabelText('Disk (GB)')).toHaveValue(200)
  })

  it('shows the shared activation flow and disables static networking', async () => {
    const user = userEvent.setup()
    render(<VMForm />)
    await user.selectOptions(screen.getByLabelText('OS Image'), 'windows')
    expect(screen.getByText(/Windows deploys without a product key/)).toBeInTheDocument()
    expect(screen.queryByText('Cloud-Init')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Static')).toBeDisabled()
    await user.selectOptions(screen.getByLabelText('OS Image'), 'linux')
    expect(screen.getByText('Cloud-Init')).toBeInTheDocument()
    expect(screen.getByLabelText('Static')).not.toBeDisabled()
  })
})

describe('Windows image registration', () => {
  it('switches to upload and prevents URL selection for Windows WIM', async () => {
    const user = userEvent.setup()
    render(<OSImagesView osImages={[]} onRefresh={vi.fn()} />)
    await user.click(screen.getByRole('button', { name: 'Create' }))
    expect(screen.getByLabelText('Source')).toHaveValue('url')
    await user.selectOptions(screen.getByLabelText('Format'), 'wim')
    expect(screen.getByLabelText('Source')).toHaveValue('upload')
    expect(screen.getByLabelText('Source')).toBeDisabled()
    expect(screen.getByLabelText('OS Family')).toHaveValue('windows')
    expect(screen.getByLabelText('WinPE bootWIM')).toBeInTheDocument()
  })
})
