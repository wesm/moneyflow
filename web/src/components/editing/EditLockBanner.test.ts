import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import EditLockBanner from './EditLockBanner.svelte'

describe('EditLockBanner', () => {
  afterEach(cleanup)

  it.each([
    ['writing', 'Writing to YNAB · Editing is locked until the write finishes'],
    ['reconciling', 'Writing to YNAB · Editing is locked until the write finishes'],
    ['rate_limited', 'Writing to YNAB · Editing is locked until the write finishes'],
    ['paused', 'YNAB write paused · Editing is locked until it resumes or is reconciled'],
    ['attention_required', 'YNAB write needs attention · Editing is locked'],
    ['reconcile_confirmation_required', 'YNAB write needs attention · Editing is locked'],
    ['reconnect_required', 'YNAB write needs attention · Editing is locked'],
  ])('words the %s phase as what people must do', (phase, text) => {
    render(EditLockBanner, { providerName: 'YNAB', phase, onstatus: vi.fn() })

    expect(screen.getByText(text)).toBeTruthy()
  })

  it('opens write status from its Status w button', async () => {
    const onstatus = vi.fn()
    render(EditLockBanner, { providerName: 'Monarch', phase: 'writing', onstatus })

    await fireEvent.click(screen.getByRole('button', { name: /^Status\s*w$/ }))

    expect(onstatus).toHaveBeenCalledTimes(1)
  })

  it('focuses Status before it opens, as WebKit does not', async () => {
    let focused: Element | null = null
    const onstatus = vi.fn(() => {
      focused = document.activeElement
    })
    render(EditLockBanner, { providerName: 'Monarch', phase: 'writing', onstatus })
    const status = screen.getByRole('button', { name: /^Status\s*w$/ })

    await fireEvent.click(status)

    expect(focused).toBe(status)
  })
})
