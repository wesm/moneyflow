import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import WriteStatus from './WriteStatus.svelte'
import type { ProviderWriteStatus } from '../../lib/api/client'
import type { ProviderWriteState } from '../../lib/controller/provider-write'
import { testProviderWriteController } from '../../test/provider-write'

function setup(state: ProviderWriteState) {
  const controller = testProviderWriteController({ state, can: vi.fn(() => false) })
  render(WriteStatus, { controller, providerName: 'Monarch' })
}

function finished(overrides: Partial<ProviderWriteStatus> = {}): ProviderWriteStatus {
  return {
    version: '1',
    revision: '4',
    generation: '1',
    total: 0,
    completed: 0,
    failed: 0,
    remaining: 0,
    overrides: 0,
    actions: [],
    ...overrides,
  }
}

describe('WriteStatus', () => {
  afterEach(cleanup)

  it('keeps a completed write’s counts and shows when it finished', () => {
    const completedAt = '2026-10-15T12:00:00Z'
    setup({
      phase: 'complete',
      announcement: 'Provider write complete.',
      status: finished({ completed_at: completedAt, total: 3, completed: 3 }),
    })
    expect(screen.getByRole('status').textContent).toBe('Provider write complete.')
    expect(screen.getByText('3 of 3 complete')).toBeTruthy()
    expect(screen.getByText('Finished')).toBeTruthy()
    const time = document.querySelector('time')
    expect(time?.getAttribute('datetime')).toBe(completedAt)
    expect(time?.textContent).toBe(new Date(completedAt).toLocaleString())
    expect(screen.queryByText('Remaining')).toBeNull()
  })

  it('shows no empty progress after edits are discarded', () => {
    setup({
      phase: 'complete',
      announcement: 'Provider write stopped. Remaining edits discarded.',
      status: finished(),
    })
    expect(screen.getByRole('status').textContent).toBe(
      'Provider write stopped. Remaining edits discarded.',
    )
    expect(screen.queryByText('Progress')).toBeNull()
    expect(screen.queryByText('0 of 0 complete')).toBeNull()
  })
})
