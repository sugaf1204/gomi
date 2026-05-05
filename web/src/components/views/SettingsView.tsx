import clsx from 'clsx'
import type { Theme } from '../../app-types'
import type { Me } from '../../types'

export type SettingsViewProps = {
  theme: Theme
  onThemeChange: (theme: Theme) => void
  user: Me | null
}

export function SettingsView({ theme, onThemeChange, user }: SettingsViewProps) {
  const themeButton = (value: Theme) =>
    clsx(
      'w-full text-left px-[0.8rem] py-[0.7rem] border border-line bg-white/75 transition-colors',
      theme === value && 'border-brand text-brand-strong bg-[rgba(43,122,120,0.12)]'
    )

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

      <section className="ui-panel border border-line bg-white/72 p-[1rem] grid gap-[0.5rem]">
        <header>
          <p className="m-0 text-[0.75rem] uppercase tracking-[0.08em] text-ink-soft">Account</p>
          <h2 className="text-[1.15rem] mt-[0.25rem]">Current User</h2>
        </header>
        <p className="m-0 text-[0.9rem]">
          <span className="text-ink-soft">Username </span>
          <strong>{user?.username ?? '-'}</strong>
        </p>
        <p className="m-0 text-[0.9rem]">
          <span className="text-ink-soft">Role </span>
          <strong>{user?.role ?? '-'}</strong>
        </p>
      </section>
    </section>
  )
}
