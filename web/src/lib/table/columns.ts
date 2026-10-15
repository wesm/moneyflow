import type { ViewProjection } from '../api/client'
import { drillFrames } from '../shell/drills'

/** Column keys. Every gridcell carries its key as `data-column`. `actions` is the trailing › or
 * ⋯ button of a compact row. */
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
  /**
   * The column shows labels, and its track fits the longest one the view has shown; see
   * `columnTracks`.
   */
  text?: true
  /** Among text columns, the lowest takes the room the other columns leave. */
  slack?: 1 | 2 | 3
  /** The column shows only at or above this viewport width. */
  minWidth?: 760
  /** The track's minimum in rem, which `columnsThatFit` adds up. */
  min: number
  /** When the minimums overflow the workspace, columns drop in this order; others stay. */
  dropOrder?: 1 | 2 | 3 | 4 | 5
}

const METRIC_MIN = 6.5
const METRIC_WIDTH = `${METRIC_MIN}rem`
const SELECT: Column = {
  key: 'select',
  label: 'Select all matching',
  align: 'start',
  width: '2rem',
  min: 2,
}
/**
 * A text column's track: no wider than its longest label, as the grid's sizer row holds it, and
 * never wider than the room. Where the room is short, the grid gives each text column an equal
 * share and a column whose labels fit in less keeps only what it needs, so the widest columns
 * narrow first.
 */
const TEXT_TRACK = 'minmax(0, max-content)'
/**
 * The track of the text column that takes the room the other columns leave. An `auto` maximum
 * sizes like `max-content`, then the grid stretches it into the leftover room.
 */
const SLACK_TRACK = 'minmax(0, auto)'

function metric(
  key: ColumnKey,
  label: string,
  sortField?: SortField,
  dropOrder?: Column['dropOrder'],
): Column {
  const sort = sortField ? { sortField } : {}
  const drop = dropOrder ? { dropOrder } : {}
  return { key, label, align: 'end', width: METRIC_WIDTH, min: METRIC_MIN, ...sort, ...drop }
}

function capitalized(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1)
}

/**
 * Room in rem for the vs prior header, such as `vs Feb 7 – Mar 9`, so it is never cut off: 0.40
 * rem per character of the small header font, which leaves a margin for wide glyphs, plus the
 * cell's padding, in quarter rems, and never narrower than the cells' 5.5 rem.
 */
function labelRem(label: string): number {
  return Math.max(5.5, Math.ceil(([...label].length * 0.4 + 1) * 4) / 4)
}

/**
 * The insights columns of a grouped view: Trend, and vs prior unless the scope is All, which
 * has no prior range. Neither is sortable.
 */
function insightColumns(compared: boolean, comparisonLabel: string): Column[] {
  const shared = { align: 'end', minWidth: 760, dropOrder: 2 } as const
  const trend: Column = { key: 'trend', label: 'Trend', width: '4.5rem', min: 4.5, ...shared }
  if (!compared) return [trend]
  const min = labelRem(comparisonLabel)
  return [trend, { key: 'vs_prior', label: comparisonLabel, width: `${min}rem`, min, ...shared }]
}

function groupedColumns(dimension: string): Column[] {
  const name: Column = {
    key: 'name',
    label: capitalized(dimension),
    sortField: dimension as SortField,
    align: 'start',
    width: TEXT_TRACK,
    text: true,
    slack: 1,
    min: 10,
  }
  const contextLabel = { merchant: 'Top category', category: 'Group' }[dimension]
  const context: Column[] = contextLabel
    ? [
        {
          key: 'context',
          label: contextLabel,
          align: 'start',
          width: TEXT_TRACK,
          text: true,
          slack: 2,
          minWidth: 760,
          min: 7,
          dropOrder: 1,
        },
      ]
    : []
  return [
    SELECT,
    name,
    ...context,
    metric('count', 'Count', 'count', 4),
    metric('in', 'In', undefined, 5),
    metric('out', 'Out', undefined, 5),
    metric('net', 'Net', 'amount'),
    metric('share', 'Share', undefined, 3),
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
      width: TEXT_TRACK,
      text: true,
      slack: 1,
      min: 8,
    },
    metric('count', 'Count', 'count', 3),
    metric('in', 'In', undefined, 4),
    metric('out', 'Out', undefined, 4),
    metric('net', 'Net', 'amount'),
    // The bar takes the room the columns leave, ahead of the short period labels.
    {
      key: 'net_bar',
      label: 'Net bar',
      align: 'start',
      width: 'minmax(8rem, 1fr)',
      min: 8,
      dropOrder: 1,
    },
    metric('savings', 'Savings rate', undefined, 2),
  ]
}

