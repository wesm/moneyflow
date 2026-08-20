import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ProviderSelector from './ProviderSelector.svelte'

describe('provider selector', () => {
  afterEach(cleanup)

  it('keeps unavailable providers focusable and explains their status', async () => {
    const onselect = vi.fn()
    render(ProviderSelector, { onselect, onback: vi.fn() })

    await fireEvent.keyDown(window, { key: 's' })
    expect(screen.getByRole('status').textContent).toContain(
      'SimpleFIN is not available in Go yet.',
    )
    await fireEvent.keyDown(window, { key: 'm' })
    expect(onselect).toHaveBeenCalledWith('monarch')
    await fireEvent.keyDown(window, { key: 'a' })
    expect(onselect).toHaveBeenCalledWith('amazon')
  })
})
