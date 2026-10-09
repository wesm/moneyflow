import { describe, expect, it } from 'vitest'

import { availableCapabilities, unavailableReason } from './capabilities'
import { testCapabilities } from '../../test/capabilities'
import { testProjection } from '../../test/projection'

describe('availableCapabilities', () => {
  it('lists only the capabilities the view offers', () => {
    const projection = testProjection({
      capabilities: testCapabilities(['view.drill', 'overlay.help'], ['overlay.help']),
    })
    expect([...availableCapabilities(projection)]).toEqual(['view.drill'])
  })

  it('treats a projection without capabilities as offering none', () => {
    expect(availableCapabilities(testProjection({ capabilities: null })).size).toBe(0)
  })
})

describe('unavailableReason', () => {
  it("uses the server's reason when it gives one", () => {
    const projection = testProjection({
      capabilities: [
        {
          id: 'transactions.export',
          description: 'Export',
          category: 'Actions',
          available: false,
          reason: 'Commit pending edits before exporting.',
        },
      ],
    })
    expect(unavailableReason(projection, 'transactions.export')).toBe(
      'Commit pending edits before exporting.',
    )
  })

  it('falls back to a generic reason for a missing or unexplained capability', () => {
    const projection = testProjection({
      capabilities: testCapabilities(['view.drill'], ['view.drill']),
    })
    expect(unavailableReason(projection, 'view.drill')).toBe('Not available in this view')
    expect(unavailableReason(projection, 'view.find-duplicates')).toBe('Not available in this view')
  })
})
