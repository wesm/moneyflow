import { tick, untrack } from 'svelte'

import type { TransactionInformationResponse } from '../api/client'

export type Overlay =
  | 'search'
  | 'filters'
  | 'help'
  | 'merchant'
  | 'category'
  | 'categories'
  | 'groups'
  | 'review'
  | 'write'
  | 'duplicates'
  | 'delete'
  | 'export'
  | 'information'

export interface OverlayHost {
  /** Pushes a shortcut scope and returns the function that pops it. */
  pushScope(name: string): (() => void) | undefined
  /** Focuses the grid, unless something else has opened since. */
  focusGrid(): void
  /** The grid's container. Focus inside it returns to the grid, not to an opener. */
  grid(): HTMLElement | undefined
}

/**
 * Tracks the open shell overlay, its shortcut scope, and its data. Closing returns focus to the
 * control that opened the overlay, or to the grid when the overlay opened from the grid or its
 * opener has left the page.
 */
export class OverlayState {
  current = $state<Overlay | undefined>()
  deleteTarget = $state<{ identity: string; kind: 'transaction' } | undefined>()
  deleteCount = $state(0)
  information = $state<TransactionInformationResponse | undefined>()
  #host: OverlayHost
  #opener: HTMLElement | undefined
  #popScope: (() => void) | undefined
  #restoreFrame: number | undefined

  constructor(host: OverlayHost) {
    this.#host = host
  }

  /** Opens `next` without tracking state, so an effect that opens it does not rerun on close. */
  open(next: Overlay): void {
    this.#cancelRestore()
    if (untrack(() => this.current) === undefined) this.#opener = this.#openerOutsideGrid()
    this.#popScope?.()
    this.#popScope = this.#host.pushScope(next)
    this.current = next
  }

  close(): void {
    this.current = undefined
    this.#popScope?.()
    this.#popScope = undefined
    this.deleteTarget = undefined
    this.deleteCount = 0
    this.information = undefined
    void tick().then(() => {
      this.#restoreFrame = requestAnimationFrame(() => {
        this.#restoreFrame = undefined
        if (this.current === undefined) this.#restoreFocus()
      })
    })
  }

  /** Pushes the open overlay's scope onto a rebuilt shortcut manager. */
  rescope(): void {
    const current = untrack(() => this.current)
    this.#popScope = current ? this.#host.pushScope(current) : undefined
  }

  destroy(): void {
    this.#popScope?.()
    this.#cancelRestore()
  }

  #cancelRestore(): void {
    if (this.#restoreFrame !== undefined) cancelAnimationFrame(this.#restoreFrame)
    this.#restoreFrame = undefined
  }

  #openerOutsideGrid(): HTMLElement | undefined {
    const active = document.activeElement
    if (!(active instanceof HTMLElement) || active === document.body) return undefined
    return this.#host.grid()?.contains(active) ? undefined : active
  }

  #restoreFocus(): void {
    const opener = this.#opener
    this.#opener = undefined
    if (opener?.isConnected) opener.focus({ preventScroll: true })
    else this.#host.focusGrid()
  }
}
