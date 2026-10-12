import { untrack } from 'svelte'

const storageKey = 'moneyflow-charts'

export interface ChartHost {
  /** Phone width, where charts open in a dialog. */
  compact(): boolean
  /** Pushes a shortcut scope and returns the function that pops it. */
  pushScope(name: string): (() => void) | undefined
}

/**
 * Tracks the Charts switch. Above phone width the switch shows or hides the chart row above the
 * table; it starts on and the choice persists in this browser. At phone width the switch opens a
 * dialog with its own shortcut scope.
 */
export class ChartState {
  dialogOpen = $state(false)
  #row = $state(readRow())
  #host: ChartHost
  #popScope: (() => void) | undefined

  constructor(host: ChartHost) {
    this.#host = host
  }

  /** Whether the chart row shows above the table. The KPI strip shows either way. */
  get row(): boolean {
    return !this.#host.compact() && this.#row
  }

  /** Whether the Charts switch is on. */
  get checked(): boolean {
    return this.#host.compact() ? this.dialogOpen : this.row
  }

  toggle(checked: boolean): void {
    if (this.#host.compact()) return this.setDialog(checked)
    this.#row = checked
    writeRow(checked)
  }

  setDialog(open: boolean): void {
    this.#popScope?.()
    this.#popScope = open ? this.#host.pushScope('charts') : undefined
    this.dialogOpen = open
  }

  /** Pushes the chart dialog's scope onto a rebuilt shortcut manager. */
  rescope(): void {
    this.#popScope = untrack(() => this.dialogOpen) ? this.#host.pushScope('charts') : undefined
  }

  destroy(): void {
    this.#popScope?.()
  }
}

// Browsers can refuse storage (private windows, blocked site data); the switch then starts on
// and forgets the choice on reload, which is all it can do.
function readRow(): boolean {
  try {
    return localStorage.getItem(storageKey) !== 'off'
  } catch {
    return true
  }
}

function writeRow(shown: boolean): void {
  try {
    localStorage.setItem(storageKey, shown ? 'on' : 'off')
  } catch {
    // See readRow: the choice lasts until the page reloads.
  }
}
