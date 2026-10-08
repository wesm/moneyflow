import { describe, expect, it } from 'vitest'

import { partitionChartMarks, validateChartPartitions, type ChartPartition } from './chart'
import type { ViewProjection } from './api/client'

type Mark = NonNullable<ViewProjection['chart']['marks']>[number]

describe('chart projection adapter', () => {
  it.each([
    { minor: '-8200', scale: 2, tick: -5000, expected: '−41.00' },
    { minor: '8200', scale: 2, tick: 10000, expected: '82.00' },
    { minor: '-123', scale: 2, tick: -5000, expected: '−0.62' },
    { minor: '-2000', scale: 0, tick: -5000, expected: '−1k' },
    { minor: '-12345', scale: 3, tick: -10000, expected: '−12.345' },
    { minor: '-123456789', scale: 2, tick: -10000, expected: '−1.23M' },
    { minor: '-9223372036854775808', scale: 2, tick: -10000, expected: '−92233.72T' },
    { minor: '0', scale: 2, tick: 0, expected: '0.00' },
  ])(
    'formats monetary axis ticks for $minor at scale $scale',
    ({ minor, scale, tick, expected }) => {
      const datum = mark('amount', 'Example', 'USD', scale, 'Exact server label', -10000)
      datum.amount.minor = minor
      const partition = partitionChartMarks([datum], new Map(), false)[0]!
      expect(partition.formatTick(tick)).toBe(expected)
      expect(partition.marks[0]?.display).toBe('Exact server label')
    },
  )

  it('preserves partitions, ordinary row order, exact labels, ratios, and identities', () => {
    const marks = [
      mark('usd-2', 'Second', 'USD', 2, '-$2.00', -5000),
      mark('eur', 'Euro', 'EUR', 2, '-€1.00', -10000),
      mark('usd-1', 'First', 'USD', 2, '-$4.00', -10000),
    ]
    const partitions = partitionChartMarks(marks, new Map(), false)

    expect(partitions.map((partition) => partition.key)).toEqual(['USD:2', 'EUR:2'])
    expect(partitions[0]?.marks.map((item) => item.identity)).toEqual(['usd-2', 'usd-1'])
    expect(partitions[0]?.marks[0]).toMatchObject({
      categoricalKey: 'usd-2',
      label: 'Second',
      display: '-$2.00',
      ratio: -5000,
    })
    expect(Object.isFrozen(partitions[0]?.marks[0])).toBe(true)
  })

  it('sorts time marks chronologically from typed server periods', () => {
    const periods = new Map([
      ['feb', { granularity: 'month', year: 2026, month: 2 }],
      ['jan', { granularity: 'month', year: 2026, month: 1 }],
    ])
    const partitions = partitionChartMarks(
      [
        mark('feb', 'Feb 2026', 'USD', 2, '-$2.00', -2000),
        mark('jan', 'Jan 2026', 'USD', 2, '-$1.00', -1000),
      ],
      periods,
      true,
    )
    expect(partitions[0]?.marks.map((item) => item.identity)).toEqual(['jan', 'feb'])
  })

  it('rejects duplicate identities, missing time periods, and mismatched partitions', () => {
    const duplicate = mark('same', 'One', 'USD', 2, '-$1.00', -1000)
    expect(() => partitionChartMarks([duplicate, { ...duplicate }], new Map(), false)).toThrow(
      'duplicate chart identity',
    )
    expect(() => partitionChartMarks([duplicate], new Map(), true)).toThrow('typed period')

    const invalid: ChartPartition[] = [
      {
        key: 'USD:2',
        currency: 'USD',
        scale: 2,
        formatTick: () => '',
        domain: [-10000, 0],
        marks: [
          {
            identity: 'eur',
            categoricalKey: 'eur',
            index: 0,
            label: 'Euro',
            display: '-€1.00',
            ratio: -1000,
            currency: 'EUR',
            scale: 2,
          },
        ],
      },
    ]
    expect(() => validateChartPartitions(invalid)).toThrow('mismatched money partition')
  })
})

function mark(
  identity: string,
  label: string,
  currency: string,
  scale: number,
  display: string,
  plotRatio: number,
): Mark {
  return {
    identity,
    index: 0,
    label,
    amount: { minor: '-100', decimal: '-1.00', currency, scale, display },
    plot_ratio: plotRatio,
  }
}
