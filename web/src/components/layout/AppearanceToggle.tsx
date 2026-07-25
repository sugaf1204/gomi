import clsx from 'clsx'
import type { Appearance } from '../../app-types'

export type AppearanceToggleProps = {
  appearance: Appearance
  onAppearanceChange: (appearance: Appearance) => void
}

const options: Appearance[] = ['light', 'dark']

// Segmented control: two 10px mono labels, the active one filled.
export function AppearanceToggle({ appearance, onAppearanceChange }: AppearanceToggleProps) {
  return (
    <div className="flex border border-line" role="group" aria-label="Appearance">
      {options.map((option) => (
        <button
          key={option}
          type="button"
          aria-pressed={appearance === option}
          onClick={() => onAppearanceChange(option)}
          className={clsx(
            'border-0 shadow-none px-[7px] py-[3px] font-mono text-[10px] uppercase tracking-[0.1em]',
            'hover:transform-none! hover:shadow-none!',
            appearance === option ? 'bg-ink text-bg' : 'bg-transparent text-ink-soft'
          )}
        >
          {option}
        </button>
      ))}
    </div>
  )
}
