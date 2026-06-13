import clsx from 'clsx'
import type { FormEvent } from 'react'
import { useState } from 'react'
import type { Theme } from '../../app-types'
import { api } from '../../api'
import type { Me } from '../../types'

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

  const themeButton = (value: Theme) =>
    clsx(
      'w-full text-left px-[0.8rem] py-[0.7rem] border border-line bg-white/75 transition-colors',
      theme === value && 'border-brand text-brand-strong bg-[rgba(43,122,120,0.12)]'
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

  return (
    <section className="min-h-0 grid content-start gap-[1rem]">
      <section className="ui-panel border border-line bg-white/72 p-[1rem] grid gap-[0.8rem]">
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

      <section className="ui-panel border border-line bg-white/72 p-[1rem] grid gap-[0.8rem]">
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
          {passwordError && <p className="m-0 text-[0.84rem] text-[#9b2d2d]">{passwordError}</p>}
          {passwordMessage && <p className="m-0 text-[0.84rem] text-brand-strong">{passwordMessage}</p>}
          <div>
            <button type="submit" disabled={savingPassword || !user} className="bg-brand border-brand-strong text-white">
              {savingPassword ? 'Updating...' : 'Change password'}
            </button>
          </div>
        </form>
      </section>
    </section>
  )
}
