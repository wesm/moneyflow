import { getThemeMode, getThemeName } from '@kenn-io/kit-ui'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { initMoneyflowTheme } from './theme'
import { memoryStorage } from '../../test/storage'

const modeKey = 'kit-ui-theme'

function initWith(storage: Storage): void {
  vi.stubGlobal('localStorage', storage)
  initMoneyflowTheme()
}

describe('initMoneyflowTheme', () => {
  beforeEach(() => {
    document.documentElement.className = ''
    document.documentElement.removeAttribute('data-kit-theme')
  })
  afterEach(() => vi.unstubAllGlobals())

  it('starts dark and names the moneyflow theme on a first visit', () => {
    const storage = memoryStorage()
    initWith(storage)

    expect(getThemeMode()).toBe('dark')
    expect(getThemeName()).toBe('moneyflow')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(document.documentElement.getAttribute('data-kit-theme')).toBe('moneyflow')
  })

  it.each(['light', 'dark', 'system'] as const)('keeps a stored %s choice', (choice) => {
    const storage = memoryStorage({ [modeKey]: choice })
    initWith(storage)

    expect(getThemeMode()).toBe(choice)
    expect(storage.getItem(modeKey)).toBe(choice)
    expect(document.documentElement.getAttribute('data-kit-theme')).toBe('moneyflow')
  })

  it('treats an unrecognized stored value as a first visit', () => {
    initWith(memoryStorage({ [modeKey]: 'sepia' }))

    expect(getThemeMode()).toBe('dark')
  })

  it('starts dark when storage cannot be read', () => {
    const blocked = memoryStorage()
    blocked.getItem = () => {
      throw new DOMException('blocked', 'SecurityError')
    }
    initWith(blocked)

    expect(getThemeMode()).toBe('dark')
  })

  it('starts dark when the browser refuses access to storage', () => {
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      get: () => {
        throw new DOMException('blocked', 'SecurityError')
      },
    })
    initMoneyflowTheme()

    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(document.documentElement.getAttribute('data-kit-theme')).toBe('moneyflow')
  })

  it('replaces a previously stored theme name', () => {
    initWith(memoryStorage({ [`${modeKey}-name`]: 'graphite', [modeKey]: 'light' }))

    expect(getThemeName()).toBe('moneyflow')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })
})
