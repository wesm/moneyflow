import type { ViewProjection } from '../api/client'
import { contextCell } from './cells'
import type { Column, ColumnKey } from './columns'
import type { TableRow } from './virtual-rows'

/** How many of the widest different labels in the loaded windows size each text column. */
const KEPT = 5

/** How wide `text` draws, in any unit that ranks labels of one column. */
export type LabelWidth = (text: string) => number

/** Ranks labels by their length, where no canvas can measure them. */
export const characterCount: LabelWidth = (text) => [...text].length

/**
 * Measures labels in the page's font on a canvas, which lays nothing out, and remembers each
 * width. A label's rank does not depend on the font size, or much on its weight, so one font
 * ranks every column. Counts characters where no canvas can measure text, as in jsdom.
 */
export function drawnWidth(): LabelWidth {
  const context = document.createElement('canvas').getContext('2d')
  if (!context) return characterCount
  context.font = `16px ${getComputedStyle(document.body).fontFamily}`
  const widths = new Map<string, number>()
  return (text) => {
    let width = widths.get(text)
    if (width === undefined) {
      width = context.measureText(text).width
      widths.set(text, width)
    }
    return width
  }
}

/** The text `row` shows in the text column `key`, such as a staged merchant's old → new names. */
export function shownText(row: TableRow, key: ColumnKey, dimension: string): string {
  if ('merchant' in row) {
    if (key === 'merchant') return staged(row.merchant, row.staged?.merchant)
    if (key === 'category') return staged(row.category, row.staged?.category)
    if (key === 'account') return row.account
    return ''
  }
  if (key === 'name' || key === 'period') return row.label
  if (key === 'context') return contextCell(row, dimension)
  return ''
}

function staged(committed: string, value: string | undefined): string {
  return value === undefined || value === committed ? committed : `${committed} → ${value}`
}

/**
 * The rows whose labels size the grid's text columns: for each column, rows with the few
 * widest different labels of every window the view has loaded. The grid draws them in hidden
 * rows, and the rows on screen never size a column, so widths hold still while the table
 * scrolls. A label that once sized a column stays while the view does, so a window that loads
 * later can only widen it. A new view, sort, or revision starts over.
 */
export class ColumnSizer {
  #view: string | undefined
  /** For each text column, the row standing for each label that has sized it. */
  #kept = new Map<ColumnKey, Map<string, TableRow>>()
  #rows: TableRow[] = []
  readonly #width: LabelWidth

  constructor(width: LabelWidth = characterCount) {
    this.#width = width
  }

  /**
   * The rows to draw in the sizer for `view`, the canonical query and revision. The same array
   * returns while its rows stay the same.
   */
  rows(
    view: string,
    windows: readonly ViewProjection[],
    columns: readonly Column[],
    dimension: string,
  ): TableRow[] {
    if (view !== this.#view) {
      this.#view = view
      this.#kept = new Map()
    }
    const loaded: TableRow[] = []
    for (const window of windows)
      loaded.push(...(window.detail_rows ?? window.aggregate_rows ?? []))
    const rows = new Map<string, TableRow>()
    for (const column of columns) {
      if (!column.text) continue
      const kept = this.#widest(column.key, loaded, dimension)
      for (const row of kept) rows.set(row.identity, row)
    }
    const next = [...rows.values()]
    const same =
      next.length === this.#rows.length && next.every((row, index) => row === this.#rows[index])
    if (!same) this.#rows = next
    return this.#rows
  }

  #widest(key: ColumnKey, loaded: readonly TableRow[], dimension: string): TableRow[] {
    // A kept label stays even when wider ones arrive, so a column never narrows mid-scroll. Its row
    // keeps standing for it, so the sizer redraws nothing when a new window, such as after a
    // selection toggle, holds the same labels.
    const kept = this.#kept.get(key) ?? new Map<string, TableRow>()
    const labels = new Map(kept)
    for (const row of loaded) {
      const text = shownText(row, key, dimension)
      if (!labels.has(text)) labels.set(text, row)
    }
    const widest: { text: string; row: TableRow; width: number }[] = []
    for (const [text, row] of labels) {
      const width = this.#width(text)
      const at = widest.findIndex((ranked) => width > ranked.width)
      if (at >= 0) widest.splice(at, 0, { text, row, width })
      else widest.push({ text, row, width })
      if (widest.length > KEPT) widest.pop()
    }
    for (const { text, row } of widest) if (!kept.has(text)) kept.set(text, row)
    this.#kept.set(key, kept)
    return [...kept.values()]
  }
}
