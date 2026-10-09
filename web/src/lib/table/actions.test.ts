import { describe, expect, it } from 'vitest'

import { rowActionButtons } from './actions'
import type { ViewProjection } from '../api/client'
import { testCapabilities } from '../../test/capabilities'
import { testProjection } from '../../test/projection'

const editIDs = [
  'transaction.edit-merchant',
  'transaction.edit-category',
  'transaction.toggle-hidden',
  'transaction.delete',
]

function projection(overrides: Partial<ViewProjection> = {}): ViewProjection {
  return testProjection({ selection_count: 0, ...overrides })
}

describe('rowActionButtons', () => {
  it('offers the edits and Info for one row, with the keys that run them on the cursor row', () => {
    const ids = [...editIDs, 'transaction.show-info']
    const buttons = rowActionButtons(
      projection({ selection_count: 0, capabilities: testCapabilities(ids) }),
    )
    expect(buttons).toEqual([
      { action: 'edit.merchant', label: 'Merchant', key: 'm' },
      { action: 'edit.category', label: 'Category', key: 'c' },
      { action: 'edit.hide', label: 'Hide', key: 'h' },
      { action: 'edit.delete', label: 'Delete', key: 'x' },
      { action: 'transaction.info', label: 'Info', key: 'i' },
    ])
  })

  it('gives an unavailable Delete the server reason and leaves the rest enabled', () => {
    const reason = 'Deleting is not available here.'
    const ids = [...editIDs, 'transaction.show-info']
    const capabilities = testCapabilities(ids, ['transaction.delete']).map((capability) =>
      capability.available ? capability : { ...capability, reason },
    )
    const buttons = rowActionButtons(projection({ selection_count: 0, capabilities }))
    expect(buttons.map((button) => button.reason)).toEqual([
      undefined,
      undefined,
      undefined,
      reason,
      undefined,
    ])
  })

  it('disables the edits while a selection exists, since they would edit the selection', () => {
    const reason = 'Deleting is not available here.'
    const ids = [...editIDs, 'transaction.show-info']
    const capabilities = testCapabilities(ids, ['transaction.delete']).map((capability) =>
      capability.available ? capability : { ...capability, reason },
    )
    const buttons = rowActionButtons(projection({ selection_count: 1, capabilities }))
    const selected = 'Clear the selection to edit one transaction'
    expect(buttons.map((button) => button.reason)).toEqual([
      selected,
      selected,
      selected,
      reason,
      undefined,
    ])
  })
})
