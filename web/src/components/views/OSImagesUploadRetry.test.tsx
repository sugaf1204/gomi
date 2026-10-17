import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { api } from '../../api'
import { OSImagesView } from './OSImagesView'
import type { OSImage } from '../../types'

afterEach(() => vi.restoreAllMocks())

describe('Windows image companion upload recovery', () => {
  it('retries only pending assets on the registered image after closing and reopening', async () => {
    const user = userEvent.setup()
    const image: OSImage = { name: 'windows-test', osFamily: 'windows', osVersion: '11', arch: 'amd64', format: 'wim', source: 'upload', ready: false }
    const create = vi.spyOn(api, 'createOSImage').mockResolvedValue(image)
    const root = vi.spyOn(api, 'uploadOSImageFile').mockResolvedValue(image)
    let failBCD = true
    const companion = vi.spyOn(api, 'uploadOSImageArtifact').mockImplementation(async (_name, path) => {
      if (path === 'BCD' && failBCD) {
        failBCD = false
        throw new Error('Network interrupted during BCD upload')
      }
      return image
    })
    const refresh = vi.fn()
    render(<OSImagesView osImages={[]} onRefresh={refresh} />)
    await user.click(screen.getByRole('button', { name: 'Create' }))
    await user.type(screen.getByLabelText('Name'), 'windows-test')
    await user.selectOptions(screen.getByLabelText('Format'), 'wim')
    const manifest = {
      bootModes: ['uefi'], root: { format: 'wim', path: 'install.wim' },
      windows: { imageIndex: 1, winPE: { wimboot: 'wimboot', bcd: 'BCD', bootSDI: 'boot.sdi', bootWIM: 'boot.wim' } },
    }
    const manifestField = screen.getByLabelText(/^Manifest JSON/)
    await user.click(manifestField)
    await user.paste(JSON.stringify(manifest))
    await user.upload(screen.getByLabelText('File'), new File(['root-image'], 'install.wim'))
    for (const key of ['wimboot', 'bcd', 'bootSDI', 'bootWIM']) {
      await user.upload(screen.getByLabelText('WinPE ' + key), new File(['asset'], key))
    }
    await user.click(screen.getByRole('button', { name: 'Create & Upload' }))
    await screen.findByRole('button', { name: 'Retry uploads' })
    expect(create).toHaveBeenCalledTimes(1)
    expect(root).toHaveBeenCalledTimes(1)
    expect(companion.mock.calls.map((call) => call[1])).toEqual(['wimboot', 'BCD'])
    expect(screen.getByLabelText('Name')).toBeDisabled()
    expect(manifestField).toBeDisabled()
    expect(screen.getByLabelText('File')).toBeDisabled()
    expect(screen.getByLabelText('WinPE wimboot')).toBeDisabled()
    expect(screen.getByLabelText('WinPE bcd')).not.toBeDisabled()
    await user.upload(screen.getByLabelText('WinPE bcd'), new File(['replacement-BCD'], 'BCD'))
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await user.click(screen.getByRole('button', { name: 'Create' }))
    expect(screen.getByLabelText('Name')).toHaveValue('windows-test')
    expect(screen.getByLabelText('Name')).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Retry uploads' }))
    await waitFor(() => expect(screen.queryByText('Create OS Image')).not.toBeInTheDocument())
    expect(create).toHaveBeenCalledTimes(1)
    expect(root).toHaveBeenCalledTimes(1)
    expect(companion.mock.calls.map((call) => call[1])).toEqual(['wimboot', 'BCD', 'BCD', 'boot.sdi', 'boot.wim'])
    expect(companion.mock.calls.every((call) => call[0] === image.name)).toBe(true)
    expect(companion.mock.calls[2][2].size).toBe('replacement-BCD'.length)
  })
})
