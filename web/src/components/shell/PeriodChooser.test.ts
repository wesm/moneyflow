import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import PeriodChooser from './PeriodChooser.svelte'

function chooser(range = { from: '2026-04-01', to: '2026-04-30' }) {
  const anchor = document.createElement('button')
  document.body.append(anchor)
  const onpick = vi.fn()
  const ondismiss = vi.fn()
  // `anchor` is also a mount option, so the props go under `props`.
  render(PeriodChooser, {
    props: { anchor, range, first: '2024-01-01', latest: '2026-10-07', onpick, ondismiss },
  })
  return { anchor, onpick, ondismiss }
}

describe('PeriodChooser', () => {
  afterEach(() => {
    cleanup()
    document.body.replaceChildren()
  })

  it('opens on the scope kind with its segment focused', () => {
    chooser()
    const month = screen.getByRole('radio', { name: 'Month' })
    expect(month.getAttribute('aria-checked')).toBe('true')
    expect(document.activeElement).toBe(month)
    expect(screen.getByRole('button', { name: 'Apr 2026' }).getAttribute('aria-current')).toBe(
      'date',
    )
  })

  it('applies a custom range picked end first, in order', async () => {
    const { onpick } = chooser()
    await fireEvent.click(screen.getByRole('radio', { name: 'Custom' }))
    const apply = screen.getByRole('button', { name: 'Apply' })
    await fireEvent.click(screen.getByRole('button', { name: 'Apr 9, 2026' }))
    expect((apply as HTMLButtonElement).disabled).toBe(true)
    await fireEvent.click(screen.getByRole('button', { name: 'Previous month' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Mar 10, 2026' }))
    expect(screen.getByRole('dialog', { name: 'Choose period' }).textContent).toContain(
      'Mar 10, 2026',
    )
    await fireEvent.click(apply)
    expect(onpick).toHaveBeenCalledWith({ start: '2026-03-10', end: '2026-04-09' })
  })

  it('picks a month from another year and all time with a', async () => {
    const { onpick } = chooser()
    await fireEvent.click(screen.getByRole('button', { name: 'Previous year' }))
    expect((screen.getByRole('button', { name: 'Jan 2025' }) as HTMLButtonElement).disabled).toBe(
      false,
    )
    await fireEvent.click(screen.getByRole('button', { name: 'Feb 2025' }))
    expect(onpick).toHaveBeenLastCalledWith({ start: '2025-02-01', end: '2025-02-28' })
    await fireEvent.keyDown(screen.getByRole('radio', { name: 'Month' }), { key: 'a' })
    expect(onpick).toHaveBeenLastCalledWith(undefined)
  })

  it('moves between years with the arrows', async () => {
    chooser({ from: '2025-01-01', to: '2025-12-31' })
    const selected = screen.getByRole('button', { name: '2025' })
    expect(selected.tabIndex).toBe(0)
    await fireEvent.keyDown(selected, { key: 'ArrowRight' })
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '2026' }))
    expect(screen.getByRole('button', { name: '2026' }).tabIndex).toBe(0)
  })

  it('closes on Escape and returns focus to the label button', async () => {
    const { anchor, ondismiss } = chooser()
    await fireEvent.keyDown(screen.getByRole('radio', { name: 'Month' }), { key: 'Escape' })
    expect(ondismiss).toHaveBeenCalledTimes(1)
    expect(document.activeElement).toBe(anchor)
  })

  it('closes when focus leaves for the page but not when it moves inside', async () => {
    const { anchor, ondismiss } = chooser()
    const outside = document.createElement('button')
    document.body.append(outside)
    const month = screen.getByRole('radio', { name: 'Month' })
    await fireEvent.focusOut(month, { relatedTarget: screen.getByRole('radio', { name: 'Day' }) })
    await fireEvent.focusOut(month, { relatedTarget: anchor })
    expect(ondismiss).not.toHaveBeenCalled()
    await fireEvent.focusOut(month, { relatedTarget: outside })
    expect(ondismiss).toHaveBeenCalledTimes(1)
    await fireEvent.focusOut(anchor, { relatedTarget: outside })
    expect(ondismiss).toHaveBeenCalledTimes(2)
  })

  it("starts on Latest's month in a year the scope does not reach", async () => {
    chooser({ from: '2025-09-01', to: '2025-09-30' })
    await fireEvent.click(screen.getByRole('button', { name: 'Next year' }))
    expect(screen.getByRole('button', { name: 'Oct 2026' }).tabIndex).toBe(0)
    expect(screen.getByRole('button', { name: 'Jan 2026' }).tabIndex).toBe(-1)
  })
})
