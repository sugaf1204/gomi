import clsx from 'clsx'
import type { FormEvent } from 'react'
import { useEffect, useState } from 'react'
import type { Theme } from '../../app-types'
import { api } from '../../api'
import type { Me, ServiceAccount } from '../../types'

export type SettingsViewProps = {
  theme: Theme
  onThemeChange: (theme: Theme) => void
  user: Me | null
}

export function SettingsView({ theme, onThemeChange, user }: SettingsViewProps) {
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [savingPassword, setSavingPassword] = useState(false)
  const [passwordMessage, setPasswordMessage] = useState('')
  const [passwordError, setPasswordError] = useState('')
  const [serviceAccounts, setServiceAccounts] = useState<ServiceAccount[]>([])
  const [serviceAccountName, setServiceAccountName] = useState('')
  const [serviceAccountRole, setServiceAccountRole] = useState<ServiceAccount['role']>('viewer')
  const [serviceAccountToken, setServiceAccountToken] = useState('')
  const [serviceAccountMessage, setServiceAccountMessage] = useState('')
  const [serviceAccountError, setServiceAccountError] = useState('')
  const [loadingServiceAccounts, setLoadingServiceAccounts] = useState(false)
  const [savingServiceAccount, setSavingServiceAccount] = useState(false)

  const canManageServiceAccounts = user?.role === 'admin'

  useEffect(() => {
    if (!canManageServiceAccounts) {
      setServiceAccounts([])
      return
    }
    let cancelled = false
    setLoadingServiceAccounts(true)
    api.listServiceAccounts()
      .then((response) => {
        if (!cancelled) setServiceAccounts(response.serviceAccounts)
      })
      .catch((err) => {
        if (!cancelled) setServiceAccountError(err instanceof Error ? err.message : 'Failed to load service accounts')
      })
      .finally(() => {
        if (!cancelled) setLoadingServiceAccounts(false)
      })
    return () => {
      cancelled = true
    }
  }, [canManageServiceAccounts])

  const themeButton = (value: Theme) =>
    clsx(
      'w-full text-left px-[0.8rem] py-[0.7rem] border border-line bg-panel/75 transition-colors',
      theme === value && 'border-brand text-brand-strong bg-brand-wash'
    )

  async function handlePasswordSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setPasswordMessage('')
    setPasswordError('')

    if (newPassword !== confirmPassword) {
      setPasswordError('New passwords do not match')
      return
    }
    if (newPassword.length < 8) {
      setPasswordError('New password must be at least 8 characters')
      return
    }

    setSavingPassword(true)
    try {
      await api.changeMyPassword(currentPassword, newPassword)
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
      setPasswordMessage('Password updated')
    } catch (err) {
      setPasswordError(err instanceof Error ? err.message : 'Failed to update password')
    } finally {
      setSavingPassword(false)
    }
  }

  async function handleServiceAccountSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setServiceAccountMessage('')
    setServiceAccountError('')
    setServiceAccountToken('')
    const name = serviceAccountName.trim()
    if (!name) {
      setServiceAccountError('Name is required')
      return
    }

    setSavingServiceAccount(true)
    try {
      const response = await api.createServiceAccount(name, serviceAccountRole)
      setServiceAccounts((items) => {
        const next = items.filter((item) => item.name !== response.serviceAccount.name)
        return [...next, response.serviceAccount].sort((a, b) => a.name.localeCompare(b.name))
      })
      setServiceAccountName('')
      setServiceAccountToken(response.token)
      setServiceAccountMessage('Service account token issued')
    } catch (err) {
      setServiceAccountError(err instanceof Error ? err.message : 'Failed to issue service account token')
    } finally {
      setSavingServiceAccount(false)
    }
  }

  async function handleDeleteServiceAccount(name: string) {
    setServiceAccountMessage('')
    setServiceAccountError('')
    setServiceAccountToken('')
    try {
      await api.deleteServiceAccount(name)
      setServiceAccounts((items) => items.filter((item) => item.name !== name))
      setServiceAccountMessage('Service account deleted')
    } catch (err) {
      setServiceAccountError(err instanceof Error ? err.message : 'Failed to delete service account')
    }
  }

  return (
    <section className="min-h-0 grid content-start gap-[1rem]">
      <section className="ui-panel border border-line bg-panel/72 p-[1rem] grid gap-[0.8rem]">
        <header>
          <p className="m-0 text-[0.75rem] uppercase tracking-[0.08em] text-ink-soft">Appearance</p>
          <h2 className="text-[1.15rem] mt-[0.25rem]">Theme</h2>
        </header>

        <div className="grid gap-[0.45rem] sm:grid-cols-2">
          <button
            type="button"
            className={themeButton('default')}
            onClick={() => onThemeChange('default')}
            aria-pressed={theme === 'default'}
          >
            <strong className="block text-[0.93rem]">Default</strong>
            <span className="block text-[0.8rem] text-ink-soft mt-[0.18rem]">Sharp corners and dense controls.</span>
          </button>

          <button
            type="button"
            className={themeButton('rounded')}
            onClick={() => onThemeChange('rounded')}
            aria-pressed={theme === 'rounded'}
          >
            <strong className="block text-[0.93rem]">Rounded</strong>
            <span className="block text-[0.8rem] text-ink-soft mt-[0.18rem]">Softer corners for controls and panels.</span>
          </button>
        </div>
      </section>

      <section className="ui-panel border border-line bg-panel/72 p-[1rem] grid gap-[0.8rem]">
        <header>
          <p className="m-0 text-[0.75rem] uppercase tracking-[0.08em] text-ink-soft">Account</p>
          <h2 className="text-[1.15rem] mt-[0.25rem]">Current User</h2>
        </header>
        <div className="grid gap-[0.25rem]">
          <p className="m-0 text-[0.9rem]">
            <span className="text-ink-soft">Username </span>
            <strong>{user?.username ?? '-'}</strong>
          </p>
          <p className="m-0 text-[0.9rem]">
            <span className="text-ink-soft">Role </span>
            <strong>{user?.role ?? '-'}</strong>
          </p>
        </div>

        <form className="grid gap-[0.65rem] max-w-[420px]" onSubmit={handlePasswordSubmit}>
          <label className="grid gap-[0.25rem] text-[0.84rem] text-ink-soft">
            Current password
            <input
              type="password"
              autoComplete="current-password"
              value={currentPassword}
              onChange={(event) => setCurrentPassword(event.target.value)}
              required
            />
          </label>
          <label className="grid gap-[0.25rem] text-[0.84rem] text-ink-soft">
            New password
            <input
              type="password"
              autoComplete="new-password"
              value={newPassword}
              onChange={(event) => setNewPassword(event.target.value)}
              minLength={8}
              required
            />
          </label>
          <label className="grid gap-[0.25rem] text-[0.84rem] text-ink-soft">
            Confirm new password
            <input
              type="password"
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(event) => setConfirmPassword(event.target.value)}
              minLength={8}
              required
            />
          </label>
          {passwordError && <p className="m-0 text-[0.84rem] text-danger">{passwordError}</p>}
          {passwordMessage && <p className="m-0 text-[0.84rem] text-brand-strong">{passwordMessage}</p>}
          <div>
            <button type="submit" disabled={savingPassword || !user} className="bg-brand border-brand-strong text-white">
              {savingPassword ? 'Updating...' : 'Change password'}
            </button>
          </div>
        </form>
      </section>

      <section className="ui-panel border border-line bg-panel/72 p-[1rem] grid gap-[0.8rem]">
        <header>
          <p className="m-0 text-[0.75rem] uppercase tracking-[0.08em] text-ink-soft">Automation</p>
          <h2 className="text-[1.15rem] mt-[0.25rem]">Service Accounts</h2>
        </header>

        <form className="grid gap-[0.65rem] max-w-[560px]" onSubmit={handleServiceAccountSubmit}>
          <div className="grid gap-[0.65rem] sm:grid-cols-[1fr_150px_auto] sm:items-end">
            <label className="grid gap-[0.25rem] text-[0.84rem] text-ink-soft">
              Name
              <input
                type="text"
                value={serviceAccountName}
                onChange={(event) => setServiceAccountName(event.target.value)}
                disabled={!canManageServiceAccounts || savingServiceAccount}
                required
              />
            </label>
            <label className="grid gap-[0.25rem] text-[0.84rem] text-ink-soft">
              Role
              <select
                value={serviceAccountRole}
                onChange={(event) => setServiceAccountRole(event.target.value as ServiceAccount['role'])}
                disabled={!canManageServiceAccounts || savingServiceAccount}
              >
                <option value="viewer">viewer</option>
                <option value="operator">operator</option>
                <option value="admin">admin</option>
              </select>
            </label>
            <button type="submit" disabled={!canManageServiceAccounts || savingServiceAccount} className="bg-brand border-brand-strong text-white">
              {savingServiceAccount ? 'Issuing...' : 'Issue token'}
            </button>
          </div>
        </form>

        {serviceAccountError && <p className="m-0 text-[0.84rem] text-danger">{serviceAccountError}</p>}
        {serviceAccountMessage && <p className="m-0 text-[0.84rem] text-brand-strong">{serviceAccountMessage}</p>}
        {serviceAccountToken && (
          <pre className="m-0 max-w-[760px] overflow-auto border border-line bg-[#101827] text-[#d7deea] p-[0.75rem] text-[0.78rem]">{serviceAccountToken}</pre>
        )}

        <div className="grid gap-[0.45rem]">
          {loadingServiceAccounts && <p className="m-0 text-[0.84rem] text-ink-soft">Loading...</p>}
          {!loadingServiceAccounts && canManageServiceAccounts && serviceAccounts.length === 0 && (
            <p className="m-0 text-[0.84rem] text-ink-soft">No service accounts</p>
          )}
          {!canManageServiceAccounts && (
            <p className="m-0 text-[0.84rem] text-ink-soft">Admin role required</p>
          )}
          {serviceAccounts.map((account) => (
            <div key={account.name} className="grid gap-[0.5rem] border border-line bg-panel/70 px-[0.75rem] py-[0.65rem] sm:grid-cols-[1fr_auto] sm:items-center">
              <div className="min-w-0">
                <strong className="block text-[0.92rem]">{account.name}</strong>
                <span className="block text-[0.8rem] text-ink-soft">
                  {account.role} - last used {account.lastUsedAt ? new Date(account.lastUsedAt).toLocaleString() : 'never'}
                </span>
              </div>
              <button type="button" onClick={() => void handleDeleteServiceAccount(account.name)} disabled={!canManageServiceAccounts}>
                Delete
              </button>
            </div>
          ))}
        </div>
      </section>
    </section>
  )
}
