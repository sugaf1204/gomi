import { useEffect, type Dispatch, type SetStateAction } from 'react'
import { api } from '../api'
import type { MachineSettingsDraft, View } from '../app-types'
import type { AuditEvent, Machine } from '../types'

// Delay between dashboard refreshes, measured from when the previous refresh
// settles (see the self-scheduling loop below).
const REFRESH_INTERVAL_MS = 5000

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
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    // Self-scheduling loop instead of setInterval: the next refresh is only
    // queued after the previous one settles, so slow refreshes never stack up
    // into overlapping bursts of the (multi-endpoint) dashboard fetch.
    const tick = async () => {
      await refreshAll()
      if (!cancelled) {
        timer = setTimeout(() => void tick(), REFRESH_INTERVAL_MS)
      }
    }
    void tick()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [token])

  useEffect(() => {
    if (!token) return
    if (view === 'overview') {
      // The overview renders only aggregate counts; it never displays audit
      // events, so skip fetching them here. Fetching the full activity feed
      // (all pages) on the default landing view added many list round-trips
      // for data that is never shown.
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
