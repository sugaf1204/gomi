import { useEffect, useRef, useState } from 'react'
import clsx from 'clsx'
import { filterPalette, type PaletteEntry } from '../../lib/palette'
import { ModalOverlay } from '../ui/ModalOverlay'

export type CommandPaletteProps = {
  open: boolean
  onClose: () => void
  entries: PaletteEntry[]
  onSelect: (entry: PaletteEntry) => void
}

const MAX_RESULTS = 12

export function CommandPalette({ open, onClose, entries, onSelect }: CommandPaletteProps) {
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (open) {
      setQuery('')
      setActiveIndex(0)
      // Autofocus once the input has mounted.
      const id = requestAnimationFrame(() => inputRef.current?.focus())
      return () => cancelAnimationFrame(id)
    }
    return undefined
  }, [open])

  const results = filterPalette(entries, query).slice(0, MAX_RESULTS)

  useEffect(() => {
    setActiveIndex((current) => {
      if (results.length === 0) return 0
      return Math.min(current, results.length - 1)
    })
  }, [results.length])

  if (!open) return null

  function selectEntry(entry: PaletteEntry) {
    onSelect(entry)
    onClose()
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      setActiveIndex((current) => Math.min(current + 1, Math.max(results.length - 1, 0)))
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      setActiveIndex((current) => Math.max(current - 1, 0))
    } else if (event.key === 'Enter') {
      event.preventDefault()
      const entry = results[activeIndex]
      if (entry) selectEntry(entry)
    } else if (event.key === 'Escape') {
      event.preventDefault()
      onClose()
    }
  }

  return (
    <ModalOverlay onBackdropClick={onClose}>
      <div className="w-[min(480px,100%)] bg-panel border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] flex flex-col">
        <input
          ref={inputRef}
          aria-label="jump to…"
          placeholder="jump to…"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={handleKeyDown}
          className="w-full border-0 border-b border-line bg-transparent px-[0.85rem] py-[0.7rem] font-mono text-[0.9rem] text-ink outline-none"
        />
        <ul role="listbox" aria-label="jump to results" className="m-0 p-0 list-none max-h-[360px] overflow-y-auto">
          {results.length === 0 && (
            <li className="px-[0.85rem] py-[0.6rem] text-[0.82rem] text-ink-soft">No matches</li>
          )}
          {results.map((entry, index) => {
            const active = index === activeIndex
            return (
              <li
                key={entry.id}
                role="option"
                aria-selected={active}
                onMouseEnter={() => setActiveIndex(index)}
                onClick={() => selectEntry(entry)}
                className={clsx(
                  'flex items-center gap-2 px-[0.85rem] py-[0.5rem] text-[0.86rem] cursor-pointer border-l-[3px]',
                  active ? 'border-l-brand bg-panel-3 text-ink' : 'border-l-transparent text-ink-mid'
                )}
              >
                <span className="truncate font-mono">{entry.label}</span>
                <span className="ml-auto font-mono text-[10.5px] text-ink-soft shrink-0">{entry.kind}</span>
              </li>
            )
          })}
        </ul>
      </div>
    </ModalOverlay>
  )
}
