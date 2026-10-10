import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import RowActionsSheet from './RowActionsSheet.svelte'
import { OverlayState } from '../../lib/shell/overlay-state.svelte'
import { testCapabilities } from '../../test/capabilities'
import { testProjection } from '../../test/projection'

const ids = [
  'transaction.edit-merchant',
  'transaction.edit-category',
  'transaction.toggle-hidden',
  'transaction.delete',
  'transaction.show-info',
]

function sheet(unavailable: string[] = []) {
  const projection = testProjection({
    capabilities: testCapabilities(ids, unavailable).map((capability) =>
      capability.available ? capability : { ...capability, reason: 'Not while writing.' },
    ),
  })
  const events = { onaction: vi.fn(), onclose: vi.fn() }
  const overlays = new OverlayState({
    pushScope: () => undefined,
    focusGrid: () => {},
    grid: () => undefined,
  })
  const row = projection.detail_rows![0]!
  render(RowActionsSheet, { open: true, row, projection, overlays, ...events })
  return events
}

describe('RowActionsSheet', () => {
  afterEach(cleanup)

  it('names the row and runs each item as its key does', async () => {
    const { onaction } = sheet()
    const dialog = screen.getByRole('dialog', { name: 'Example Merchant' })
    expect(dialog.textContent).toContain('-$12.34')
    const items = ['Merchant', 'Category', 'Hide', 'Delete', 'Info']
    for (const name of items) await fireEvent.click(screen.getByRole('button', { name }))
    expect(onaction.mock.calls.map(([action]) => action)).toEqual([
      'edit.merchant',
      'edit.category',
      'edit.hide',
      'edit.delete',
      'transaction.info',
    ])
  })

  it('shows why Delete is unavailable and keeps the other items enabled', () => {
    sheet(['transaction.delete'])
    const remove = screen.getByRole('button', { name: 'Delete' }) as HTMLButtonElement
    expect(remove.disabled).toBe(true)
    const reason = document.getElementById(remove.getAttribute('aria-describedby')!)
    expect(reason?.textContent).toBe('Not while writing.')
    expect(reason?.checkVisibility?.() ?? true).toBe(true)
    const hide = screen.getByRole('button', { name: 'Hide' }) as HTMLButtonElement
    expect(hide.disabled).toBe(false)
  })

  it('closes on Escape', async () => {
    const { onclose } = sheet()
    await fireEvent.keyDown(screen.getByRole('button', { name: 'Info' }), { key: 'Escape' })
    expect(onclose).toHaveBeenCalledTimes(1)
  })
})
