import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { GuardedAction } from './app-types'
import { api } from './api'
import { supportsDeploymentTarget } from './lib/osImages'
import { AppWorkspaceShell } from './components/layout/AppWorkspaceShell'
import { VMQuickDeploySettings } from './components/layout/VMQuickDeploySettings'
import { CommandPalette } from './components/layout/CommandPalette'
import { buildPaletteEntries, type PaletteEntry } from './lib/palette'
import { ToastRegion, type ToastItem } from './components/ui/ToastRegion'
import { AuthView } from './components/views/AuthView'
import { useAppDataState } from './hooks/useAppDataState'
import { quickDeployAuditRefreshTarget } from './hooks/quickDeployAuditRefresh'
import { useVMQuickDeploy } from './hooks/useVMQuickDeploy'
import { useAppDerivedData } from './hooks/useAppDerivedData'
import { useSelectionSyncEffects } from './hooks/useAppEffects'
import { useAuthHandlers, useMachineActionHandlers, useSubnetHandlers } from './hooks/useAppHandlers'
import { useAppUiState } from './hooks/useAppUiState'
import { useWorkspaceContentProps } from './hooks/useWorkspaceContentProps'

function actionLabel(action: GuardedAction) {
  if (action === 'redeploy') return 'Redeploy'
  if (action === 'poweron') return 'Power On'
  if (action === 'poweroff') return 'Power Off'
  return 'Delete'
}

function errorMessage(value: unknown) {
  if (value instanceof Error) return value.message
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value)
  } catch {
    return 'Unknown error'
  }
}

// Quick Deploy used to have a separate Machines-side preset. Nothing reads this
// key any more, and the stored blob can hold an IPMI username and a webhook URL,
// so it is cleared rather than left behind in every existing browser.
const REMOVED_MACHINE_QUICK_DEPLOY_STORAGE_KEY = 'gomi.machines.quick-deploy-preset'

