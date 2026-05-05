import type { Dispatch, FormEvent, SetStateAction } from 'react'
import { api } from '../api'
import type { AuthFormState, ConfirmDialogState, GuardedAction, MachineSettingsDraft, SubnetFormState, View } from '../app-types'
import type { Machine, Subnet } from '../types'

type AuthHandlersParams = {
  authForm: AuthFormState
  setError: Dispatch<SetStateAction<string>>
  setToken: Dispatch<SetStateAction<string | null>>
  resetAfterLogout: () => void
}

type MachineHandlersParams = {
  selected: Machine | null
  view: View
  confirmDialog: ConfirmDialogState
  setConfirmDialog: Dispatch<SetStateAction<ConfirmDialogState>>
  setError: Dispatch<SetStateAction<string>>
  refreshAll: () => Promise<void>
  refreshAudit: (machineName?: string) => Promise<void>
  machineSettingsDirty: boolean
  machineSettingsDraft: MachineSettingsDraft
  setMachineSettingsSaving: Dispatch<SetStateAction<boolean>>
  setInlineEditField: Dispatch<SetStateAction<'power' | null>>
}

type SubnetHandlersParams = {
  subnetForm: SubnetFormState
  selectedSubnet: string
  setSelectedSubnet: Dispatch<SetStateAction<string>>
  setError: Dispatch<SetStateAction<string>>
  setSubnetFormOpen: Dispatch<SetStateAction<boolean>>
  setSubnetForm: Dispatch<SetStateAction<SubnetFormState>>
  refreshAll: () => Promise<void>
}

export function useAuthHandlers({ authForm, setError, setToken, resetAfterLogout }: AuthHandlersParams) {
  async function login(e: FormEvent) {
    e.preventDefault()
    setError('')
    try {
      const response = await api.login(authForm.username, authForm.password)
      localStorage.setItem('gomi.token', response.token)
      setToken(response.token)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed')
    }
  }

  async function setupFirstAdmin(e: FormEvent) {
    e.preventDefault()
    setError('')
    try {
      const response = await api.setupAdmin(authForm.username, authForm.password)
      localStorage.setItem('gomi.token', response.token)
      setToken(response.token)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Setup failed')
    }
  }

  async function logout() {
    try {
      await api.logout()
    } catch {}
    localStorage.removeItem('gomi.token')
    setToken(null)
    resetAfterLogout()
  }

  return { login, logout, setupFirstAdmin }
}

export function useMachineActionHandlers({
  selected,
  view,
  confirmDialog,
  setConfirmDialog,
  setError,
  refreshAll,
  refreshAudit,
  machineSettingsDirty,
  machineSettingsDraft,
  setMachineSettingsSaving,
  setInlineEditField
}: MachineHandlersParams) {
  function openConfirm(action: GuardedAction) {
    if (!selected) return
    setConfirmDialog({
      open: true,
      action,
      machineName: selected.name,
      targets: [selected.name],
      input: '',
      running: false
    })
  }

  function closeConfirm() {
    setConfirmDialog((current) => ({ ...current, open: false, targets: [], input: '', running: false }))
  }

  async function saveMachineSettings() {
    if (!selected) return
    if (!machineSettingsDirty) return
    if (!machineSettingsDraft.power.type) {
      setError('Power Type is required')
      return
    }
    setMachineSettingsSaving(true)
    setError('')
    try {
      await api.updateMachineSettings(
        selected.name,
        { power: machineSettingsDraft.power }
      )
      await refreshAll()
      await refreshAudit(selected.name)
      setInlineEditField(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update machine settings')
    } finally {
      setMachineSettingsSaving(false)
    }
  }

  async function submitConfirm() {
    if (!selected) return
    const requiresNameInput = confirmDialog.action === 'delete'
    if (requiresNameInput && confirmDialog.input.trim() !== confirmDialog.machineName) return

    setConfirmDialog((current) => ({ ...current, running: true }))
    setError('')

    try {
      if (confirmDialog.action === 'redeploy') {
        await api.redeploy(selected.name)
      } else if (confirmDialog.action === 'delete') {
        await api.deleteMachine(selected.name)
      } else if (confirmDialog.action === 'poweron') {
        await api.powerOn(selected.name)
      } else {
        await api.powerOff(selected.name)
      }

      await refreshAll()
      if (view === 'machines') {
        await refreshAudit(selected.name)
      }
      closeConfirm()
    } catch (err) {
      setConfirmDialog((current) => ({ ...current, running: false }))
      setError(err instanceof Error ? err.message : 'Action failed')
    }
  }

  return { openConfirm, closeConfirm, saveMachineSettings, submitConfirm }
}

export function useSubnetHandlers({
  subnetForm,
  selectedSubnet,
  setSelectedSubnet,
  setError,
  setSubnetFormOpen,
  setSubnetForm,
  refreshAll
}: SubnetHandlersParams) {
  async function createSubnet(e: FormEvent) {
    e.preventDefault()
    setError('')
    try {
      await api.createSubnet({
        name: subnetForm.name,
        spec: {
          cidr: subnetForm.cidr,
          defaultGateway: subnetForm.gateway || undefined,
          dnsServers: subnetForm.dnsServers ? subnetForm.dnsServers.split(',').map((s) => s.trim()).filter(Boolean) : undefined,
          vlanId: subnetForm.vlanId ? Number(subnetForm.vlanId) : undefined
        }
      })
      setSubnetFormOpen(false)
      setSubnetForm({ name: '', cidr: '', gateway: '', dnsServers: '', vlanId: '' })
      await refreshAll()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create subnet')
    }
  }

  async function deleteSubnet(name: string) {
    setError('')
    try {
      await api.deleteSubnet(name)
      if (selectedSubnet === name) setSelectedSubnet('')
      await refreshAll()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete subnet')
    }
  }

  async function updateSubnet(name: string, spec: Subnet['spec']) {
    setError('')
    try {
      await api.updateSubnet(name, spec)
      await refreshAll()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update subnet')
    }
  }

  return { createSubnet, deleteSubnet, updateSubnet }
}
