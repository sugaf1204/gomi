import { useEffect, type Dispatch, type SetStateAction } from 'react'
import { api } from '../api'
import type { MachineSettingsDraft, View } from '../app-types'
import type { AuditEvent, Machine } from '../types'

type SelectionSyncParams = {
  machines: Machine[]
  selectedMachine: string
  setSelectedMachine: Dispatch<SetStateAction<string>>
  selected: Machine | null
  setMachineSettingsDraft: Dispatch<SetStateAction<MachineSettingsDraft>>
  setInlineEditField: Dispatch<SetStateAction<'power' | null>>
}

type RefreshSyncParams = {
  token: string | null
  view: View
  selectedMachine: string
  activityMachineFilter: string
  refreshAll: () => Promise<void>
  refreshAudit: (machineName?: string) => Promise<void>
  setAudit: Dispatch<SetStateAction<AuditEvent[]>>
}

export function useApiTokenEffect(token: string | null) {
  useEffect(() => {
    api.setToken(token)
  }, [token])
}

export function useSelectionSyncEffects({
  machines,
  selectedMachine,
  setSelectedMachine,
  selected,
  setMachineSettingsDraft,
  setInlineEditField
}: SelectionSyncParams) {
  useEffect(() => {
    if (machines.length === 0) return
    if (!selectedMachine || !machines.some((machine) => machine.name === selectedMachine)) {
      setSelectedMachine(machines[0].name)
    }
  }, [machines, selectedMachine, setSelectedMachine])

  useEffect(() => {
    if (!selected) {
      setMachineSettingsDraft({ power: { type: 'manual' } })
      setInlineEditField(null)
      return
    }
    setMachineSettingsDraft({
      power: { ...selected.power }
    })
    setInlineEditField(null)
  }, [selected, setInlineEditField, setMachineSettingsDraft])
}

export function useRefreshEffects({
  token,
  view,
  selectedMachine,
  activityMachineFilter,
  refreshAll,
  refreshAudit,
  setAudit
}: RefreshSyncParams) {
  useEffect(() => {
    if (!token) return
    void refreshAll()
    const id = setInterval(() => void refreshAll(), 2500)
    return () => clearInterval(id)
  }, [token])

  useEffect(() => {
    if (!token) return
    if (view === 'overview') {
      void refreshAudit(undefined)
      return
    }
    if (view === 'machines') {
      if (selectedMachine) {
        void refreshAudit(selectedMachine)
      } else {
        setAudit([])
      }
      return
    }
    if (view === 'activity') {
      void refreshAudit(activityMachineFilter || undefined)
    }
  }, [token, view, selectedMachine, activityMachineFilter])
}
