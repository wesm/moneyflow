import { describe, expect, it } from 'vitest'

import { drillFrames } from './drills'
import { testProjection } from '../../test/projection'

const merchantDrill = 'drill=merchant%3AUSD%3A2%3Aexample-key'

describe('drillFrames', () => {
  it('is empty at the top level, where the one breadcrumb names the grouping', () => {
    const projection = testProjection({
      canonical_query: 'v=1',
      breadcrumbs: [{ dimension: 'merchant', label: 'Merchants' }],
    })
    expect(drillFrames(projection)).toEqual([])
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
