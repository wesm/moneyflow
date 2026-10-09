import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import CommandSearch from './CommandSearch.svelte'
import type { SearchPreview } from '../../lib/controller/search'
import { testProjection } from '../../test/projection'

function host(search = '', valid = true) {
  const snapshot = { id: 'snapshot' }
  const held = vi.fn()
  return {
    held,
    projection: testProjection({ view: { ...testProjection().view, search } }),
    beginSearch: vi.fn(() => snapshot),
    previewSearch: vi.fn<(search: string) => Promise<SearchPreview>>(async () =>
      valid ? 'applied' : 'rejected',
    ),
    commitSearch: vi.fn(),
    restoreSearch: vi.fn(),
    hold<T>(work: Promise<T>): Promise<T> {
      held(work)
      return work
    },
  }
}

function field(): HTMLInputElement {
  return screen.getByRole('searchbox', { name: 'Search transactions' }) as HTMLInputElement
}

describe('CommandSearch', () => {
  afterEach(cleanup)

  it('shows the applied search and a / hint while empty', () => {
    render(CommandSearch, { controller: host(), ondone: vi.fn() })
    expect(field().value).toBe('')
    expect(field().closest('.kit-text-input')?.querySelector('kbd')?.textContent).toContain('/')
    cleanup()
    render(CommandSearch, { controller: host('noodle'), ondone: vi.fn() })
    expect(field().value).toBe('noodle')
  })

  it('previews while typing and applies on Enter', async () => {
    const controller = host()
    const ondone = vi.fn()
    render(CommandSearch, { controller, ondone })
    await fireEvent.input(field(), { target: { value: 'noodle' } })
    await waitFor(() => expect(controller.previewSearch).toHaveBeenCalledWith('noodle'))
    expect(controller.commitSearch).not.toHaveBeenCalled()
    await fireEvent.keyDown(field(), { key: 'Enter' })
    await waitFor(() => expect(controller.commitSearch).toHaveBeenCalledOnce())
    expect(ondone).toHaveBeenCalledOnce()
  })

  it('cancels the preview on one Esc and restores the applied search', async () => {
    const controller = host('noodle')
    const ondone = vi.fn()
    render(CommandSearch, { controller, ondone })
    await fireEvent.input(field(), { target: { value: 'pizza' } })
    await waitFor(() => expect(controller.previewSearch).toHaveBeenCalledWith('pizza'))
    await fireEvent.keyDown(field(), { key: 'Escape' })
    expect(controller.restoreSearch).toHaveBeenCalledOnce()
    expect(controller.previewSearch).not.toHaveBeenCalledWith('')
    expect(ondone).toHaveBeenCalledOnce()
    await waitFor(() => expect(field().value).toBe('noodle'))
  })

  it('returns to the grid on Esc when nothing was typed', async () => {
    const controller = host('noodle')
    const ondone = vi.fn()
    render(CommandSearch, { controller, ondone })
    await fireEvent.keyDown(field(), { key: 'Escape' })
    expect(controller.beginSearch).not.toHaveBeenCalled()
    expect(controller.restoreSearch).not.toHaveBeenCalled()
    expect(ondone).toHaveBeenCalledOnce()
    expect(field().value).toBe('noodle')
  })

  it('reports an invalid expression and keeps the field open', async () => {
    const controller = host('', false)
    const ondone = vi.fn()
    render(CommandSearch, { controller, ondone })
    await fireEvent.input(field(), { target: { value: '[' } })
    expect(await screen.findByText('That search expression is invalid.')).not.toBeNull()
    expect(field().getAttribute('aria-invalid')).toBe('true')
    await fireEvent.keyDown(field(), { key: 'Enter' })
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(controller.commitSearch).not.toHaveBeenCalled()
    expect(ondone).not.toHaveBeenCalled()
  })

  it('refuses a search over 2,048 bytes without a request', async () => {
    const controller = host()
    render(CommandSearch, { controller, ondone: vi.fn() })
    await fireEvent.input(field(), { target: { value: 'é'.repeat(1025) } })
    expect(await screen.findByText('Search text must be at most 2048 bytes.')).not.toBeNull()
    await new Promise((resolve) => setTimeout(resolve, 200))
    expect(controller.previewSearch).not.toHaveBeenCalled()
  })

  it('applies the preview when focus leaves the field, holding later transitions', async () => {
    const controller = host()
    render(CommandSearch, { controller, ondone: vi.fn() })
    field().focus()
    await fireEvent.input(field(), { target: { value: 'noodle' } })
    await fireEvent.focusOut(field(), { relatedTarget: document.body })
    expect(controller.held).toHaveBeenCalledOnce()
    await waitFor(() => expect(controller.commitSearch).toHaveBeenCalledOnce())
  })

  it('keeps the search open while the window itself loses focus', async () => {
    const controller = host()
    const focused = vi.spyOn(document, 'hasFocus').mockReturnValue(false)
    try {
      render(CommandSearch, { controller, ondone: vi.fn() })
      await fireEvent.input(field(), { target: { value: 'noodle' } })
      await fireEvent.focusOut(field(), { relatedTarget: null })
      expect(controller.held).not.toHaveBeenCalled()
      expect(controller.commitSearch).not.toHaveBeenCalled()
      expect(field().value).toBe('noodle')
    } finally {
      focused.mockRestore()
    }
  })

  it('keeps text typed while the search that lost focus is still being applied', async () => {
    const controller = host()
    let release!: (outcome: SearchPreview) => void
    controller.previewSearch.mockImplementationOnce(
      () => new Promise<SearchPreview>((resolve) => (release = resolve)),
    )
    render(CommandSearch, { controller, ondone: vi.fn() })
    field().focus()
    await fireEvent.input(field(), { target: { value: 'noodle' } })
    await fireEvent.focusOut(field(), { relatedTarget: document.body })
    await waitFor(() => expect(controller.previewSearch).toHaveBeenCalledWith('noodle'))
    await fireEvent.input(field(), { target: { value: 'pizza' } })
    release('applied')
    await waitFor(() => expect(controller.commitSearch).toHaveBeenCalledOnce())
    await waitFor(() => expect(controller.previewSearch).toHaveBeenCalledWith('pizza'))
    expect(controller.beginSearch).toHaveBeenCalledTimes(2)
    expect(field().value).toBe('pizza')
  })

  it('starts no new search for text typed during a held leave once the field lost focus', async () => {
    const controller = host()
    let release!: (outcome: SearchPreview) => void
    controller.previewSearch.mockImplementationOnce(
      () => new Promise<SearchPreview>((resolve) => (release = resolve)),
    )
    const elsewhere = document.body.appendChild(document.createElement('button'))
    render(CommandSearch, { controller, ondone: vi.fn() })
    field().focus()
    await fireEvent.input(field(), { target: { value: 'noodle' } })
    await fireEvent.focusOut(field(), { relatedTarget: document.body })
    await waitFor(() => expect(controller.previewSearch).toHaveBeenCalledWith('noodle'))
    await fireEvent.input(field(), { target: { value: 'pizza' } })
    elsewhere.focus()
    release('applied')
    await waitFor(() => expect(controller.commitSearch).toHaveBeenCalledOnce())
    await new Promise((resolve) => setTimeout(resolve, 200))

    expect(controller.beginSearch).toHaveBeenCalledOnce()
    expect(controller.previewSearch).not.toHaveBeenCalledWith('pizza')
    expect(field().value).toBe('')
    elsewhere.remove()
  })

  it('ends a search without applying it when browser history changes the view', async () => {
    const controller = host('noodle')
    const ondone = vi.fn()
    render(CommandSearch, { controller, ondone })
    await fireEvent.input(field(), { target: { value: 'pizza' } })
    await waitFor(() => expect(controller.previewSearch).toHaveBeenCalledWith('pizza'))
    window.dispatchEvent(new PopStateEvent('popstate'))
    await waitFor(() => expect(field().value).toBe('noodle'))
    await fireEvent.keyDown(field(), { key: 'Enter' })
    await fireEvent.keyDown(field(), { key: 'Escape' })
    expect(controller.commitSearch).not.toHaveBeenCalled()
    expect(controller.restoreSearch).not.toHaveBeenCalled()
  })

  it('drops an invalid search when focus leaves the field', async () => {
    const controller = host('', false)
    render(CommandSearch, { controller, ondone: vi.fn() })
    field().focus()
    await fireEvent.input(field(), { target: { value: '[' } })
    await fireEvent.focusOut(field(), { relatedTarget: document.body })
    await waitFor(() => expect(controller.restoreSearch).toHaveBeenCalledOnce())
    expect(controller.commitSearch).not.toHaveBeenCalled()
    await waitFor(() => expect(screen.queryByText('That search expression is invalid.')).toBeNull())
  })

  it('focuses and selects the field for the / key', async () => {
    const { component } = render(CommandSearch, { controller: host('noodle'), ondone: vi.fn() })
    component.focus()
    expect(document.activeElement).toBe(field())
    expect(field().selectionStart).toBe(0)
    expect(field().selectionEnd).toBe('noodle'.length)
  })
})
