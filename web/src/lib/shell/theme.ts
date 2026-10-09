import { initTheme, setThemeMode, setThemeName } from '@kenn-io/kit-ui'

const MODE_KEY = 'kit-ui-theme'
const STORED_MODES = ['light', 'dark', 'system']

function hasStoredMode(storage: Storage): boolean {
  try {
    return STORED_MODES.includes(storage.getItem(MODE_KEY) ?? '')
  } catch {
    return false
  }
}

/** Applies the Moneyflow theme: dark on a first visit, the stored choice afterwards. */
export function initMoneyflowTheme(storage: Storage = localStorage): void {
  initTheme()
  setThemeName('moneyflow')
  if (!hasStoredMode(storage)) setThemeMode('dark')
}
