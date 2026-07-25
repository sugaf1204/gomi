import { useEffect, useState } from 'react'
import type { ActivityType, Appearance, GroupBy, AuthFormState, ConfirmDialogState, MachineSettingsDraft, MachineTab, SubnetFormState, Theme, View } from '../app-types'
import type { PowerConfig } from '../types'
import { usePersistentStringState } from './usePersistentStringState'

const views: Set<string> = new Set(['overview', 'machines', 'hypervisors', 'virtual-machines', 'activity', 'network', 'dns-records', 'cloud-init', 'os-images', 'users', 'settings'])
const themes: Set<string> = new Set(['default', 'rounded'])
const appearances: Set<string> = new Set(['light', 'dark'])
const MACHINE_SELECTION_STORAGE_KEY = 'gomi.machines.selected'
const SYSTEM_DRAWER_STORAGE_KEY = 'gomi.rail.system-open'
const GROUP_BY_STORAGE_KEY = 'gomi.machines.group-by'
const groupByValues: Set<string> = new Set(['subnet', 'hypervisor', 'phase'])
const SUBNET_SELECTION_STORAGE_KEY = 'gomi.network.selected-subnet'
const VIRTUAL_MACHINES_PATH_SEGMENT = 'virtual-machines'

function normalizePathname(pathname: string): string {
  if (!pathname || pathname === '/') return '/'
  return pathname.endsWith('/') ? pathname.slice(0, -1) : pathname
}

function parsePathname(pathname: string): { view: View, selectedVirtualMachineRoute: string } {
  const normalized = normalizePathname(pathname)
  if (normalized === '/') {
    return { view: 'overview', selectedVirtualMachineRoute: '' }
  }

  const [head = '', tail = ''] = normalized.split('/').filter(Boolean)
  const view = views.has(head) ? (head as View) : 'overview'
  if (view === VIRTUAL_MACHINES_PATH_SEGMENT) {
    try {
      return { view, selectedVirtualMachineRoute: decodeURIComponent(tail) }
    } catch {
      return { view, selectedVirtualMachineRoute: tail }
    }
  }
  return { view, selectedVirtualMachineRoute: '' }
}

function readRouteFromLocation(): { view: View, selectedVirtualMachineRoute: string } {
  return parsePathname(window.location.pathname)
}

function buildPathname(view: View, selectedVirtualMachineRoute: string): string {
  if (view === VIRTUAL_MACHINES_PATH_SEGMENT && selectedVirtualMachineRoute) {
    return `/${VIRTUAL_MACHINES_PATH_SEGMENT}/${encodeURIComponent(selectedVirtualMachineRoute)}`
  }
  return `/${view}`
}

function readThemeFromStorage(): Theme {
  const value = localStorage.getItem('gomi.theme')
  return themes.has(value ?? '') ? (value as Theme) : 'default'
}

