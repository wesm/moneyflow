import { describe, expect, it } from 'vitest'

import { calendarCells, type CalendarCell, type CalendarDays } from './calendar'

/** Every day from `start` through `end`, as ISO dates. */
function daysFrom(start: string, end: string): string[] {
  const days: string[] = []
  for (let day = new Date(`${start}T00:00:00Z`); ; day.setUTCDate(day.getUTCDate() + 1)) {
    const iso = day.toISOString().slice(0, 10)
    days.push(iso)
    if (iso === end) return days
  }
}

/** A calendar with no money on any day but the given ones. */
function calendar(
  start: string,
  end: string,
  amounts: Record<string, { in?: string; out?: string }> = {},
): CalendarDays {
  const days = daysFrom(start, end)
  return {
    in: days.map((day) => amounts[day]?.in ?? '0'),
    out: days.map((day) => amounts[day]?.out ?? '0'),
    scale: 2,
  }
}

const september = { layout: 'months', start: '2026-09-01', end: '2026-09-30' } as const
const septemberScope = { start: '2026-09-01', end: '2026-09-30' }

function cells(blocks: ReturnType<typeof calendarCells>): CalendarCell[] {
  return blocks.flatMap((block) => block.weeks.flat()).filter((cell) => cell !== undefined)
}

function cellOn(blocks: ReturnType<typeof calendarCells>, date: string): CalendarCell {
  const found = cells(blocks).find((cell) => cell.date === date)
  if (!found) throw new Error(`no cell for ${date}`)
  return found
}

