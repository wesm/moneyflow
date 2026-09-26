import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ProviderSelector from './ProviderSelector.svelte'

describe('provider selector', () => {
  afterEach(cleanup)

  it('selects SimpleFIN with its experimental label', async () => {
    const onselect = vi.fn()
    render(ProviderSelector, { onselect, onback: vi.fn() })

    await fireEvent.keyDown(window, { key: 's' })
    expect(onselect).toHaveBeenCalledWith('simplefin')
    expect(screen.getByRole('button', { name: /SimpleFIN \(experimental\)/ })).toBeTruthy()
    await fireEvent.keyDown(window, { key: 'm' })
    expect(onselect).toHaveBeenCalledWith('monarch')
    await fireEvent.keyDown(window, { key: 'a' })
    expect(onselect).toHaveBeenCalledWith('amazon')
    await fireEvent.keyDown(window, { key: 'y' })
    expect(onselect).toHaveBeenCalledWith('ynab')
  })
})
