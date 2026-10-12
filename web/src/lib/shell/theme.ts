import { initTheme, setThemeMode, setThemeName } from '@kenn-io/kit-ui'

const MODE_KEY = 'kit-ui-theme'
const STORED_MODES = ['light', 'dark', 'system']

// Browsers can refuse storage (private windows, blocked site data), some by throwing from
// getItem and some from the localStorage getter itself.
function hasStoredMode(): boolean {
  try {
    return STORED_MODES.includes(localStorage.getItem(MODE_KEY) ?? '')
  } catch {
    return false
  }
}

/** Whether reading the localStorage global, which kit-ui's theme store does unguarded, throws. */
function storageGetterThrows(): boolean {
  try {
    return localStorage === null
  } catch {
    return true
  }
}

/**
 * Applies the Moneyflow theme: dark on a first visit, the stored choice afterwards. Where kit-ui's
 * theme store cannot run, it starts dark without it.
 */
export function initMoneyflowTheme(): void {
  if (storageGetterThrows()) {
    document.documentElement.classList.add('dark')
    document.documentElement.setAttribute('data-kit-theme', 'moneyflow')
    return
  }
  initTheme()
  setThemeName('moneyflow')
  if (!hasStoredMode()) setThemeMode('dark')
}
