import { useEffect, useState } from 'react'
import { api } from '../api'
import type { VirtualMachine } from '../types'
import {
  buildCreateVMPayload,
  buildVMNetworkPayload
} from '../components/views/virtual-machines/useVirtualMachineOperations'
import {
  invalidVMConfigReason,
  quickDeployPresetReady,
  quickDeployVMName,
  readQuickDeployPreset,
  renderPresetTemplateName,
  writeQuickDeployPreset
} from '../components/views/virtual-machines/quickDeployPreset'
import { mergeSelectedCloudInitRef } from '../components/views/virtual-machines/vmFormState'
import type { QuickDeployPreset } from '../components/views/virtual-machines/vmFormState'
import type { ToastTone } from '../components/ui/ToastRegion'

type Params = {
  virtualMachines: VirtualMachine[]
  // VM-capable images only: a preset naming an image the dialog cannot offer
  // must not pass validation. Only `name` is read, so the narrow shape stands
  // in for OSImage[].
  vmOSImages: { name: string }[]
  onVirtualMachineUpsert: (virtualMachine: VirtualMachine) => void
  refreshAll: () => void | Promise<void>
  // refreshAll does not fetch audit events, and the audit effect is keyed on
  // view/filter changes that a header deploy does not cause. Without this the
  // server's create-vm event stays off an already-open Activity timeline.
  refreshAuditIfVisible: () => void | Promise<void>
}

export type VMQuickDeploy = {
  preset: QuickDeployPreset
  setPreset: React.Dispatch<React.SetStateAction<QuickDeployPreset>>
  settingsOpen: boolean
  openSettings: () => void
  closeSettings: () => void
  deploying: boolean
  nextVMName: (preset: QuickDeployPreset) => string
  presetReady: (preset: QuickDeployPreset) => boolean
  deploy: () => Promise<void>
}

function notify(message: string, tone: ToastTone) {
  window.dispatchEvent(new CustomEvent('gomi:toast', { detail: { tone, message } }))
}

// Deploying a VM is a workspace-level action rather than a Virtual Machines view
// action, so the preset and the deploy call live here and are driven from the
// shared header. Unlike the in-view version this replaced, it deliberately does
// not select the new VM: that only makes sense while the VM list is on screen.
export function useVMQuickDeploy({
  virtualMachines,
  vmOSImages,
  onVirtualMachineUpsert,
  refreshAll,
  refreshAuditIfVisible
}: Params): VMQuickDeploy {
  const [preset, setPreset] = useState<QuickDeployPreset>(readQuickDeployPreset)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [deploying, setDeploying] = useState(false)

  useEffect(() => {
    writeQuickDeployPreset(preset)
  }, [preset])

  useEffect(() => {
    if (!settingsOpen) return
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape' && !deploying) setSettingsOpen(false)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [settingsOpen, deploying])

  const presetReady = (current: QuickDeployPreset) => quickDeployPresetReady(current, vmOSImages)

  // hostname is substituted into the template name only. The user-data body is
  // stored verbatim so cloud-init renders its own Jinja on the target.
  async function resolveCloudInitRefs(current: QuickDeployPreset, hostname: string) {
    if (current.cloudInitMode === 'none') return [] as string[]
    if (current.cloudInitMode === 'existing') {
      return mergeSelectedCloudInitRef(current.cloudInitExistingRef.trim(), [])
    }
    const templateName = renderPresetTemplateName(current.cloudInitTemplateName.trim(), hostname)
    const userData = current.cloudInitUserData.trim()
    if (!templateName || !userData) throw new Error('Cloud-Init inline creation requires both template name and user-data')
    const created = await api.createCloudInitTemplate({
      name: templateName,
      description: 'Auto-generated from Quick Deploy preset',
      userData
    })
    return mergeSelectedCloudInitRef(created.name, [])
  }

  async function deploy() {
    if (!presetReady(preset)) {
      // Reopen the dialog so the preset can be corrected, and name the reason
      // when there is a specific one rather than just a missing required field.
      const reason = invalidVMConfigReason(preset)
      if (reason) notify(reason, 'error')
      setSettingsOpen(true)
      return
    }
    const vmName = quickDeployVMName(preset)

    // Reject a name we already know is taken before resolveCloudInitRefs runs.
    // In 'create' mode that call upserts a template, so letting the request
    // reach the server's 409 would overwrite the existing VM's template (or
    // leave an orphan) even though no VM is created. Reset Count makes this
    // collision easy to hit deliberately.
    if (virtualMachines.some((vm) => vm.name === vmName)) {
      notify(`A VM named ${vmName} already exists. Adjust the preset name or count.`, 'error')
      setSettingsOpen(true)
      return
    }

    // Reserve the next name before awaiting so a rapid second click cannot
    // reuse this one. The server rejects duplicates with 409 as a backstop.
    setPreset((current) => ({ ...current, count: String(Math.max(1, Number(current.count) || 1) + 1) }))

    setDeploying(true)
    try {
      const cloudInitRefs = await resolveCloudInitRefs(preset, vmName)
      const vmNetwork = buildVMNetworkPayload(preset)
      const result = await api.createVirtualMachine({
        ...buildCreateVMPayload(preset, cloudInitRefs, vmNetwork),
        name: vmName
      })
      onVirtualMachineUpsert(result)
      await refreshAll()
      await refreshAuditIfVisible()
      // The deploy can be triggered from any view, so a toast is often the only
      // sign that anything happened.
      if (result.phase === 'Error') {
        notify(`${vmName} created but deploy failed: ${result.lastError || 'deploy failed'}`, 'error')
      } else {
        notify(`Deploying ${vmName}`, 'info')
      }
    } catch (err) {
      notify(err instanceof Error ? err.message : `Failed to deploy ${vmName}`, 'error')
    } finally {
      setDeploying(false)
    }
  }

  return {
    preset,
    setPreset,
    settingsOpen,
    openSettings: () => setSettingsOpen(true),
    closeSettings: () => setSettingsOpen(false),
    deploying,
    nextVMName: quickDeployVMName,
    presetReady,
    deploy
  }
}
