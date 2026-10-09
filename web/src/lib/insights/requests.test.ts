import { describe, expect, it } from 'vitest'

import type { ViewProjection } from '../api/client'
import { testAggregateRow, testProjection } from '../../test/projection'
import { insightsBodies, missingRows, seriesRows } from './requests'

function grouped(
  grouping: string,
  identities: string[],
  offset = 0,
  sub_grouping?: string,
): ViewProjection {
  const base = testProjection()
  return testProjection({
    view: {
      ...base.view,
      mode: 'aggregate',
      grouping,
      ...(sub_grouping === undefined ? {} : { sub_grouping }),
    },
    detail_rows: null,
    aggregate_rows: identities.map((identity, index) =>
      testAggregateRow({ identity, index: offset + index }),
    ),
    total_rows: 600,
    window: { offset, limit: 200, count: identities.length },
  })
}

describe('seriesRows', () => {
  it('lists the loaded window and its cached neighbors once each', () => {
    const loaded = grouped('merchant', ['m-2', 'm-3'], 200)
    const neighbors = [grouped('merchant', ['m-0', 'm-1']), grouped('merchant', ['m-3', 'm-4'])]

    expect(seriesRows(loaded, neighbors)).toEqual(['m-2', 'm-3', 'm-0', 'm-1', 'm-4'])
  })

  it('charts the rows of a drill-down sub-grouping', () => {
    expect(seriesRows(grouped('merchant', ['c-1'], 0, 'category'), [])).toEqual(['c-1'])
  })

  it('requests no series in the Time grouping or a transaction view', () => {
    expect(seriesRows(grouped('time', ['t-1']), [])).toEqual([])
    expect(seriesRows(grouped('merchant', ['t-1'], 0, 'time'), [])).toEqual([])
    expect(seriesRows(testProjection(), [])).toEqual([])
  })
})

describe('insightsBodies', () => {
  const identities = (count: number, start = 0) =>
    Array.from({ length: count }, (_, index) => `row-${start + index}`)

  it('asks for the summary with the first 200 rows and the rest without it', () => {
    const bodies = insightsBodies('v=1', identities(450), true)

    expect(bodies.map((body) => [body.summary, body.rows?.length])).toEqual([
      [true, 200],
      [false, 200],
      [false, 50],
    ])
    expect(bodies[1]?.rows?.[0]).toBe('row-200')
    expect(bodies.every((body) => body.query === 'v=1')).toBe(true)
  })

  it('sends the summary alone when no row needs a series', () => {
    expect(insightsBodies('v=1', [], true)).toEqual([{ query: 'v=1', summary: true }])
  })

  it('sends nothing when neither the summary nor a series is missing', () => {
    expect(insightsBodies('v=1', [], false)).toEqual([])
  })

  it('splits series-only requests at 200 rows', () => {
    expect(insightsBodies('v=1', identities(200), false)).toEqual([
      { query: 'v=1', rows: identities(200), summary: false },
    ])
    expect(insightsBodies('v=1', identities(201), false)).toHaveLength(2)
  })
})

describe('missingRows', () => {
  it('skips loaded rows, rows on their way, and repeats', () => {
    const loaded = new Map([['a', true]])

    expect(missingRows(['a', 'b', 'c', 'd', 'd'], loaded, [['b'], ['c']])).toEqual(['d'])
    expect(missingRows(['a', 'b'], undefined, [])).toEqual(['a', 'b'])
  })
})
