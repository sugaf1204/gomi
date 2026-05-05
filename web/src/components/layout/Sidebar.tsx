import { useEffect, useRef, useState, useCallback } from 'react'
import { createPortal } from 'react-dom'
import clsx from 'clsx'
import type { View } from '../../app-types'

export type SidebarProps = {
  view: View
  onViewChange: (view: View) => void
  username?: string
  accountExpanded: boolean
  onToggleAccount: () => void
  onCloseAccount: () => void
  onLogout: () => void | Promise<void>
}

export function Sidebar({
  view,
  onViewChange,
  username,
  accountExpanded,
  onToggleAccount,
  onCloseAccount,
  onLogout
}: SidebarProps) {
  const accountLabel = username || 'account'
  const accountInitial = accountLabel.slice(0, 1).toUpperCase()
  const triggerRef = useRef<HTMLButtonElement>(null)
  const popoverRef = useRef<HTMLDivElement>(null)
  const [popoverPos, setPopoverPos] = useState<{ top: number; left: number } | null>(null)

  const updatePos = useCallback(() => {
    if (!triggerRef.current) return
    const rect = triggerRef.current.getBoundingClientRect()
    setPopoverPos({
      top: rect.top - 6,
      left: rect.left
    })
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

  const navBtn = (v: View) =>
    clsx(
      'text-left font-ui font-medium tracking-normal bg-transparent border-0 border-l-[3px] border-l-transparent shadow-none',
      view === v && '!border-l-ink-soft !bg-[rgba(0,0,0,0.05)]'
    )

  const separator = 'border-0 border-t border-line my-[0.15rem]'

  return (
    <aside className="sidebar-shell h-screen overflow-y-auto flex flex-col gap-[0.95rem] p-[1rem_0.85rem] border-r border-line bg-white/55 backdrop-blur-[6px] md:flex-col max-md:h-auto max-md:grid max-md:grid-rows-[auto_auto_auto] max-md:border-r-0 max-md:border-b max-md:border-line">
      <div className="flex items-center gap-[0.62rem] pb-[0.2rem]">
        <img src="/favicon.svg" alt="GoMI" width="28" height="28" className="rounded" />
        <p className="m-0 font-display text-[1.08rem] font-medium tracking-[0.012em]">GoMI</p>
      </div>

      <nav className="grid gap-[0.35rem]">
        <button className={navBtn('overview')} onClick={() => onViewChange('overview')}>Overview</button>
        <button className={navBtn('machines')} onClick={() => onViewChange('machines')}>Machines</button>
        <button className={navBtn('hypervisors')} onClick={() => onViewChange('hypervisors')}>Hypervisors</button>
        <button className={navBtn('virtual-machines')} onClick={() => onViewChange('virtual-machines')}>Virtual Machines</button>

        <hr className={separator} />

        <button className={navBtn('cloud-init')} onClick={() => onViewChange('cloud-init')}>Cloud-Init</button>
        <button className={navBtn('os-images')} onClick={() => onViewChange('os-images')}>OS Images</button>

        <hr className={separator} />

        <button className={navBtn('network')} onClick={() => onViewChange('network')}>Network</button>
        <button className={navBtn('dhcp-leases')} onClick={() => onViewChange('dhcp-leases')}>DHCP Leases</button>
        <button className={navBtn('users')} onClick={() => onViewChange('users')}>Users</button>
        <button className={navBtn('activity')} onClick={() => onViewChange('activity')}>Activity</button>
      </nav>

      <section className="mt-auto max-md:mt-0 grid gap-[0.35rem]">
        <hr className={separator} />
        <button className={navBtn('settings')} onClick={() => onViewChange('settings')}>Settings</button>
        <button
          ref={triggerRef}
          className="sidebar-account-trigger w-full text-left py-[0.52rem] px-[0.35rem] bg-transparent border-0 shadow-none hover:shadow-none! hover:transform-none!"
          onClick={onToggleAccount}
          aria-expanded={accountExpanded}
        >
          <span className="sidebar-account-avatar">{accountInitial}</span>
          <span className="sidebar-account-name">{accountLabel}</span>
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
