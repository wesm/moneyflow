import type { ViewProjection } from './api/client'

type ServerMark = NonNullable<ViewProjection['chart']['marks']>[number]
type AggregateRow = NonNullable<ViewProjection['aggregate_rows']>[number]
export type ServerPeriod = NonNullable<AggregateRow['period']>

export interface ChartMark {
  readonly identity: string
  readonly categoricalKey: string
  readonly index: number
  readonly label: string
  readonly display: string
  readonly ratio: number
  readonly currency: string
  readonly scale: number
  readonly chronologicalKey?: string
}

export interface ChartPartition {
  readonly key: string
  readonly currency: string
  readonly scale: number
  readonly marks: readonly ChartMark[]
}

export function partitionChartMarks(
  marks: readonly ServerMark[],
  periods: ReadonlyMap<string, ServerPeriod>,
  chronological: boolean,
): readonly ChartPartition[] {
  const identities = new Set<string>()
  const partitions = new Map<string, { currency: string; scale: number; marks: ChartMark[] }>()
  for (const mark of marks) {
    if (identities.has(mark.identity)) throw new Error('duplicate chart identity')
    identities.add(mark.identity)
    if (!Number.isInteger(mark.plot_ratio) || Math.abs(mark.plot_ratio) > 10_000) {
      throw new Error('invalid chart plot ratio')
    }
    const period = periods.get(mark.identity)
    if (chronological && period === undefined)
      throw new Error('time chart mark lacks a typed period')
    const key = `${mark.amount.currency}:${mark.amount.scale}`
    let partition = partitions.get(key)
    if (!partition) {
      partition = { currency: mark.amount.currency, scale: mark.amount.scale, marks: [] }
      partitions.set(key, partition)
    }
    partition.marks.push(
      Object.freeze({
        identity: mark.identity,
        categoricalKey: mark.identity,
        index: mark.index,
        label: mark.label,
        display: mark.amount.display,
        ratio: mark.plot_ratio,
        currency: mark.amount.currency,
        scale: mark.amount.scale,
        ...(period === undefined ? {} : { chronologicalKey: periodKey(period) }),
      }),
    )
  }
  const result = [...partitions].map(([key, partition]) => {
    const ordered = chronological
      ? [...partition.marks].sort((left, right) =>
          (left.chronologicalKey ?? '').localeCompare(right.chronologicalKey ?? ''),
        )
      : partition.marks
    return Object.freeze({
      key,
      currency: partition.currency,
      scale: partition.scale,
      marks: Object.freeze(ordered),
    })
  })
  validateChartPartitions(result)
  return Object.freeze(result)
}

export function validateChartPartitions(partitions: readonly ChartPartition[]): void {
  const identities = new Set<string>()
  for (const partition of partitions) {
    if (partition.key !== `${partition.currency}:${partition.scale}`) {
      throw new Error('invalid money partition key')
    }
    for (const mark of partition.marks) {
      if (mark.currency !== partition.currency || mark.scale !== partition.scale) {
        throw new Error('chart datum has a mismatched money partition')
      }
      if (identities.has(mark.identity)) throw new Error('duplicate chart identity')
      identities.add(mark.identity)
    }
  }
}

function periodKey(period: ServerPeriod): string {
  const month = period.month ?? 0
  const day = period.day ?? 0
  if (!Number.isInteger(period.year) || !Number.isInteger(month) || !Number.isInteger(day)) {
    throw new Error('invalid typed chart period')
  }
  return `${String(period.year).padStart(4, '0')}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`
}
