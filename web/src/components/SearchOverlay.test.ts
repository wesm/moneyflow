import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import SearchOverlay from './SearchOverlay.svelte'
import type { ViewController } from '../lib/controller/view-controller.svelte'
import { testProjection } from '../test/projection'

describe('SearchOverlay', () => {
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it('focuses search, previews text, commits on Enter, and closes', async () => {
    vi.useFakeTimers()
    const controller = searchController()
    const onclose = vi.fn()
    render(SearchOverlay, { controller, onclose })
    const input = screen.getByRole('searchbox', { name: 'Search transactions' })
    expect(document.activeElement).toBe(input)
    await fireEvent.input(input, { target: { value: 'coffee' } })
    await vi.advanceTimersByTimeAsync(150)
    expect(controller.previewSearch).toHaveBeenCalledWith('coffee')
    await fireEvent.keyDown(input, { key: 'Enter' })
    await vi.waitFor(() => expect(controller.commitSearch).toHaveBeenCalledTimes(1))
    expect(onclose).toHaveBeenCalledTimes(1)
  })

  it('cancels the whole overlay on Escape even when the input has text', async () => {
    const controller = searchController()
    const onclose = vi.fn()
    render(SearchOverlay, { controller, onclose })
    const input = screen.getByRole('searchbox', { name: 'Search transactions' })
    await fireEvent.input(input, { target: { value: 'temporary' } })
    await fireEvent.keyDown(input, { key: 'Escape' })
    expect(controller.restoreSearch).toHaveBeenCalledTimes(1)
    expect(onclose).toHaveBeenCalledTimes(1)
  })
})

function searchController(): ViewController {
  const snapshot = {
    projection: testProjection(),
    history: {
      owner: 'moneyflow-web-v1' as const,
      instance: 'test',
      sequence: 0,
      query: 'v=1',
      cursorIndex: 0,
      scrollTop: 0,
      selection: testProjection().selection,
    },
    cursorIdentity: 'row-0',
    cursorIndex: 0,
    scrollTop: 0,
  }
  return {
    projection: testProjection(),
    beginSearch: vi.fn(() => snapshot),
    previewSearch: vi.fn(async () => true),
    commitSearch: vi.fn(),
    restoreSearch: vi.fn(),
  } as unknown as ViewController
}
