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
})
