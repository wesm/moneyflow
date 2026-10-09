import { describe, expect, it } from 'vitest'

import { revealScrollTop, visibleRange } from './layout'
import { loadedRow, rowToShow } from './virtual-rows'
import { testAggregateRow, testProjection } from '../../test/projection'

describe('visibleRange', () => {
  it('names the first and last rows that are fully inside the viewport', () => {
    expect(visibleRange(0, 300, 30, 1000)).toEqual({ first: 0, last: 9 })
    expect(visibleRange(45, 300, 30, 1000)).toEqual({ first: 2, last: 10 })
    expect(visibleRange(6000, 300, 30, 1000)).toEqual({ first: 200, last: 209 })
  })

  it('stops at the last row when scrolled to the end', () => {
    expect(visibleRange(29_700, 300, 30, 1000)).toEqual({ first: 990, last: 999 })
    expect(visibleRange(40_000, 300, 30, 1000)).toEqual({ first: 999, last: 999 })
  })

  it('treats a scroll position above the rows as the top', () => {
    expect(visibleRange(-20, 300, 30, 1000)).toEqual({ first: 0, last: 8 })
  })

  it('keeps one row when the viewport is shorter than a row', () => {
    expect(visibleRange(60, 10, 30, 1000)).toEqual({ first: 2, last: 2 })
  })

  it('reports an empty range without rows', () => {
    expect(visibleRange(0, 300, 30, 0)).toEqual({ first: 0, last: -1 })
  })
})

describe('revealScrollTop', () => {
  // A 330 px scroller showing the 30 px header and ten rows, with the grid 4 px down in it.
  const port = (scrollTop: number) => ({ scrollTop, viewport: 330, rowHeight: 30, gridTop: 4 })

  it('keeps the position while the row shows whole below the header', () => {
    expect(revealScrollTop(10, port(304))).toBe(304)
    expect(revealScrollTop(19, port(304))).toBe(304)
  })

  it('scrolls a row above the viewport to just below the sticky header', () => {
    expect(revealScrollTop(9, port(304))).toBe(274)
  })

  it('scrolls a row below the viewport up to the bottom edge', () => {
    expect(revealScrollTop(20, port(304))).toBe(334)
    expect(revealScrollTop(300, port(0))).toBe(8734)
  })

  it('scrolls to the very top for the first row', () => {
    expect(revealScrollTop(0, port(300))).toBe(0)
  })
})

describe('loadedRow', () => {
  const window = (offset: number, count: number) =>
    testProjection({
      detail_rows: null,
      aggregate_rows: Array.from({ length: count }, (_, position) =>
        testAggregateRow({ index: offset + position, identity: `agg-${offset + position}` }),
      ),
      total_rows: 450,
      window: { offset, limit: 200, count },
    })

  it('finds a row in whichever loaded window holds it', () => {
    const windows = [window(200, 200), window(0, 200), window(400, 50)]
    expect(loadedRow(windows, 0)?.identity).toBe('agg-0')
    expect(loadedRow(windows, 199)?.identity).toBe('agg-199')
    expect(loadedRow(windows, 200)?.identity).toBe('agg-200')
    expect(loadedRow(windows, 449)?.identity).toBe('agg-449')
  })

  it('has no row outside the loaded windows', () => {
    const windows = [window(200, 200)]
    expect(loadedRow(windows, 199)).toBeUndefined()
    expect(loadedRow(windows, 400)).toBeUndefined()
    expect(loadedRow(windows, -1)).toBeUndefined()
  })
})

describe('rowToShow', () => {
  const loaded = { offset: 0, limit: 200 }

  it('asks for the first visible row once scrolling leaves the window and the cursor', () => {
    expect(rowToShow({ first: 300, last: 319 }, 5, loaded)).toBe(300)
  })

  it('asks for nothing while the first visible row is in the loaded window', () => {
    expect(rowToShow({ first: 150, last: 169 }, 5, loaded)).toBeUndefined()
  })

  it('keeps the window of a cursor that shows on screen', () => {
    expect(rowToShow({ first: 181, last: 200 }, 200, { offset: 200, limit: 200 })).toBeUndefined()
    expect(rowToShow({ first: 300, last: 319 }, 319, loaded)).toBeUndefined()
  })

  it('asks for nothing without rows', () => {
    expect(rowToShow({ first: 0, last: -1 }, 0, { offset: 200, limit: 200 })).toBeUndefined()
  })
})
