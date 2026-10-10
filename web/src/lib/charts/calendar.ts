import type { InsightsResponse } from '../api/client'
import { formatMinor, parseMinor } from '../money/format'
import { dayLabel, MONTHS } from '../shell/period'

type Ranges = InsightsResponse['ranges']
export type Heatmap = NonNullable<Ranges['heatmap']>
type DateRange = NonNullable<Ranges['scope']>
export type OutLevel = 0 | 1 | 2 | 3 | 4

/** The summary's `calendar`, one value per heatmap day, with the currency's scale. */
export interface CalendarDays {
  in: readonly string[]
  out: readonly string[]
  scale: number
}

/** One heatmap day. */
export interface CalendarCell {
  date: string
  /** 0 without Out; otherwise 1–4, the quarter of the drawn days with Out that it falls in. */
  outLevel: OutLevel
  hasIn: boolean
  /** Inside the view's scope; other days are drawn faded. */
  inScope: boolean
  /** After today, so drawn blank. */
  future: boolean
  /** The cursor transaction's day. */
  cursor: boolean
  /** The accessible name: `{Mon D, YYYY}, out {out}`, plus `, in {in}` when In is not zero. */
  name: string
}

/** Seven slots in weekday order; a slot outside the heatmap's days is empty. */
export type CalendarWeek = (CalendarCell | undefined)[]

/** One month's calendar grid, labeled, or the whole unlabeled strip of week columns. */
export interface CalendarBlock {
  label?: string
  weeks: CalendarWeek[]
}

const dayMilliseconds = 86_400_000

function dayNumber(date: string): number {
  return Date.parse(`${date}T00:00:00Z`) / dayMilliseconds
}

function isoDay(day: number): string {
  return new Date(day * dayMilliseconds).toISOString().slice(0, 10)
}

function magnitude(minor: string): bigint {
  const value = parseMinor(minor)
  return value < 0n ? -value : value
}

/**
 * Levels by rank, not by size against the largest day, so one large payment does not wash out
 * every other day. Equal amounts share a level, and the largest day is always level 4.
 */
function outLevels(out: readonly string[]): OutLevel[] {
  const magnitudes = out.map(magnitude)
  const ranked = magnitudes.filter((value) => value > 0n).sort((a, b) => (a < b ? -1 : 1))
  const atOrBelow = new Map<bigint, number>()
  ranked.forEach((value, index) => atOrBelow.set(value, index + 1))
  return magnitudes.map((value) => {
    const rank = atOrBelow.get(value)
    if (rank === undefined) return 0
    return Math.ceil((4 * rank) / ranked.length) as OutLevel
  })
}

/** Splits `cells` into weeks after `lead` empty slots, padding the last week. */
function weeksOf(cells: readonly CalendarCell[], lead: number): CalendarWeek[] {
  const slots: CalendarWeek = [...Array<undefined>(lead), ...cells]
  const weeks: CalendarWeek[] = []
  for (let start = 0; start < slots.length; start += 7) {
    const week = slots.slice(start, start + 7)
    weeks.push([...week, ...Array<undefined>(7 - week.length)])
  }
  return weeks
}

/** Sunday-first grids, one per calendar month. */
function monthBlocks(cells: readonly CalendarCell[]): CalendarBlock[] {
  const blocks: CalendarBlock[] = []
  let start = 0
  while (start < cells.length) {
    const opening = cells[start]!.date
    let end = start
    while (end < cells.length && cells[end]!.date.startsWith(opening.slice(0, 7))) end += 1
    const day = new Date(dayNumber(opening) * dayMilliseconds)
    blocks.push({
      label: `${MONTHS[day.getUTCMonth()]} ${day.getUTCFullYear()}`,
      weeks: weeksOf(cells.slice(start, end), day.getUTCDay()),
    })
    start = end
  }
  return blocks
}

/**
 * The heatmap's days arranged for drawing. Layout `months` gives one Sunday-first grid per
 * month; layout `weeks` gives one strip of week columns that ends on the heatmap's last day,
 * which is 53 columns for a year or for the 371-day strip ending at Latest. Throws unless the
 * calendar holds one value per heatmap day.
 */
export function calendarCells(
  heatmap: Heatmap,
  calendar: CalendarDays,
  scope: DateRange | undefined,
  today: string,
  cursorDate?: string,
): CalendarBlock[] {
  const first = dayNumber(heatmap.start)
  const count = Math.round(dayNumber(heatmap.end) - first) + 1
  for (const field of ['in', 'out'] as const) {
    if (calendar[field].length !== count) {
      throw new Error(
        `Calendar ${field} has ${calendar[field].length} values for ${count} heatmap days.`,
      )
    }
  }
  const levels = outLevels(calendar.out)
  const cells = calendar.out.map((out, index): CalendarCell => {
    const date = isoDay(first + index)
    const rising = calendar.in[index] ?? '0'
    const hasIn = parseMinor(rising) !== 0n
    const spoken = `${dayLabel(date)}, out ${formatMinor(out, calendar.scale)}`
    return {
      date,
      outLevel: levels[index] ?? 0,
      hasIn,
      inScope: scope === undefined || (date >= scope.start && date <= scope.end),
      future: date > today,
      cursor: date === cursorDate,
      name: hasIn ? `${spoken}, in ${formatMinor(rising, calendar.scale)}` : spoken,
    }
  })
  if (heatmap.layout === 'months') return monthBlocks(cells)
  return [{ weeks: weeksOf(cells, (7 - (cells.length % 7)) % 7) }]
}
