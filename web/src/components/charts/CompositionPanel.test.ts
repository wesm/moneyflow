import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import CompositionPanel from './CompositionPanel.svelte'
import type { Composition } from '../../lib/charts/composition'
import { RevealHold, revealHoldKey } from '../../lib/table/reveal-hold.svelte'

const composition: Composition = {
  spending: [
    { key: 'k1', identity: 'merchant:rent', label: 'Rent', net: '-200000', share_tenths: 400 },
    {
      key: 'k2',
      identity: 'merchant:mega',
      label: 'Online Megastore',
      net: '-36000',
      share_tenths: 62,
    },
    { label: '5 others', net: '-100000', share_tenths: 200, others: 5 },
  ],
  income: [],
}

function setup(props: Partial<{ cursorIdentity: string; onleave: () => void }> = {}) {
  const onrow = vi.fn()
  const ondrill = vi.fn()
  render(CompositionPanel, {
    title: 'Where it went',
    composition,
    scale: 2,
    onrow,
    ondrill,
    ...props,
  })
  const list = screen.getByRole('list', { name: 'Where it went' })
  return { onrow, ondrill, list }
}

describe('CompositionPanel', () => {
  afterEach(() => cleanup())

  it('lists spending by net share, naming each bar in visible label order', () => {
    const { list } = setup()
    const items = within(list).getAllByRole('listitem')
    expect(items.map((item) => item.textContent?.replace(/\s+/g, ' ').trim())).toEqual([
      'Rent -2,000.00 40.0%',
      'Online Megastore -360.00 6.2%',
      '5 others -1,000.00 20.0%',
    ])
    expect(
      within(list).getByRole('button', { name: 'Online Megastore, -360.00, 6.2%' }),
    ).not.toBeNull()
  })

  it('moves the cursor on click and drills on double-click or Enter', async () => {
    const { list, onrow, ondrill } = setup()
    const bar = within(list).getByRole('button', { name: /^Online Megastore,/ })
    await fireEvent.click(bar)
    expect(onrow).toHaveBeenCalledWith('merchant:mega')
    await fireEvent.dblClick(bar)
    expect(ondrill).toHaveBeenCalledWith('merchant:mega')
    ondrill.mockClear()
    onrow.mockClear()
    await fireEvent.keyDown(bar, { key: 'Enter' })
    expect(ondrill).toHaveBeenCalledWith('merchant:mega')
    expect(onrow).not.toHaveBeenCalled()
  })

  it('moves the cursor at once on a pointer click and holds only the scroll to its row', async () => {
    vi.useFakeTimers()
    try {
      const hold = new RevealHold()
      const onrow = vi.fn()
      const ondrill = vi.fn()
      render(CompositionPanel, {
        props: { title: 'Where it went', composition, scale: 2, onrow, ondrill },
        context: new Map([[revealHoldKey, hold]]),
      })
      const bar = screen.getByRole('button', { name: /^Online Megastore,/ })
      const reveal = vi.fn()

      await fireEvent.click(bar, { detail: 1 })
      expect(onrow).toHaveBeenCalledWith('merchant:mega')
      expect(hold.held).toBe(true)
      hold.defer(reveal)
      vi.advanceTimersByTime(400)
      expect(hold.held).toBe(false)
      expect(reveal).toHaveBeenCalledTimes(1)

      // The second click of a double-click drops the held scroll; the double-click drills.
      reveal.mockClear()
      await fireEvent.click(bar, { detail: 1 })
      hold.defer(reveal)
      await fireEvent.click(bar, { detail: 2 })
      await fireEvent.dblClick(bar, { detail: 2 })
      vi.advanceTimersByTime(1000)
      expect(ondrill).toHaveBeenCalledWith('merchant:mega')
      expect(reveal).not.toHaveBeenCalled()

      // Space clicks with no count, so its row is revealed at once.
      await fireEvent.click(bar, { detail: 0 })
      expect(hold.held).toBe(false)
    } finally {
      vi.useRealTimers()
    }
  })

  it('keeps the others bar out of reach and draws it apart from the ranked rows', () => {
    const { list } = setup()
    expect(within(list).queryByRole('button', { name: /^5 others/ })).toBeNull()
    expect(within(list).getAllByRole('button')).toHaveLength(2)
    const others = within(list).getByText('5 others').closest('.mf-bar')
    expect(others?.classList.contains('mf-bar--others')).toBe(true)
  })

  it('leaves for the table on Esc from the Spending/Income switch', async () => {
    const onleave = vi.fn()
    setup({ onleave })
    const spending = screen.getByRole('radio', { name: 'Spending' })
    const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    spending.dispatchEvent(event)
    expect(onleave).toHaveBeenCalledTimes(1)
    // Claimed, so the shell's Esc does not also go back a level.
    expect(event.defaultPrevented).toBe(true)
  })

  it('draws the cursor row’s bar solid and makes it the Tab stop', () => {
    const { list } = setup({ cursorIdentity: 'merchant:mega' })
    const bar = within(list).getByRole('button', { name: /^Online Megastore,/ })
    expect(bar.classList.contains('mf-bar--cursor')).toBe(true)
    expect(bar.getAttribute('tabindex')).toBe('0')
    const rent = within(list).getByRole('button', { name: /^Rent,/ })
    expect(rent.classList.contains('mf-bar--cursor')).toBe(false)
    expect(rent.getAttribute('tabindex')).toBe('-1')
  })

  it('moves between bars with the arrows and leaves with Esc', async () => {
    const onleave = vi.fn()
    const { list } = setup({ onleave })
    const [rent, mega] = within(list).getAllByRole('button')
    expect(rent?.getAttribute('tabindex')).toBe('0')
    rent?.focus()
    await fireEvent.keyDown(rent!, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(mega)
    expect(mega?.getAttribute('tabindex')).toBe('0')
    await fireEvent.keyDown(mega!, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(mega)
    await fireEvent.keyDown(mega!, { key: 'Escape' })
    expect(onleave).toHaveBeenCalled()
  })

  it('jumps to the last bar with End, steps back with ArrowUp, and returns with Home', async () => {
    const three: Composition = {
      spending: [
        ...composition.spending.slice(0, 2),
        { key: 'k3', identity: 'merchant:cafe', label: 'Cafe', net: '-1200', share_tenths: 2 },
      ],
      income: [],
    }
    render(CompositionPanel, {
      title: 'Where it went',
      composition: three,
      scale: 2,
      onrow: vi.fn(),
    })
    const list = screen.getByRole('list', { name: 'Where it went' })
    const [rent, mega, cafe] = within(list).getAllByRole('button')
    rent?.focus()
    await fireEvent.keyDown(rent!, { key: 'End' })
    expect(document.activeElement).toBe(cafe)
    await fireEvent.keyDown(cafe!, { key: 'ArrowUp' })
    expect(document.activeElement).toBe(mega)
    await fireEvent.keyDown(mega!, { key: 'Home' })
    expect(document.activeElement).toBe(rent)
    expect(rent?.getAttribute('tabindex')).toBe('0')
  })

  it('switches to Income without asking anything, and shows the empty state there', async () => {
    const { onrow } = setup()
    await fireEvent.click(screen.getByRole('radio', { name: 'Income' }))
    expect(screen.getByText('No income')).not.toBeNull()
    expect(screen.queryByRole('list', { name: 'Where it went' })).toBeNull()
    expect(onrow).not.toHaveBeenCalled()
  })

  it('draws bars that name no table row as plain items', () => {
    render(CompositionPanel, { title: 'Sep 2026 by group', composition, scale: 2 })
    const list = screen.getByRole('list', { name: 'Sep 2026 by group' })
    expect(within(list).queryAllByRole('button')).toHaveLength(0)
    expect(within(list).getAllByRole('listitem')).toHaveLength(3)
  })
})
