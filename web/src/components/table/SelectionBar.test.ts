import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import SelectionBar from './SelectionBar.svelte'
import type { ViewProjection } from '../../lib/api/client'
import { testCapabilities } from '../../test/capabilities'
import { testMoney, testProjection } from '../../test/projection'

const editIDs = [
  'transaction.edit-merchant',
  'transaction.edit-category',
  'transaction.toggle-hidden',
  'transaction.delete',
]
const groupedReason = 'Open the group to delete its transactions.'
const writeReason = 'Finish the provider write before editing or refreshing.'

function projection(overrides: Partial<ViewProjection> = {}): ViewProjection {
  const net = testMoney('-421240', '-4,212.40')
  return testProjection({
    selection_count: 3,
    view: {
      mode: 'aggregate',
      grouping: 'merchant',
      time_granularity: 'year',
      sort_field: 'amount',
      sort_direction: 'desc',
      sort_fields: ['merchant', 'count', 'amount'],
      drill_depth: 0,
      root: { mode: 'aggregate', grouping: 'merchant', time_granularity: 'year' },
      currency: { currency: 'USD', scale: 2 },
    },
    selection_summary: [{ currency: 'USD', scale: 2, count: 127, in: net, out: net, net }],
    capabilities: testCapabilities(editIDs, ['transaction.delete']).map((capability) =>
      capability.available ? capability : { ...capability, reason: groupedReason },
    ),
    ...overrides,
  })
}

function bar() {
  return screen.getByRole('region', { name: 'Selection' })
}

describe('SelectionBar', () => {
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it('names the selection and offers each action with its key', () => {
    render(SelectionBar, { projection: projection(), onaction: vi.fn() })
    expect(bar().textContent).toContain('3 merchants · 127 transactions · -4,212.40')
    const buttons = within(bar()).getAllByRole('button')
    const names = ['Merchant m', 'Category c', 'Hide h', 'Delete x', 'Clear Esc']
    expect(buttons).toHaveLength(names.length)
    for (const [index, name] of names.entries()) {
      expect(within(bar()).getByRole('button', { name })).toBe(buttons[index])
    }
  })

  it('runs the action of each button', async () => {
    const onaction = vi.fn()
    const detail = projection({ capabilities: testCapabilities(editIDs) })
    render(SelectionBar, { projection: detail, onaction })
    for (const name of ['Merchant m', 'Category c', 'Hide h', 'Delete x', 'Clear Esc']) {
      await fireEvent.click(within(bar()).getByRole('button', { name }))
    }
    expect(onaction.mock.calls.map(([action]) => action)).toEqual([
      'edit.merchant',
      'edit.category',
      'edit.hide',
      'edit.delete',
      'clear',
    ])
  })

  it('disables Delete in a grouped view and explains why', async () => {
    vi.useFakeTimers()
    render(SelectionBar, { projection: projection(), onaction: vi.fn() })
    const remove = within(bar()).getByRole('button', { name: /^Delete(?: (?:x|d|Enter))?$/ })
    expect((remove as HTMLButtonElement).disabled).toBe(true)
    // Only the kit Tooltip explains it; a native title would show a second, unstyled tip.
    expect(remove.hasAttribute('title')).toBe(false)
    await fireEvent.mouseEnter(remove.closest('.kit-tooltip-trigger')!)
    await vi.advanceTimersByTimeAsync(300)
    expect(screen.getByRole('tooltip').textContent).toContain(groupedReason)
  })

  it('disables every edit while a provider write runs, but not Clear', async () => {
    vi.useFakeTimers()
    const capabilities = testCapabilities(editIDs, editIDs).map((capability) => ({
      ...capability,
      reason: writeReason,
    }))
    render(SelectionBar, { projection: projection({ capabilities }), onaction: vi.fn() })
    for (const name of ['Merchant m', 'Category c', 'Hide h', 'Delete x']) {
      const button = within(bar()).getByRole('button', { name }) as HTMLButtonElement
      expect(button.disabled).toBe(true)
      await fireEvent.mouseEnter(button.closest('.kit-tooltip-trigger')!)
      await vi.advanceTimersByTimeAsync(300)
      expect(screen.getByRole('tooltip').textContent).toContain(writeReason)
      await fireEvent.mouseLeave(button.closest('.kit-tooltip-trigger')!)
      await vi.advanceTimersByTimeAsync(300)
    }
    const clear = within(bar()).getByRole('button', {
      name: /^Clear(?: Esc)?$/,
    }) as HTMLButtonElement
    expect(clear.disabled).toBe(false)
  })
})
