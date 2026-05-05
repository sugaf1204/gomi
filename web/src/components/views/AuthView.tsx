import type { FormEvent } from 'react'

type Props = {
  mode: 'login' | 'setup'
  checking?: boolean
  authForm: {
    username: string
    password: string
  }
  onAuthFormChange: (field: 'username' | 'password', value: string) => void
  onSubmit: (event: FormEvent) => void | Promise<void>
}

export function AuthView({ mode, checking = false, authForm, onAuthFormChange, onSubmit }: Props) {
  const isSetup = mode === 'setup'

  return (
    <main className="min-h-screen grid place-items-center p-[1.2rem]">
      <section className="ui-panel w-[min(440px,100%)] bg-white/86 backdrop-blur-[4px] border border-white/80 shadow-[0_24px_70px_rgba(78,66,52,0.15)] p-[1.35rem] grid gap-[0.95rem]">
        <p className="m-0 font-ui font-medium text-[0.72rem] uppercase tracking-[0.08em] text-ink-soft">gomi control plane</p>
        <h1 className="text-[clamp(1.9rem,4vw,2.4rem)]">{isSetup ? 'Create First Admin' : 'Sign In'}</h1>
        <p className="m-0 text-ink-soft">
          {isSetup
            ? 'No admin user exists yet. Run the server-side setup command before signing in.'
            : 'Provisioning and power workflow for bare-metal operations.'}
        </p>
        {isSetup ? (
          <pre className="m-0 overflow-auto rounded-[8px] bg-ink text-white p-[0.85rem] text-[0.78rem] leading-[1.45]">
            gomi setup admin --config=/etc/gomi/gomi.yaml --username admin --password-file /path/to/password
          </pre>
        ) : (
          <form className="grid gap-[0.8rem]" onSubmit={(event) => void onSubmit(event)}>
            <label>
              Username
              <input
                value={authForm.username}
                onChange={(e) => onAuthFormChange('username', e.target.value)}
                required
              />
            </label>
            <label>
              Password
              <input
                type="password"
                value={authForm.password}
                onChange={(e) => onAuthFormChange('password', e.target.value)}
                required
              />
            </label>
            <button className="bg-brand border-brand-strong text-white" type="submit" disabled={checking}>
              {checking ? 'Checking...' : 'Sign In'}
            </button>
          </form>
        )}
      </section>
    </main>
  )
}
