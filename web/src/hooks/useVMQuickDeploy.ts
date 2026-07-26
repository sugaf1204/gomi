import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import type { VirtualMachine } from '../types'
import {
  buildCreateVMPayload,
  buildVMNetworkPayload
} from '../components/views/virtual-machines/useVirtualMachineOperations'
import {
  clearQuickDeploySecrets,
  invalidVMConfigReason,
  quickDeployPresetReady,
  quickDeployVMName,
  readQuickDeployPreset,
  renderPresetTemplateName,
  writeQuickDeployPreset
} from '../components/views/virtual-machines/quickDeployPreset'
import { isStaleDeploySession } from './quickDeploySession'
import { mergeSelectedCloudInitRef } from '../components/views/virtual-machines/vmFormState'
import type { QuickDeployPreset } from '../components/views/virtual-machines/vmFormState'
import type { ToastTone } from '../components/ui/ToastRegion'

type Params = {
  // The hook is mounted above App's unauthenticated early return, so it stays
  // alive across a logout. Changing identity clears the session-scoped state.
  token: string | null
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
  //
  // Called after the deploy settles, so it must read the view and filter as
  // they are at that moment, not as they were when the deploy started.
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
  token,
  virtualMachines,
  vmOSImages,
  onVirtualMachineUpsert,
  refreshAll,
  refreshAuditIfVisible
}: Params): VMQuickDeploy {
  const [preset, setPreset] = useState<QuickDeployPreset>(readQuickDeployPreset)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [deploying, setDeploying] = useState(false)

  // deploy() awaits twice before calling this, so the callback captured at
  // deploy time can be stale: the user may have changed the Activity filter or
  // navigated into Activity while the request was in flight.
  const refreshAuditRef = useRef(refreshAuditIfVisible)
  refreshAuditRef.current = refreshAuditIfVisible

  useEffect(() => {
    writeQuickDeployPreset(preset)
  }, [preset])

  // Identifies the session a deploy was started in. A deploy that outlives its
  // session must not touch the next one, so the continuation compares this
  // after every await instead of trusting that it is still relevant.
  const sessionRef = useRef(0)

  // The password and inline user-data are kept out of localStorage because they
  // are secrets; leaving them in memory would hand them to whoever logs in next
  // in the same tab. The rest of the preset is already persisted and is not
  // session-scoped, so only the secrets and the open dialog are cleared.
  useEffect(() => {
    sessionRef.current += 1
    setPreset(clearQuickDeploySecrets)
    setSettingsOpen(false)
    // An in-flight deploy belongs to the session that started it. Its own
    // finally would clear this eventually, but not before the new session is
    // left with both header buttons disabled — indefinitely if the request
    // hangs, since fetch has no timeout here.
    setDeploying(false)
  }, [token])

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

    const session = sessionRef.current
    // The request cannot be recalled, so the server may still create the VM.
    // What this guards is the continuation: reporting a previous session's
    // deploy into the current one, or writing its VM into a list the new user
    // is not entitled to see.
    const sessionEnded = () => isStaleDeploySession(session, sessionRef.current)

    setDeploying(true)
    try {
      const cloudInitRefs = await resolveCloudInitRefs(preset, vmName)
      if (sessionEnded()) return
      const vmNetwork = buildVMNetworkPayload(preset)
      const result = await api.createVirtualMachine({
        ...buildCreateVMPayload(preset, cloudInitRefs, vmNetwork),
        name: vmName
      })
      if (sessionEnded()) return
      onVirtualMachineUpsert(result)
      await refreshAll()
      if (sessionEnded()) return
      await refreshAuditRef.current()
      // The deploy can be triggered from any view, so a toast is often the only
      // sign that anything happened.
      if (result.phase === 'Error') {
        notify(`${vmName} created but deploy failed: ${result.lastError || 'deploy failed'}`, 'error')
      } else {
        notify(`Deploying ${vmName}`, 'info')
      }
    } catch (err) {
      if (sessionEnded()) return
      notify(err instanceof Error ? err.message : `Failed to deploy ${vmName}`, 'error')
    } finally {
      // Only the owning session's deploy may clear the flag: a stale one
      // finishing here would otherwise re-enable buttons the current session
      // had legitimately disabled for its own deploy.
      if (!sessionEnded()) setDeploying(false)
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