describe('calendarCells', () => {
  it('lays a month out as a Sunday-first calendar grid', () => {
    const blocks = calendarCells(
      september,
      calendar('2026-09-01', '2026-09-30'),
      septemberScope,
      '2026-10-07',
    )
    expect(blocks).toHaveLength(1)
    const [block] = blocks
    expect(block?.label).toBe('Sep 2026')
    // September 1, 2026 is a Tuesday; the 30th is a Wednesday.
    expect(block?.weeks).toHaveLength(5)
    expect(block?.weeks[0]?.map((cell) => cell?.date)).toEqual([
      undefined,
      undefined,
      '2026-09-01',
      '2026-09-02',
      '2026-09-03',
      '2026-09-04',
      '2026-09-05',
    ])
    expect(block?.weeks[4]?.map((cell) => cell?.date)).toEqual([
      '2026-09-27',
      '2026-09-28',
      '2026-09-29',
      '2026-09-30',
      undefined,
      undefined,
      undefined,
    ])
  })

  it('draws one grid per month of a short custom range', () => {
    const heatmap = { layout: 'months', start: '2026-08-01', end: '2026-09-30' } as const
    const blocks = calendarCells(
      heatmap,
      calendar(heatmap.start, heatmap.end),
      { start: '2026-08-20', end: '2026-09-10' },
      '2026-10-07',
    )
    expect(blocks.map((block) => block.label)).toEqual(['Aug 2026', 'Sep 2026'])
    expect(cells(blocks)).toHaveLength(61)
  })

  it('lays a year out as a strip of 53 week columns ending on its last day', () => {
    const heatmap = { layout: 'weeks', start: '2025-01-01', end: '2025-12-31' } as const
    const blocks = calendarCells(
      heatmap,
      calendar(heatmap.start, heatmap.end),
      { start: '2025-01-01', end: '2025-12-31' },
      '2026-10-07',
    )
    expect(blocks).toHaveLength(1)
    const weeks = blocks[0]!.weeks
    expect(weeks).toHaveLength(53)
    // 365 days fill 52 weeks and one day, so the first column holds only January 1.
    expect(weeks[0]?.filter((cell) => cell !== undefined).map((cell) => cell.date)).toEqual([
      '2025-01-01',
    ])
    expect(weeks[52]?.at(-1)?.date).toBe('2025-12-31')
    expect(cells(blocks)).toHaveLength(365)
  })

  it('fills all 53 columns of the strip that ends at Latest', () => {
    const heatmap = { layout: 'weeks', start: '2025-10-02', end: '2026-10-07' } as const
    const blocks = calendarCells(
      heatmap,
      calendar(heatmap.start, heatmap.end),
      undefined,
      '2026-10-07',
    )
    const weeks = blocks[0]!.weeks
    expect(weeks).toHaveLength(53)
    expect(weeks.every((week) => week.every((cell) => cell !== undefined))).toBe(true)
    expect(weeks[0]?.[0]?.date).toBe('2025-10-02')
  })

  it('splits days with Out into four levels by size, leaving days without Out at zero', () => {
    const blocks = calendarCells(
      september,
      calendar('2026-09-01', '2026-09-30', {
        '2026-09-01': { out: '-100' },
        '2026-09-02': { out: '-200' },
        '2026-09-03': { out: '-300' },
        '2026-09-04': { out: '-400000' },
        '2026-09-05': { out: '-200' },
        '2026-09-06': { in: '5000' },
      }),
      septemberScope,
      '2026-10-07',
    )
    const level = (date: string) => cellOn(blocks, date).outLevel
    expect(level('2026-09-01')).toBe(1)
    // Equal amounts share a level.
    expect(level('2026-09-02')).toBe(3)
    expect(level('2026-09-05')).toBe(3)
    expect(level('2026-09-03')).toBe(4)
    expect(level('2026-09-04')).toBe(4)
    expect(level('2026-09-06')).toBe(0)
    expect(level('2026-09-07')).toBe(0)
  })

  it('gives the only day with Out the top level', () => {
    const blocks = calendarCells(
      september,
      calendar('2026-09-01', '2026-09-30', { '2026-09-15': { out: '-1' } }),
      septemberScope,
      '2026-10-07',
    )
    expect(cellOn(blocks, '2026-09-15').outLevel).toBe(4)
  })

  it('marks days with In, whatever their Out', () => {
    const blocks = calendarCells(
      september,
      calendar('2026-09-01', '2026-09-30', {
        '2026-09-15': { in: '12000', out: '-48000' },
        '2026-09-16': { out: '-48000' },
      }),
      septemberScope,
      '2026-10-07',
    )
    expect(cellOn(blocks, '2026-09-15').hasIn).toBe(true)
    expect(cellOn(blocks, '2026-09-16').hasIn).toBe(false)
  })

  it('names each day with its Out, and its In only when there is some', () => {
    const blocks = calendarCells(
      september,
      calendar('2026-09-01', '2026-09-30', {
        '2026-09-15': { in: '12000', out: '-48000' },
        '2026-09-16': { out: '-123456' },
      }),
      septemberScope,
      '2026-10-07',
    )
    expect(cellOn(blocks, '2026-09-15').name).toBe('Sep 15, 2026, out -480.00, in +120.00')
    expect(cellOn(blocks, '2026-09-16').name).toBe('Sep 16, 2026, out -1,234.56')
    expect(cellOn(blocks, '2026-09-17').name).toBe('Sep 17, 2026, out +0.00')
  })

  it('puts only the scope inside a Day scope, and every day inside All', () => {
    const day = calendarCells(
      september,
      calendar('2026-09-01', '2026-09-30'),
      { start: '2026-09-15', end: '2026-09-15' },
      '2026-10-07',
    )
    expect(
      cells(day)
        .filter((cell) => cell.inScope)
        .map((cell) => cell.date),
    ).toEqual(['2026-09-15'])
    const all = calendarCells(
      september,
      calendar('2026-09-01', '2026-09-30'),
      undefined,
      '2026-10-07',
    )
    expect(cells(all).every((cell) => cell.inScope)).toBe(true)
  })

  it('marks days after today as future', () => {
    const october = { layout: 'months', start: '2026-10-01', end: '2026-10-31' } as const
    const blocks = calendarCells(
      october,
      calendar(october.start, october.end),
      { start: '2026-10-01', end: '2026-10-31' },
      '2026-10-07',
    )
    expect(cellOn(blocks, '2026-10-07').future).toBe(false)
    expect(cellOn(blocks, '2026-10-08').future).toBe(true)
    expect(cells(blocks).filter((cell) => cell.future)).toHaveLength(24)
  })

  it('marks the cursor transaction’s day, and no day without one', () => {
    const data = calendar('2026-09-01', '2026-09-30')
    const marked = calendarCells(september, data, septemberScope, '2026-10-07', '2026-09-15')
    expect(
      cells(marked)
        .filter((cell) => cell.cursor)
        .map((cell) => cell.date),
    ).toEqual(['2026-09-15'])
    const unmarked = calendarCells(september, data, septemberScope, '2026-10-07')
    expect(cells(unmarked).some((cell) => cell.cursor)).toBe(false)
  })

  it('rejects a calendar that does not hold one value per heatmap day', () => {
    const short = calendar('2026-09-01', '2026-09-29')
    expect(() => calendarCells(september, short, septemberScope, '2026-10-07')).toThrow(
      /29 values for 30 heatmap days/,
    )
  })
})
