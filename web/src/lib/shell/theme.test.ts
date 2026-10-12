import { getThemeMode, getThemeName } from '@kenn-io/kit-ui'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { initMoneyflowTheme } from './theme'

const modeKey = 'kit-ui-theme'

function stubStorage(initial: Record<string, string> = {}): Storage {
  const values = new Map(Object.entries(initial))
  return {
    get length() {
      return values.size
    },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => [...values.keys()][index] ?? null,
    removeItem: (key) => void values.delete(key),
    setItem: (key, value) => void values.set(key, value),
  }
}

function initWith(storage: Storage): void {
  vi.stubGlobal('localStorage', storage)
  initMoneyflowTheme(storage)
}

describe('initMoneyflowTheme', () => {
  beforeEach(() => {
    document.documentElement.className = ''
    document.documentElement.removeAttribute('data-kit-theme')
  })
  afterEach(() => vi.unstubAllGlobals())

  it('starts dark and names the moneyflow theme on a first visit', () => {
    const storage = stubStorage()
    initWith(storage)

    expect(getThemeMode()).toBe('dark')
    expect(getThemeName()).toBe('moneyflow')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(document.documentElement.getAttribute('data-kit-theme')).toBe('moneyflow')
  })

  it.each(['light', 'dark', 'system'] as const)('keeps a stored %s choice', (choice) => {
    const storage = stubStorage({ [modeKey]: choice })
    initWith(storage)

    expect(getThemeMode()).toBe(choice)
    expect(storage.getItem(modeKey)).toBe(choice)
    expect(document.documentElement.getAttribute('data-kit-theme')).toBe('moneyflow')
  })

  it('treats an unrecognized stored value as a first visit', () => {
    initWith(stubStorage({ [modeKey]: 'sepia' }))

    expect(getThemeMode()).toBe('dark')
  })

  it('starts dark when storage cannot be read', () => {
    const blocked = stubStorage()
    blocked.getItem = () => {
      throw new DOMException('blocked', 'SecurityError')
    }
    initWith(blocked)

    expect(getThemeMode()).toBe('dark')
  })

  it('replaces a previously stored theme name', () => {
    initWith(stubStorage({ [`${modeKey}-name`]: 'graphite', [modeKey]: 'light' }))

    expect(getThemeName()).toBe('moneyflow')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })
})
