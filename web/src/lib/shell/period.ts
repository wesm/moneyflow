import type { TransitionBody, ViewProjection } from '../api/client'
import { drillFrames } from './drills'

/** The view's date range: inclusive local YYYY-MM-DD dates, or undefined for all time. */
export type DateRange = ViewProjection['filters']['date_range']
/** A `time.set-period` target. */
export type Period = Required<NonNullable<TransitionBody['period']>>
export type ScopeKind = 'day' | 'month' | 'year' | 'custom' | 'all'
export type PeriodKind = 'day' | 'month' | 'year'
export type ChooserTab = PeriodKind | 'custom'
type Currency = ViewProjection['currencies'][number]
type CurrencyChoice = Pick<Currency, 'currency' | 'scale' | 'count'>

export const MONTHS = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
] as const

function parts(date: string): [number, number, number] {
  const [year, month, day] = date.split('-').map(Number)
  return [year ?? 0, month ?? 0, day ?? 0]
}

function pad(value: number, width = 2): string {
  return String(value).padStart(width, '0')
}

/** Formats a calendar date as YYYY-MM-DD. `month` counts from 1. */
export function isoDate(year: number, month: number, day: number): string {
  return `${pad(year, 4)}-${pad(month)}-${pad(day)}`
}

function daysInMonth(year: number, month: number): number {
  return new Date(Date.UTC(year, month, 0)).getUTCDate()
}

/** The calendar day, month, or year that contains `date`. */
export function periodContaining(date: string, kind: PeriodKind): Period {
  const [year, month] = parts(date)
  if (kind === 'day') return { start: date, end: date }
  if (kind === 'month') {
    return { start: isoDate(year, month, 1), end: isoDate(year, month, daysInMonth(year, month)) }
  }
  return { start: isoDate(year, 1, 1), end: isoDate(year, 12, 31) }
}

/** Names the kind of scope a date range covers. */
export function scopeKind(range: DateRange): ScopeKind {
  if (!range) return 'all'
  if (range.from === range.to) return 'day'
  for (const kind of ['month', 'year'] as const) {
    const period = periodContaining(range.from, kind)
    if (period.start === range.from && period.end === range.to) return kind
  }
  return 'custom'
}

function monthYear(date: string): string {
  const [year, month] = parts(date)
  return `${MONTHS[month - 1]} ${year}`
}

function monthDay(date: string): string {
  const [, month, day] = parts(date)
  return `${MONTHS[month - 1]} ${day}`
}

/**
 * Names a partial chart period with what it covers. A whole calendar period that holds `today`
 * reads `Oct 2026 (to date)`. A period a custom range clips names the days it covers, such as
 * `Jan 2026 (Jan 15–31)` or `2021 (Mar 5 – Dec 31)`, through today when it holds today:
 * `Oct 2026 (Oct 10 – to date)`. A period never spans two years.
 */
export function partialName(
  name: string,
  period: { start: string; end: string; period: { granularity: string } },
  today: string,
): string {
  const kind = period.period.granularity
  // The API types granularity as a string; any other value counts as a whole period.
  const known = kind === 'day' || kind === 'month' || kind === 'year'
  const calendar = known ? periodContaining(period.start, kind) : period
  const clipped = period.start !== calendar.start || period.end !== calendar.end
  const current = period.start <= today && today <= period.end
  if (!clipped) return current ? `${name} (to date)` : name
  if (current) return `${name} (${monthDay(period.start)} – to date)`
  const span =
    period.start.slice(0, 7) === period.end.slice(0, 7)
      ? `${monthDay(period.start)}–${parts(period.end)[2]}`
      : `${monthDay(period.start)} – ${monthDay(period.end)}`
  return `${name} (${span})`
}

/** "Sep 15, 2026" */
export function dayLabel(date: string): string {
  return `${monthDay(date)}, ${parts(date)[0]}`
}

/** The period control's label for a date range. */
export function periodLabel(range: DateRange): string {
  if (!range) return 'All time'
  switch (scopeKind(range)) {
    case 'day':
      return dayLabel(range.from)
    case 'month':
      return monthYear(range.from)
    case 'year':
      return range.from.slice(0, 4)
    default:
      if (range.from.slice(0, 4) === range.to.slice(0, 4)) {
        return `${monthDay(range.from)} – ${dayLabel(range.to)}`
      }
      return `${dayLabel(range.from)} – ${dayLabel(range.to)}`
  }
}

/**
 * The date that Day, Month, and Year segments resolve against: the scope's last day, or
 * Latest when the scope contains it; Latest for all time. Undefined without transactions.
 */
export function anchorDate(range: DateRange, latest: string | undefined): string | undefined {
  if (latest === undefined) return undefined
  if (!range) return latest
  return range.from <= latest && latest <= range.to ? latest : range.to
}

/** The currency the view shows, with its first and latest transaction dates. */
export function activeCurrency(projection: ViewProjection): Currency | undefined {
  const scope = projection.view.currency
  if (!scope) return undefined
  return projection.currencies.find(
    (entry) => entry.currency === scope.currency && entry.scale === scope.scale,
  )
}

/**
 * The currencies the view can switch between: the profile's, plus the view's own with no
 * transactions once its last one is gone, so the view can still leave it.
 */
export function currencyChoices(projection: ViewProjection): CurrencyChoice[] {
  const scope = projection.view.currency
  if (!scope || activeCurrency(projection)) return projection.currencies
  return [...projection.currencies, { currency: scope.currency, scale: scope.scale, count: 0 }]
}

/** Whether ‹ and › can move the period: one calendar unit, or a time drill to move. */
export function canShiftPeriod(projection: ViewProjection): boolean {
  const kind = scopeKind(projection.filters.date_range)
  if (kind === 'day' || kind === 'month' || kind === 'year') return true
  return drillFrames(projection).some((frame) => frame.dimension === 'time')
}