function readAppearanceFromStorage(): Appearance {
  const value = localStorage.getItem('gomi.appearance')
  if (appearances.has(value ?? '')) return value as Appearance
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

const initialAuthForm: AuthFormState = { username: '', password: '' }
const initialSubnetForm: SubnetFormState = { name: '', cidr: '', gateway: '', dnsServers: '', vlanId: '' }
const initialMachineSettingsDraft: MachineSettingsDraft = { power: { type: 'manual' } }
const initialConfirmDialog: ConfirmDialogState = {
  open: false,
  action: 'redeploy',
  machineName: '',
  targets: [],
  input: '',
  running: false
}

export function useAppUiState() {
  const initialRoute = readRouteFromLocation()
  const [selectedMachine, setSelectedMachine] = usePersistentStringState(MACHINE_SELECTION_STORAGE_KEY)
  const [selectedSubnet, setSelectedSubnet] = usePersistentStringState(SUBNET_SELECTION_STORAGE_KEY)
  const [authForm, setAuthForm] = useState(initialAuthForm)
  const [view, setViewState] = useState<View>(initialRoute.view)
  const [selectedVirtualMachineRoute, setSelectedVirtualMachineRouteState] = useState<string>(initialRoute.selectedVirtualMachineRoute)
  const [theme, setThemeState] = useState<Theme>(readThemeFromStorage)
  const [appearance, setAppearanceState] = useState<Appearance>(readAppearanceFromStorage)
  const [systemDrawerRaw, setSystemDrawerRaw] = usePersistentStringState(SYSTEM_DRAWER_STORAGE_KEY)
  const systemDrawerOpen = systemDrawerRaw === 'open'
  const [machineFilter, setMachineFilter] = useState('')
  const [activityTypeFilter, setActivityTypeFilter] = useState<ActivityType>('all')
  const [activityMachineFilter, setActivityMachineFilter] = useState('')
  const [subnetFormOpen, setSubnetFormOpen] = useState(false)
  const [subnetForm, setSubnetForm] = useState(initialSubnetForm)
  const [machineSettingsDraft, setMachineSettingsDraft] = useState(initialMachineSettingsDraft)
  const [machineSettingsSaving, setMachineSettingsSaving] = useState(false)
  const [inlineEditField, setInlineEditField] = useState<'power' | null>(null)
  const [accountExpanded, setAccountExpanded] = useState(false)
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [groupByRaw, setGroupByRaw] = usePersistentStringState(GROUP_BY_STORAGE_KEY)
  const groupBy: GroupBy = groupByValues.has(groupByRaw) ? (groupByRaw as GroupBy) : 'subnet'
  const [machineTab, setMachineTab] = useState<MachineTab>('overview')
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState>(initialConfirmDialog)

  function setView(v: View) {
    setViewState(v)
    if (v !== VIRTUAL_MACHINES_PATH_SEGMENT) {
      setSelectedVirtualMachineRouteState('')
    }
    const nextPath = buildPathname(v, v === VIRTUAL_MACHINES_PATH_SEGMENT ? selectedVirtualMachineRoute : '')
    if (window.location.pathname !== nextPath) {
      window.history.pushState({}, '', nextPath)
    }
  }

  function setSelectedVirtualMachineRoute(value: string) {
    const nextValue = value.trim()
    setSelectedVirtualMachineRouteState(nextValue)
    if (view !== VIRTUAL_MACHINES_PATH_SEGMENT) return
    const nextPath = buildPathname(view, nextValue)
    if (window.location.pathname !== nextPath) {
      window.history.pushState({}, '', nextPath)
    }
  }

  function setTheme(next: Theme) {
    setThemeState(next)
    localStorage.setItem('gomi.theme', next)
  }

  function setAppearance(next: Appearance) {
    setAppearanceState(next)
    localStorage.setItem('gomi.appearance', next)
  }

  function toggleAppearance() {
    setAppearance(appearance === 'dark' ? 'light' : 'dark')
  }

  function toggleSystemDrawer() {
    setSystemDrawerRaw(systemDrawerOpen ? '' : 'open')
  }

  useEffect(() => {
    function onPopState() {
      const route = readRouteFromLocation()
      setViewState(route.view)
      setSelectedVirtualMachineRouteState(route.selectedVirtualMachineRoute)
    }
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])

  useEffect(() => {
    const canonical = buildPathname(view, selectedVirtualMachineRoute)
    if (window.location.pathname !== canonical) {
      window.history.replaceState({}, '', canonical)
    }
  }, [view, selectedVirtualMachineRoute])

  useEffect(() => {
    document.documentElement.dataset.theme = theme
  }, [theme])

  useEffect(() => {
    document.documentElement.dataset.appearance = appearance
  }, [appearance])

  function updateAuthForm(field: keyof AuthFormState, value: string) {
    setAuthForm((current) => ({ ...current, [field]: value }))
  }

  function updateMachineSettingsPower(value: PowerConfig) {
    setMachineSettingsDraft((current) => ({ ...current, power: value }))
  }

  function updateSubnetForm(field: keyof SubnetFormState, value: string) {
    setSubnetForm((current) => ({ ...current, [field]: value }))
  }

  function toggleSubnetForm() {
    setSubnetFormOpen((current) => !current)
  }

  function toggleAccountExpanded() {
    setAccountExpanded((current) => !current)
  }

  function closeAccount() {
    setAccountExpanded(false)
  }

  function updateConfirmDialogInput(value: string) {
    setConfirmDialog((current) => ({ ...current, input: value }))
  }

  function resetUiAfterLogout() {
    setSelectedMachine('')
    setSelectedSubnet('')
    setView('overview')
    setSelectedVirtualMachineRouteState('')
    setAccountExpanded(false)
    setMachineFilter('')
    setActivityTypeFilter('all')
    setActivityMachineFilter('')
    setSubnetFormOpen(false)
    setSubnetForm(initialSubnetForm)
    setMachineSettingsDraft(initialMachineSettingsDraft)
    setMachineSettingsSaving(false)
    setInlineEditField(null)
    setMachineTab('overview')
    setConfirmDialog(initialConfirmDialog)
  }

  return {
    selectedMachine,
    setSelectedMachine,
    selectedSubnet,
    setSelectedSubnet,
    authForm,
    setAuthForm,
    view,
    setView,
    selectedVirtualMachineRoute,
    setSelectedVirtualMachineRoute,
    theme,
    setTheme,
    appearance,
    setAppearance,
    toggleAppearance,
    systemDrawerOpen,
    toggleSystemDrawer,
    paletteOpen,
    setPaletteOpen,
    groupBy,
    setGroupBy: setGroupByRaw,
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
    accountExpanded,
    setAccountExpanded,
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
  }
}
