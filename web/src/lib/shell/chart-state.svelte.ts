import { untrack } from 'svelte'

import { RailLoader } from './rail-loader.svelte'

export interface ChartHost {
  /** The full sidebar shows (1280 px and wider). */
  fullSidebar(): boolean
  /** Phone width, where charts open in a dialog. */
  compact(): boolean
  /** Pushes a shortcut scope and returns the function that pops it. */
  pushScope(name: string): (() => void) | undefined
}

/**
 * Tracks the Charts switch. Beside the full sidebar, charts start on. At icon-rail widths they
 * start off so the table keeps its money columns; the switch opens the aside on demand. At phone
 * width the switch opens a dialog with its own shortcut scope.
 */
export class ChartState {
  readonly rail = new RailLoader()
  dialogOpen = $state(false)
  #choice = $state<boolean | undefined>()
  #host: ChartHost
  #popScope: (() => void) | undefined

  constructor(host: ChartHost) {
    this.#host = host
  }

  /** Whether the Visualizations aside shows beside the table. */
  get aside(): boolean {
    return !this.#host.compact() && (this.#choice ?? this.#host.fullSidebar())
  }

  /** Whether the Charts switch is on. */
  get checked(): boolean {
    return this.#host.compact() ? this.dialogOpen : this.aside
  }

  toggle(checked: boolean): void {
    if (this.#host.compact()) this.setDialog(checked)
    else this.#choice = checked
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