const stepKeys: Record<string, (columns: number) => number> = {
  ArrowLeft: () => -1,
  ArrowRight: () => 1,
  ArrowUp: (columns) => -columns,
  ArrowDown: (columns) => columns,
}

/**
 * Moves focus within a grid of buttons: arrows step over disabled cells and stop at the
 * edges, Home and End go to the first and last enabled cell. Undefined for other keys.
 */
export function gridStep(
  key: string,
  index: number,
  disabled: boolean[],
  columns: number,
): number | undefined {
  const enabled = disabled.flatMap((off, cell) => (off ? [] : [cell]))
  if (key === 'Home') return enabled[0] ?? index
  if (key === 'End') return enabled.at(-1) ?? index
  const step = stepKeys[key]?.(columns)
  if (step === undefined) return undefined
  for (let next = index + step; next >= 0 && next < disabled.length; next += step) {
    if (!disabled[next]) return next
  }
  return index
}

/** Moves a date outside first to latest, inclusive, to the nearer end. */
export function clampDate(date: string, first: string, latest: string): string {
  if (date < first) return first
  return date > latest ? latest : date
}

const dayStepKeys: Record<string, number> = {
  ArrowLeft: -1,
  ArrowRight: 1,
  ArrowUp: -7,
  ArrowDown: 7,
}

function addDays(date: string, days: number): string {
  const [year, month, day] = parts(date)
  const next = new Date(Date.UTC(year, month - 1, day + days))
  return isoDate(next.getUTCFullYear(), next.getUTCMonth() + 1, next.getUTCDate())
}

/** The days a chooser calendar can move between: first to latest transaction, inclusive. */
export interface DayLimits {
  first: string
  latest: string
}

/**
 * Moves focus in a day calendar showing `month`: Left and Right step a day, Up and Down a
 * week. A step past `latest` or before `first` stays put. A step out of the shown month
 * pages the calendar to the new day. Undefined for other keys.
 */
export function calendarStep(
  key: string,
  date: string,
  month: string,
  limits: DayLimits,
): { date: string; month: string } | undefined {
  const step = dayStepKeys[key]
  if (step === undefined) return undefined
  const next = addDays(date, step)
  if (step > 0 ? next > limits.latest : next < limits.first) return { date, month }
  return { date: next, month: next.slice(0, 7) === month.slice(0, 7) ? month : next }
}

/** The letter that switches the chooser to each tab. */
export const CHOOSER_KEYS: Record<ChooserTab, string> = {
  day: 'd',
  month: 'm',
  year: 'y',
  custom: 'c',
}

/** The chooser tab a letter key switches to, if any. */
export function chooserTabForKey(key: string): ChooserTab | undefined {
  return (Object.keys(CHOOSER_KEYS) as ChooserTab[]).find((tab) => CHOOSER_KEYS[tab] === key)
}

/** The chooser opens on the scope's kind; All and Custom open on Month. */
export function chooserTab(kind: ScopeKind): ChooserTab {
  return kind === 'all' || kind === 'custom' ? 'month' : kind
}

/** The Custom tab starts from the current custom range, or empty. */
export function customDraft(range: DateRange): { from: string; to: string } {
  return range && scopeKind(range) === 'custom' ? { ...range } : { from: '', to: '' }
}

export interface PeriodCell {
  /** The cell's first day. */
  key: string
  label: string
  /** The accessible name. */
  name: string
  /** The cell is the current scope. */
  selected: boolean
  disabled: boolean
}

/** The twelve months of `year`, enabled from the first transaction's month to Latest's. */
export function monthCells(
  year: number,
  range: DateRange,
  first: string,
  latest: string,
): PeriodCell[] {
  return MONTHS.map((label, index) => {
    const key = isoDate(year, index + 1, 1)
    const month = key.slice(0, 7)
    return {
      key,
      label,
      name: `${label} ${year}`,
      selected: scopeKind(range) === 'month' && range?.from === key,
      disabled: month < first.slice(0, 7) || month > latest.slice(0, 7),
    }
  })
}

/** One cell per year from the first transaction's year to Latest's. */
export function yearCells(firstYear: number, latestYear: number, range: DateRange): PeriodCell[] {
  const cells: PeriodCell[] = []
  for (let year = firstYear; year <= latestYear; year++) {
    const key = isoDate(year, 1, 1)
    const label = String(year)
    const selected = scopeKind(range) === 'year' && range?.from === key
    cells.push({ key, label, name: label, selected, disabled: false })
  }
  return cells
}

export interface EmptyScope {
  label: string
  /** The period at the same resolution that contains Latest; Month for a custom range. */
  jump: Period
}

/**
 * Describes a chosen period with no transactions in the view's currency. All time, searches,
 * and drill-downs are not empty periods: the table's own empty state covers them, since Latest
 * for the currency may not hold a transaction inside the drill-down. Neither is the period that
 * holds Latest: Latest counts hidden transactions and transfers, so filters alone emptied it.
 */
export function emptyScope(projection: ViewProjection): EmptyScope | undefined {
  const currency = activeCurrency(projection)
  const range = projection.filters.date_range
  const kind = scopeKind(range)
  if (!currency || kind === 'all' || projection.view.search) return undefined
  if (drillFrames(projection).length > 0) return undefined
  const counted = (projection.statistics ?? []).some(
    (entry) =>
      entry.currency === currency.currency && entry.scale === currency.scale && entry.count > 0,
  )
  if (counted) return undefined
  const jump = periodContaining(currency.latest, kind === 'custom' ? 'month' : kind)
  if (jump.start === range?.from && jump.end === range.to) return undefined
  return { label: periodLabel(range), jump }
}
