import { tick, untrack } from 'svelte'
import type { Attachment } from 'svelte/attachments'

import type { TransactionInformationResponse } from '../api/client'
import type { EditorDraft } from '../editing/draft.svelte'
import { restoreFocus, rowIndex } from './focus'
import type { FocusedRow } from './local-actions'

export type Overlay =
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
  | 'period'
  | 'navigation'
  | 'view'
  | 'actions'

/**
 * Where the merchant or category editor, or the delete confirmation, hangs: below a row, or above
 * the selection bar. One value holds both, so the placement always matches the element.
 */
export interface EditorAnchor {
  element: HTMLElement
  above: boolean
}

export interface OverlayHost {
  /** Pushes a shortcut scope and returns the function that pops it. */
  pushScope(name: string): (() => void) | undefined
  /** Focuses the grid, unless something else has opened since. */
  focusGrid(): void
  /** The grid's container. Focus inside it returns to the grid, not to an opener. */
  grid(): HTMLElement | undefined
  /** Runs first whenever `next` opens, before the overlay records its opener. */
  opening?(next: Overlay): void
}

/**
 * Tracks the open shell overlay, its shortcut scope, and its data. Closing returns focus to the
 * control that opened the overlay; when it left with its row, to the same control in the row now
 * at its index; otherwise to the grid.
 *
 * Overlays that take Escape on their own panel form a stack, and only the innermost takes it, so
 * a kit `Modal` stacked above them keeps its Escape.
 */
export class OverlayState {
  current = $state<Overlay | undefined>()
  /** The row the merchant or category editor edits. */
  editTarget = $state<FocusedRow | undefined>()
  /** The open editor's input, kept while the editor moves between the popover and the sheet. */
  editorDraft = $state<EditorDraft | undefined>()
  deleteTarget = $state<{ identity: string; kind: 'transaction' } | undefined>()
  deleteCount = $state(0)
  information = $state<TransactionInformationResponse | undefined>()
  /** Where the merchant or category editor, or the delete confirmation, hangs. */
  anchor = $state<EditorAnchor | undefined>()
  #host: OverlayHost
  #opener: HTMLElement | undefined
  #openerIndex: number | undefined
  #layers: symbol[] = []
  #popScope: (() => void) | undefined
  #restoreFrame: number | undefined

  constructor(host: OverlayHost) {
    this.#host = host
  }

  /**
   * Opens `next` without tracking state, so an effect that opens it does not rerun on close or
   * when the grid element changes. An `opener` inside the grid, such as a row's actions button,
   * gets focus back instead of the grid.
   */
  open(next: Overlay, opener?: HTMLElement): void {
    untrack(() => {
      this.#host.opening?.(next)
      this.#cancelRestore()
      if (this.current === undefined) {
        this.#opener = opener ?? this.#openerOutsideGrid()
        this.#openerIndex = rowIndex(this.#opener)
      }
      this.#popScope?.()
      this.#popScope = this.#host.pushScope(next)
      this.current = next
    })
  }

  /**
   * Closes the open overlay and returns focus to its opener. With 'keep', focus stays where it
   * is, as after a pointer press outside a popover. With 'grid', focus goes to the grid, as after
   * staging an edit from a phone row's ⋯ button, so the next key acts on the edited row.
   */
  close(focus?: 'keep' | 'grid'): void {
    this.current = undefined
    this.#popScope?.()
    this.#popScope = undefined
    this.editTarget = undefined
    this.editorDraft = undefined
    this.deleteTarget = undefined
    this.deleteCount = 0
    this.information = undefined
    this.anchor = undefined
    if (focus !== undefined) this.#opener = undefined
    if (focus === 'keep') return
    void tick().then(() => {
      this.#restoreFrame = requestAnimationFrame(() => {
        this.#restoreFrame = undefined
        if (this.current === undefined) this.#restoreFocus()
      })
    })
  }

  /**
   * Attaches to an overlay panel that takes Escape itself. While the panel is the innermost
   * layer, an Escape inside it that no control claimed calls `onescape`. An Escape on a control
   * with an open popup belongs to that popup. So does any Escape while a kit popup is open in the
   * panel: kit's `dismissable` takes it on the document, after the panel has seen it. An Escape
   * during text composition only cancels the composition.
   */
  escapeLayer(onescape: () => void): Attachment<HTMLElement> {
    return (panel) => {
      const layer = Symbol('overlay layer')
      this.#layers.push(layer)
      const keydown = (event: KeyboardEvent) => {
        if (event.key !== 'Escape' || event.defaultPrevented) return
        if (this.#layers.at(-1) !== layer) return
        // An Esc that cancels text composition belongs to the input method. Keep it from the
        // window's Escape handlers too, or the panel's dialog closes. Safari reports it as 229.
        if (event.isComposing || event.keyCode === 229) {
          event.stopPropagation()
          return
        }
        const target = event.target
        if (target instanceof Element && target.getAttribute('aria-expanded') === 'true') return
        if (panel.querySelector('.kit-popover-card:not([hidden])')) return
        event.preventDefault()
        event.stopPropagation()
        onescape()
      }
      panel.addEventListener('keydown', keydown)
      return () => {
        panel.removeEventListener('keydown', keydown)
        this.#layers.splice(this.#layers.indexOf(layer), 1)
      }
    }
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
    if (!restoreFocus(opener, this.#openerIndex, this.#host.grid())) this.#host.focusGrid()
  }
}
