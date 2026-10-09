import type { ViewProjection } from '../api/client'
import { drillFrames, type DrillFrame } from '../shell/drills'

/** Column keys. Every gridcell carries its key as `data-column`; `trend` and `vs_prior` arrive
 * with the insights client in PR 6. `actions` is the trailing › or ⋯ button of a compact row. */
export type ColumnKey =
  | 'select'
  | 'name'
  | 'context'
  | 'count'
  | 'in'
  | 'out'
  | 'net'
  | 'share'
  | 'trend'
  | 'vs_prior'
  | 'period'
  | 'net_bar'
  | 'savings'
  | 'date'
  | 'merchant'
  | 'category'
  | 'account'
  | 'amount'
  | 'flags'
  | 'actions'

export type SortField = ViewProjection['view']['sort_fields'][number]

export interface Column {
  key: ColumnKey
  label: string
  sortField?: SortField
  align: 'start' | 'end'
  /** A CSS grid track size. */
  width: string
  /** The column shows only at or above this viewport width. */
  minWidth?: 760
}

/** Metric columns narrow below 760 px (app.css), so Net still fits at 641 px. */
const METRIC_WIDTH = 'var(--mf-metric-width)'
const SELECT: Column = {
  key: 'select',
  label: 'Select all matching',
  align: 'start',
  width: '2rem',
}
const NARROWED_DIMENSIONS = ['merchant', 'category', 'account'] as const

function metric(key: ColumnKey, label: string, sortField?: SortField): Column {
  return { key, label, align: 'end', width: METRIC_WIDTH, ...(sortField ? { sortField } : {}) }
}

function capitalized(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1)
}

function groupedColumns(dimension: string): Column[] {
  const name: Column = {
    key: 'name',
    label: capitalized(dimension),
    sortField: dimension as SortField,
    align: 'start',
    width: 'minmax(10rem, 2fr)',
  }
  const contextLabel = { merchant: 'Top category', category: 'Group' }[dimension]
  const context: Column[] = contextLabel
    ? [
        {
          key: 'context',
          label: contextLabel,
          align: 'start',
          width: 'minmax(7rem, 1fr)',
          minWidth: 760,
        },
      ]
    : []
  return [
    SELECT,
    name,
    ...context,
    metric('count', 'Count', 'count'),
    metric('in', 'In'),
    metric('out', 'Out'),
    metric('net', 'Net', 'amount'),
    { key: 'share', label: 'Share', align: 'end', width: METRIC_WIDTH },
  ]
}

function timeColumns(): Column[] {
  return [
    SELECT,
    {
      key: 'period',
      label: 'Period',
      sortField: 'time_period',
      align: 'start',
      width: 'minmax(8rem, 1fr)',
    },
    metric('count', 'Count', 'count'),
    metric('in', 'In'),
    metric('out', 'Out'),
    metric('net', 'Net', 'amount'),
    { key: 'net_bar', label: 'Net bar', align: 'start', width: 'minmax(8rem, 1.5fr)' },
    { key: 'savings', label: 'Savings rate', align: 'end', width: METRIC_WIDTH },
  ]
}

/** The first drilled dimension, by the TUI's merchant, category, account precedence. */
function narrowedFrame(projection: ViewProjection): DrillFrame | undefined {
  const frames = drillFrames(projection)
  for (const dimension of NARROWED_DIMENSIONS) {
    const frame = frames.find((candidate) => candidate.dimension === dimension)
    if (frame) return frame
  }
  return undefined
}

/** Fits the drilled label, as the TUI does, without cutting off the column's own header. */
function narrowedWidth(frame: DrillFrame, header: string): string {
  return `${Math.min(30, Math.max([...frame.label].length, header.length) + 2)}ch`
}

function transactionColumns(projection: ViewProjection): Column[] {
  const amazon = projection.profile_kind === 'amazon'
  const narrowed = narrowedFrame(projection)
  const columns: Column[] = [
    SELECT,
    { key: 'date', label: 'Date', sortField: 'date', align: 'start', width: '6rem' },
    {
      key: 'merchant',
      label: amazon ? 'Product' : 'Merchant',
      sortField: 'merchant',
      align: 'start',
      width: 'minmax(10rem, 2fr)',
    },
    {
      key: 'category',
      label: 'Category',
      sortField: 'category',
      align: 'start',
      width: 'minmax(8rem, 1.5fr)',
    },
    {
      key: 'account',
      label: amazon ? 'Order' : 'Account',
      sortField: 'account',
      align: 'start',
      width: 'minmax(8rem, 1fr)',
    },
    metric('amount', 'Amount', 'amount'),
    { key: 'flags', label: 'Flags', align: 'start', width: '6.5rem' },
  ]
  return columns.map((column) =>
    narrowed && column.key === narrowed.dimension
      ? { ...column, width: narrowedWidth(narrowed, column.label) }
      : column,
  )
}

/** The class list of a header or body cell in the column. */
export function cellClass(column: Column): string {
  return column.align === 'end' ? 'mf-table__cell mf-table__cell--end' : 'mf-table__cell'
}

/** The media query that shows the columns with a `minWidth` of 760. */
export const WIDE_COLUMNS_QUERY = '(min-width: 760px)'

/** The columns that fit: a column with a `minWidth` drops out unless the viewport is `wide`. */
export function columnsAtWidth(columns: Column[], wide: boolean): Column[] {
  return columns.filter((column) => column.minWidth === undefined || wide)
}

/** Phone rows' cells in reading order. A phone header shows two of them, so cells are numbered. */
const COMPACT_ORDERS = {
  detail: ['select', 'merchant', 'amount', 'date', 'category', 'account', 'flags', 'actions'],
  aggregate: ['select', 'name', 'net', 'count', 'in', 'out', 'actions'],
  time: ['select', 'period', 'net', 'count', 'in', 'out', 'actions'],
} as const satisfies Record<string, readonly ColumnKey[]>

export type CompactLayout = keyof typeof COMPACT_ORDERS

/** The phone row layout of the projection's rows. */
export function compactLayout(projection: ViewProjection): CompactLayout {
  if (projection.detail_rows) return 'detail'
  return (projection.view.sub_grouping ?? projection.view.grouping) === 'time'
    ? 'time'
    : 'aggregate'
}

/** The `aria-colindex` of a phone row's cell, so each value is announced under its header. */
export function compactColumnIndex(layout: CompactLayout, key: ColumnKey): number {
  return (COMPACT_ORDERS[layout] as readonly ColumnKey[]).indexOf(key) + 1
}

/** The `aria-colcount` of a grid of phone rows. */
export function compactColumnCount(layout: CompactLayout): number {
  return COMPACT_ORDERS[layout].length
}

/** The columns of the projection's view. Only the server's sort fields stay sortable. */
export function columnsFor(projection: ViewProjection): Column[] {
  const view = projection.view
  const dimension = view.sub_grouping ?? view.grouping
  let columns: Column[]
  if (projection.detail_rows) columns = transactionColumns(projection)
  else if (dimension === 'time') columns = timeColumns()
  else columns = groupedColumns(dimension)
  const sortable = new Set<string>(view.sort_fields)
  return columns.map(({ sortField, ...column }) =>
    sortField && sortable.has(sortField) ? { ...column, sortField } : column,
  )
}
