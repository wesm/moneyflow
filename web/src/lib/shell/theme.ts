import { initTheme, setThemeMode, setThemeName } from '@kenn-io/kit-ui'

const MODE_KEY = 'kit-ui-theme'
const STORED_MODES = ['light', 'dark', 'system']

// Browsers can refuse storage (private windows, blocked site data); see hideRefusedStorage.
function hasStoredMode(): boolean {
  try {
    return STORED_MODES.includes(localStorage.getItem(MODE_KEY) ?? '')
  } catch {
    return false
  }
}

/**
 * Some browsers that block site data throw from the localStorage getter itself. kit-ui's theme
 * store reads that global outside its guard, so the app would stop before it mounts. Replacing
 * the global with undefined lets kit-ui, and every other storage reader, treat storage as absent.
 */
function hideRefusedStorage(): void {
  try {
    void localStorage
  } catch {
    Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: undefined })
  }
}

/** Applies the Moneyflow theme: dark on a first visit, the stored choice afterwards. */
export function initMoneyflowTheme(): void {
  hideRefusedStorage()
  initTheme()
  setThemeName('moneyflow')
  if (!hasStoredMode()) setThemeMode('dark')
}
