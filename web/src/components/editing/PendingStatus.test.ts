import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it } from 'vitest'
import PendingStatus from './PendingStatus.svelte'

describe('PendingStatus', () => {
  afterEach(cleanup)
  it('announces active, affected, and redo counts in text', () => {
    render(PendingStatus, {
      pending: { active_operations: 2, inactive_operations: 1, affected_transactions: 7 },
    })
    expect(screen.getByRole('status').textContent).toContain('2 pending · 7 transactions · 1 redo')
  })
})
