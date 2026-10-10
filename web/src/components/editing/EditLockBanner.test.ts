import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import EditLockBanner from './EditLockBanner.svelte'

describe('EditLockBanner', () => {
  afterEach(cleanup)

  it('names the provider and says editing stays locked until the write finishes', () => {
    render(EditLockBanner, { providerName: 'YNAB', onstatus: vi.fn() })

    expect(
      screen.getByText('Writing to YNAB · Editing is locked until the write finishes'),
    ).toBeTruthy()
  })

  it('opens write status from its Status w button', async () => {
    const onstatus = vi.fn()
    render(EditLockBanner, { providerName: 'Monarch', onstatus })

    const button = screen.getByRole('button', { name: /^Status\s*w$/ })
    await fireEvent.click(button)

    expect(onstatus).toHaveBeenCalledTimes(1)
  })
})