/**
 * The transaction columns. Merchant takes the room the other columns leave. A drill shows one
 * merchant, category, or account in every row, so that column keeps only what its label needs
 * and the next text column takes the room.
 */
function transactionColumns(projection: ViewProjection): Column[] {
  const amazon = projection.profile_kind === 'amazon'
  const drilled = new Set(drillFrames(projection).map((frame) => frame.dimension))
  const text = (key: 'merchant' | 'category' | 'account', slack: 1 | 2 | 3) =>
    ({
      key,
      sortField: key,
      align: 'start',
      width: TEXT_TRACK,
      text: true,
      ...(drilled.has(key) ? {} : { slack }),
    }) as const
  return [
    SELECT,
    {
      key: 'date',
      label: 'Date',
      sortField: 'date',
      align: 'start',
      width: '6rem',
      min: 6,
      dropOrder: 3,
    },
    { ...text('merchant', 1), label: amazon ? 'Product' : 'Merchant', min: 10 },
    { ...text('category', 2), label: 'Category', min: 8, dropOrder: 4 },
    { ...text('account', 3), label: amazon ? 'Order' : 'Account', min: 8, dropOrder: 1 },
    metric('amount', 'Amount', 'amount'),
    { key: 'flags', label: 'Flags', align: 'start', width: '6.5rem', min: 6.5, dropOrder: 2 },
  ]
}

/**
 * The grid's track size of each column shown. The shown text column with the lowest `slack`
 * takes the room the others leave; with none shown, a flexible track such as the Net bar's does.
 */
export function columnTracks(columns: readonly Column[]): string[] {
  let taker: Column | undefined
  for (const column of columns) {
    if (column.slack !== undefined && (!taker || column.slack < (taker.slack ?? 0))) taker = column
  }
  return columns.map((column) => (column === taker ? SLACK_TRACK : column.width))
}

/** The class list of a header or body cell in the column. */
export function cellClass(column: Column): string {
  return column.align === 'end' ? 'mf-table__cell mf-table__cell--end' : 'mf-table__cell'
}

/**
 * The window width in px that shows the columns with a `minWidth` of 760. The grid reads the
 * window's width, not a media query: a resize updates it before any media query listener runs,
 * so the columns are settled before the table leaves phone rows.
 */
export const WIDE_COLUMNS_WIDTH = 760

/** The columns that fit: a column with a `minWidth` drops out unless the viewport is `wide`. */
export function columnsAtWidth(columns: Column[], wide: boolean): Column[] {
  return columns.filter((column) => column.minWidth === undefined || wide)
}

function minimum(columns: readonly Column[]): number {
  return columns.reduce((total, column) => total + column.min, 0)
}

/** Room the columns must leave, because the measured width rounds to whole pixels. */
const FIT_TOLERANCE_REM = 0.25

/**
 * The columns whose minimum widths fit in `room` rem with `FIT_TOLERANCE_REM` to spare, so the
 * table never scrolls sideways. Columns drop by `dropOrder`, lowest first:
 * - grouped: Context, then Trend and vs prior, Share, Count, and In and Out;
 * - Time: the Net bar, then Savings rate, Count, and In and Out;
 * - transactions: Account, then Flags, Date, and Category.
 *
 * ☐, the name column, and Net or Amount always stay. Everything stays when `room` is unknown.
 */
export function columnsThatFit(columns: Column[], room: number | undefined): Column[] {
  if (room === undefined) return columns
  let shown = columns
  for (const order of [1, 2, 3, 4, 5] as const) {
    if (minimum(shown) + FIT_TOLERANCE_REM <= room) break
    shown = shown.filter((column) => column.dropOrder !== order)
  }
  return shown
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

/**
 * The columns of the projection's view. Only the server's sort fields stay sortable. The vs
 * prior header is `comparisonLabel`, such as `vs Aug`, once the insights name it. A label also
 * shows the scope has a comparison when the session has no date range, as in a Time drill.
 */
export function columnsFor(projection: ViewProjection, comparisonLabel?: string): Column[] {
  const view = projection.view
  const dimension = view.sub_grouping ?? view.grouping
  let columns: Column[]
  if (projection.detail_rows) columns = transactionColumns(projection)
  else if (dimension === 'time') columns = timeColumns()
  else {
    const compared = projection.filters.date_range !== undefined || !!comparisonLabel
    const label = comparisonLabel || 'vs prior'
    columns = [...groupedColumns(dimension), ...insightColumns(compared, label)]
  }
  const sortable = new Set<string>(view.sort_fields)
  return columns.map(({ sortField, ...column }) =>
    sortField && sortable.has(sortField) ? { ...column, sortField } : column,
  )
}
