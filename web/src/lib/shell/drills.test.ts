import { describe, expect, it } from 'vitest'

import { drillFrames } from './drills'
import { testProjection } from '../../test/projection'
import type { ViewProjection } from '../api/client'

function atDepth(drill_depth: number, breadcrumbs: ViewProjection['breadcrumbs']) {
  const { view } = testProjection()
  return testProjection({ view: { ...view, drill_depth }, breadcrumbs })
}

describe('drillFrames', () => {
  it('is empty at the top level, where the one breadcrumb names the grouping', () => {
    const projection = atDepth(0, [{ dimension: 'merchant', label: 'Merchants' }])
    expect(drillFrames(projection)).toEqual([])
  })

  it('returns one frame per drill-down, outermost first', () => {
    const one = atDepth(1, [{ dimension: 'merchant', label: 'Corner Grocery' }])
    expect(drillFrames(one)).toEqual([{ dimension: 'merchant', label: 'Corner Grocery' }])
    const two = atDepth(2, [
      { dimension: 'merchant', label: 'Corner Grocery' },
      { dimension: 'category', label: 'Groceries' },
    ])
    expect(drillFrames(two)).toHaveLength(2)
  })

  it('is empty at depth 0 whatever the query says about earlier views', () => {
    const nested = encodeURIComponent('mode=detail&drill=merchant%3AUSD%3A2%3Aexample-key')
    const projection = testProjection({
      canonical_query: `v=1&return=navigation%3A${nested}`,
      breadcrumbs: [{ dimension: 'merchant', label: 'Merchants' }],
    })
    expect(projection.view.drill_depth).toBe(0)
    expect(drillFrames(projection)).toEqual([])
  })

  it('tolerates a projection without breadcrumbs', () => {
    expect(drillFrames(atDepth(1, null))).toEqual([])
  })
})
