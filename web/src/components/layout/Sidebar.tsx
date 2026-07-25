import { useEffect, useRef, useState, useCallback } from 'react'
import { createPortal } from 'react-dom'
import clsx from 'clsx'
import type { View } from '../../app-types'
import { NAV_GROUPS, PRIMARY_ITEM, SYSTEM_ITEMS, type NavItem } from '../../lib/navigation'

export type NavCounts = Partial<Record<NonNullable<NavItem['count']>, number>>

export type SidebarProps = {
  view: View
  onViewChange: (view: View) => void
  counts: NavCounts
  environmentLabel?: string
  username?: string
  role?: string
  accountExpanded: boolean
  onToggleAccount: () => void
  onCloseAccount: () => void
  onLogout: () => void | Promise<void>
  systemOpen: boolean
  onToggleSystem: () => void
  onOpenPalette: () => void
}

export function Sidebar({
  view,
  onViewChange,
  counts,
  environmentLabel,
  username,
  role,
  accountExpanded,
  onToggleAccount,
  onCloseAccount,
  onLogout,
  systemOpen,
  onToggleSystem,
  onOpenPalette,
}: SidebarProps) {
  const accountLabel = username || 'account'
  const accountInitial = accountLabel.slice(0, 1).toUpperCase()
  const triggerRef = useRef<HTMLButtonElement>(null)
  const popoverRef = useRef<HTMLDivElement>(null)
  const [popoverPos, setPopoverPos] = useState<{ top: number; left: number } | null>(null)

  const updatePos = useCallback(() => {
    if (!triggerRef.current) return
    const rect = triggerRef.current.getBoundingClientRect()
    setPopoverPos({ top: rect.top - 6, left: rect.left })
  }, [])

  useEffect(() => {
    if (!accountExpanded) {
      setPopoverPos(null)
      return
    }
    updatePos()
    function handleClickOutside(e: MouseEvent) {
      const target = e.target as Node
      if (
        triggerRef.current && !triggerRef.current.contains(target) &&
        popoverRef.current && !popoverRef.current.contains(target)
      ) {
        onCloseAccount()
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [accountExpanded, onCloseAccount, updatePos])

  function renderItem(item: NavItem) {
    const active = view === item.view
    const count = item.count ? counts[item.count] : undefined
    return (
      <button
        key={item.view}
        onClick={() => onViewChange(item.view)}
        aria-current={active ? 'page' : undefined}
        className={clsx(
          'w-full flex items-center gap-2 text-left border-0 border-l-[3px] shadow-none rounded-none',
          'py-[7px] px-[9px] text-[13px] font-ui',
          'hover:transform-none! hover:shadow-none!',
          active
            ? 'border-l-brand bg-panel font-medium text-ink'
            : 'border-l-transparent bg-transparent text-ink-mid'
        )}
      >
        <span className="truncate">{item.label}</span>
        {count !== undefined && (
          <span className={clsx('ml-auto font-mono font-medium text-[10.5px]', active ? 'text-brand-accent' : 'text-ink-soft')}>
            {count}
          </span>
        )}
      </button>
    )
  }

  return (
    <aside className="sidebar-shell h-screen overflow-y-auto flex flex-col gap-[18px] pt-4 px-3 pb-3 border-r border-line bg-panel-2 max-md:h-auto max-md:border-r-0 max-md:border-b">
      <div className="flex items-center gap-[9px] px-1">
        <img src="/favicon.svg" alt="" width="24" height="24" className="block" />
        <span className="font-mono font-semibold text-[15px] tracking-[-0.01em]">gomi</span>
        {environmentLabel && (
          <span className="ml-auto font-mono text-[10.5px] text-ink-soft border border-line bg-panel py-[3px] px-[5px]">
            {environmentLabel}
          </span>
        )}
      </div>

      <button
        onClick={onOpenPalette}
        className="w-full flex items-center gap-2 text-left border border-line bg-panel shadow-none rounded-none py-[7px] px-[9px] hover:transform-none! hover:shadow-none!"
      >
        <span aria-hidden="true" className="font-mono text-[11px] text-ink-soft">/</span>
        <span className="font-mono text-[11.5px] text-ink-soft">jump to…</span>
        <span aria-hidden="true" className="ml-auto font-mono font-medium text-[10.5px] text-ink-soft bg-panel-3 border border-line py-[3px] px-1">
          ⌘K
        </span>
      </button>

      <nav className="flex flex-col gap-5 min-h-0">
        <div className="flex flex-col gap-[2px]">{renderItem(PRIMARY_ITEM)}</div>
        {NAV_GROUPS.map((group) => (
          <div key={group.heading} className="flex flex-col gap-[2px]">
            <p className="m-0 mb-[6px] px-[5px] font-mono font-semibold text-[10.5px] tracking-[0.14em] text-ink-soft">
              {group.heading}
            </p>
            {group.items.map(renderItem)}
          </div>
        ))}
      </nav>

      <section className="mt-auto max-md:mt-0 flex flex-col gap-[2px] pt-2 border-t border-line">
        <button
          onClick={onToggleSystem}
          aria-expanded={systemOpen}
          className="w-full flex items-center gap-2 text-left border-0 shadow-none rounded-none bg-transparent py-[7px] px-[9px] text-[13px] text-ink-mid hover:transform-none! hover:shadow-none!"
        >
          <span aria-hidden="true" className="font-mono text-[10px] text-ink-soft">{systemOpen ? '▾' : '▸'}</span>
          <span>System</span>
          {!systemOpen && (
            <span className="ml-auto font-mono text-[10.5px] text-ink-soft truncate">activity · users · settings</span>
          )}
        </button>
        {systemOpen && SYSTEM_ITEMS.map(renderItem)}

        <button
          ref={triggerRef}
          className="sidebar-account-trigger w-full text-left py-[7px] px-[5px] mt-1 bg-transparent border-0 shadow-none hover:shadow-none! hover:transform-none!"
          onClick={onToggleAccount}
          aria-expanded={accountExpanded}
        >
          <span className="sidebar-account-avatar">{accountInitial}</span>
          <span className="sidebar-account-name truncate">{accountLabel}</span>
          {role && <span className="ml-auto font-mono text-[10.5px] text-ink-soft">{role}</span>}
        </button>
      </section>

      {accountExpanded && popoverPos && createPortal(
        <div
          ref={popoverRef}
          className="sidebar-account-popover"
          style={{ top: popoverPos.top, left: popoverPos.left }}
        >
          <button
            className="sidebar-account-action w-full bg-transparent border-0 shadow-none hover:shadow-none! hover:transform-none!"
            onClick={() => void onLogout()}
          >
            Sign Out
          </button>
        </div>,
        document.body
      )}
    </aside>
  )
}
