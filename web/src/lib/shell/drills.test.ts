import { describe, expect, it } from 'vitest'

import { drillFrames } from './drills'
import { testProjection } from '../../test/projection'

const merchantDrill = 'drill=merchant%3AUSD%3A2%3Aexample-key'
const categoryDrill = 'drill=category%3AUSD%3A2%3Aexample-category'

describe('drillFrames', () => {
  it('is empty at the top level, where the one breadcrumb names the grouping', () => {
    const projection = testProjection({
      canonical_query: 'v=1',
      breadcrumbs: [{ dimension: 'merchant', label: 'Merchants' }],
    })
    expect(drillFrames(projection)).toEqual([])
  })

  it('returns one frame per drill-down, outermost first', () => {
    const one = testProjection({
      canonical_query: `v=1&${merchantDrill}`,
      breadcrumbs: [{ dimension: 'merchant', label: 'Corner Grocery' }],
    })
    expect(drillFrames(one)).toEqual([{ dimension: 'merchant', label: 'Corner Grocery' }])
    const two = testProjection({
      canonical_query: `v=1&${merchantDrill}&${categoryDrill}`,
      breadcrumbs: [
        { dimension: 'merchant', label: 'Corner Grocery' },
        { dimension: 'category', label: 'Groceries' },
      ],
    })
    expect(drillFrames(two)).toHaveLength(2)
  })

  it('ignores drill-downs inside a return frame', () => {
    const nested = encodeURIComponent(`mode=detail&${merchantDrill}`)
    const projection = testProjection({
      canonical_query: `v=1&return=navigation%3A${nested}`,
      breadcrumbs: [{ dimension: 'merchant', label: 'Merchants' }],
    })
    expect(drillFrames(projection)).toEqual([])
  })
})