function createToastId() {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`
}

export default function App() {
  const [token, setToken] = useState<string | null>(localStorage.getItem('gomi.token'))
  const [setupRequired, setSetupRequired] = useState(false)
  const [setupStatusLoading, setSetupStatusLoading] = useState(!token)
  const [toasts, setToasts] = useState<ToastItem[]>([])
  const lastErrorRef = useRef<{ message: string, at: number } | null>(null)
  const sessionResettingRef = useRef(false)
  const {
    selectedMachine,
    setSelectedMachine,
    selectedSubnet,
    setSelectedSubnet,
    authForm,
    view,
    setView,
    theme,
    setTheme,
    appearance,
    setAppearance,
    systemDrawerOpen,
    toggleSystemDrawer,
    paletteOpen,
    setPaletteOpen,
    machineFilter,
    setMachineFilter,
    activityTypeFilter,
    setActivityTypeFilter,
    activityMachineFilter,
    setActivityMachineFilter,
    subnetFormOpen,
    setSubnetFormOpen,
    subnetForm,
    setSubnetForm,
    machineSettingsDraft,
    setMachineSettingsDraft,
    machineSettingsSaving,
    setMachineSettingsSaving,
    inlineEditField,
    setInlineEditField,
    machineTab,
    setMachineTab,
    selectedVirtualMachineRoute,
    setSelectedVirtualMachineRoute,
    accountExpanded,
    confirmDialog,
    setConfirmDialog,
    updateAuthForm,
    updateMachineSettingsPower,
    updateSubnetForm,
    toggleSubnetForm,
    toggleAccountExpanded,
    closeAccount,
    updateConfirmDialogInput,
    resetUiAfterLogout
  } = useAppUiState()

  const {
    me,
    machines,
    audit,
    subnets,
    sshKeys,
    hypervisors,
    virtualMachines,
    cloudInits,
    osImages,
    dhcpLeases,
    dnsRecords,
    dnsRecordsError,
    systemInfo,
    error,
    setError,
    state,
    hasLoadedOnce,
    lastSyncedAt,
    refreshAll,
    refreshAudit,
    upsertMachine,
    upsertVirtualMachine,
    resetData
  } = useAppDataState({
    token,
    view,
    selectedMachine,
    activityMachineFilter,
    toErrorMessage: errorMessage
  })

  const dismissToast = useCallback((id: string) => {
    setToasts((current) => current.filter((toast) => toast.id !== id))
  }, [])

  const pushToast = useCallback((message: string, tone: ToastItem['tone'] = 'error') => {
    setToasts((current) => {
      const next = [...current, { id: createToastId(), message, tone }]
      return next.slice(-4)
    })
  }, [])

  function resetAfterLogout() {
    resetData()
    resetUiAfterLogout()
  }

  const clearSession = useCallback(() => {
    if (sessionResettingRef.current) return
    sessionResettingRef.current = true
    localStorage.removeItem('gomi.token')
    setToken(null)
    resetAfterLogout()
  }, [resetAfterLogout])

  useEffect(() => {
    if (token) {
      sessionResettingRef.current = false
    }
  }, [token])

  useEffect(() => {
    try {
      localStorage.removeItem(REMOVED_MACHINE_QUICK_DEPLOY_STORAGE_KEY)
    } catch {
      // ignore localStorage access errors
    }
  }, [])

  useEffect(() => {
    if (!error) return
    if (error === 'Session expired') {
      setError('')
      return
    }

    const now = Date.now()
    const lastError = lastErrorRef.current
    const isDuplicate = Boolean(lastError && lastError.message === error && now - lastError.at < 1000)
    if (!isDuplicate) {
      pushToast(error, 'error')
      lastErrorRef.current = { message: error, at: now }
    }

    setError('')
  }, [error, pushToast, setError])

  useEffect(() => {
    function onToast(event: Event) {
      const detail = (event as CustomEvent<{ message?: string; tone?: ToastItem['tone'] }>).detail
      if (!detail?.message) return
      pushToast(detail.message, detail.tone ?? 'error')
    }
    window.addEventListener('gomi:toast', onToast as EventListener)
    return () => window.removeEventListener('gomi:toast', onToast as EventListener)
  }, [pushToast])

  useEffect(() => {
    function onAuthExpired() {
      clearSession()
    }
    window.addEventListener('gomi:auth-expired', onAuthExpired)
    return () => window.removeEventListener('gomi:auth-expired', onAuthExpired)
  }, [clearSession])

  useEffect(() => {
    let cancelled = false
    if (token) {
      setSetupStatusLoading(false)
      return () => {
        cancelled = true
      }
    }

    setSetupStatusLoading(true)
    void api.setupStatus()
      .then((response) => {
        if (!cancelled) setSetupRequired(response.required)
      })
      .catch((err) => {
        if (!cancelled) pushToast(err instanceof Error ? err.message : 'Failed to check setup status')
      })
      .finally(() => {
        if (!cancelled) setSetupStatusLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [token, pushToast])

  const {
    selected,
    selectedSubnetData,
    filteredActivityItems,
    activitySummary,
    machineSettingsDirty,
    machineStats,
    filteredMachines
  } = useAppDerivedData({
    machines,
    selectedMachine,
    machineSettingsDraft,
    subnets,
    selectedSubnet,
    audit,
    activityTypeFilter,
    activityMachineFilter,
    machineFilter
  })

  useSelectionSyncEffects({
    machines,
    selectedMachine,
    setSelectedMachine,
    selected,
    setMachineSettingsDraft,
    setInlineEditField
  })

  const { login, logout, setupFirstAdmin } = useAuthHandlers({
    authForm,
    setError,
    setToken,
    resetAfterLogout
  })

  const { openConfirm, closeConfirm, saveMachineSettings, submitConfirm } = useMachineActionHandlers({
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
  })

  const { createSubnet, deleteSubnet, updateSubnet } = useSubnetHandlers({
    subnetForm,
    selectedSubnet,
    setSelectedSubnet,
    setError,
    setSubnetFormOpen,
    setSubnetForm,
    refreshAll
  })

  const vmOSImages = useMemo(() => osImages.filter((img) => supportsDeploymentTarget(img, 'vm')), [osImages])

  // Rebuilt every render on purpose: useVMQuickDeploy holds this in a ref it
  // reassigns each render, so the deploy reads the view and filter as they are
  // when it finishes rather than when it started.
  const refreshQuickDeployAudit = async () => {
    const target = quickDeployAuditRefreshTarget(view, activityMachineFilter)
    if (!target) return
    await refreshAudit(target.machineName)
  }

  const quickDeploy = useVMQuickDeploy({
    token,
    virtualMachines,
    vmOSImages,
    onVirtualMachineUpsert: upsertVirtualMachine,
    refreshAll,
    refreshAuditIfVisible: refreshQuickDeployAudit
  })

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setPaletteOpen(true)
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [setPaletteOpen])

  const paletteEntries = buildPaletteEntries({
    machines: machines.map((machine) => machine.name),
    virtualMachines: virtualMachines.map((vm) => vm.name),
    subnets: subnets.map((subnet) => subnet.name)
  })

  function jumpTo(entry: PaletteEntry) {
    setView(entry.view)
    if (!entry.target) return
    if (entry.view === 'machines') setSelectedMachine(entry.target)
    if (entry.view === 'virtual-machines') setSelectedVirtualMachineRoute(entry.target)
    if (entry.view === 'network') setSelectedSubnet(entry.target)
  }

  const workspaceContentProps = useWorkspaceContentProps({
    quickDeploy,
    refreshAll,
    lastSyncedAt,
    dataLoading: !hasLoadedOnce && state === 'loading',
    machines,
    audit,
    subnets,
    sshKeys,
    hypervisors,
    virtualMachines,
    upsertVirtualMachine,
    cloudInits,
    osImages,
    dhcpLeases,
    dnsRecords,
    dnsRecordsError,
    systemInfo,
    me,
    theme,
    setTheme,
    view,
    appearance,
    setAppearance,
    machineStats,
    machineFilter,
    setMachineFilter,
    filteredMachines,
    selectedMachine,
    setSelectedMachine,
    upsertMachine,
    selected,
    openConfirm,
    saveMachineSettings,
    machineSettingsDirty,
    machineSettingsSaving,
    inlineEditField,
    setInlineEditField,
    machineSettingsPower: machineSettingsDraft.power,
    updateMachineSettingsPower,
    selectedVirtualMachineRoute,
    setSelectedVirtualMachineRoute,
    machineTab,
    setMachineTab,
    activityTypeFilter,
    setActivityTypeFilter,
    activityMachineFilter,
    setActivityMachineFilter,
    filteredActivityItems,
    activitySummary,
    subnetFormOpen,
    toggleSubnetForm,
    subnetForm,
    updateSubnetForm,
    createSubnet,
    selectedSubnet,
    setSelectedSubnet,
    deleteSubnet,
    updateSubnet,
    selectedSubnetData
  })

  if (!token) {
    return (
      <>
        <AuthView
          mode={setupRequired ? 'setup' : 'login'}
          checking={setupStatusLoading}
          authForm={authForm}
          onAuthFormChange={updateAuthForm}
          onSubmit={setupRequired ? setupFirstAdmin : login}
        />
        <ToastRegion toasts={toasts} onDismiss={dismissToast} />
      </>
    )
  }

  return (
    <>
      <AppWorkspaceShell
        sidebar={{
          view,
          onViewChange: setView,
          counts: {
            machines: machines.length,
            virtualMachines: virtualMachines.length,
            hypervisors: hypervisors.length,
            subnets: subnets.length,
            osImages: osImages.length,
            cloudInits: cloudInits.length,
            dnsRecords: dnsRecords.length
          },
          environmentLabel: systemInfo?.hostname,
          username: me?.username,
          role: me?.role,
          accountExpanded,
          onToggleAccount: toggleAccountExpanded,
          onCloseAccount: closeAccount,
          onLogout: logout,
          systemOpen: systemDrawerOpen,
          onToggleSystem: toggleSystemDrawer,
          onOpenPalette: () => setPaletteOpen(true)
        }}
        workspace={{
          view,
          ...workspaceContentProps
        }}
        loading={!hasLoadedOnce && state === 'loading'}
        confirmDialog={{
          open: confirmDialog.open,
          title: `Confirm ${actionLabel(confirmDialog.action)}`,
          machineName: confirmDialog.machineName,
          targets: confirmDialog.targets,
          input: confirmDialog.input,
          running: confirmDialog.running,
          confirmLabel: actionLabel(confirmDialog.action),
          context: selected ? {
            osFamily: selected.osPreset.family,
            osVersion: selected.osPreset.version,
            currentPhase: selected.phase
          } : undefined,
          onInputChange: updateConfirmDialogInput,
          onCancel: closeConfirm,
          onConfirm: () => void submitConfirm()
        }}
      />
      <VMQuickDeploySettings
        quickDeploy={quickDeploy}
        hypervisors={hypervisors}
        osImages={osImages}
        cloudInits={cloudInits}
        sshKeys={sshKeys}
        subnets={subnets}
        onRefresh={refreshAll}
      />
      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        entries={paletteEntries}
        onSelect={jumpTo}
      />
      <ToastRegion toasts={toasts} onDismiss={dismissToast} />
    </>
  )
}
