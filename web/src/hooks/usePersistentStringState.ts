import { useEffect, useState, type Dispatch, type SetStateAction } from 'react'

function readStoredValue(storageKey: string): string {
  if (typeof window === 'undefined') return ''
  try {
    return localStorage.getItem(storageKey) ?? ''
  } catch {
    return ''
  }
}

export function usePersistentStringState(storageKey: string): [string, Dispatch<SetStateAction<string>>] {
  const [value, setValue] = useState<string>(() => readStoredValue(storageKey))

  useEffect(() => {
    if (typeof window === 'undefined') return
    try {
      if (value) localStorage.setItem(storageKey, value)
      else localStorage.removeItem(storageKey)
    } catch {
      // ignore localStorage access errors
    }
  }, [storageKey, value])

  return [value, setValue]
}
