import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import CommandSearch from './CommandSearch.svelte'
import { SearchCarry } from '../../lib/shell/search-carry'
import CommandSearchSwap from '../../test/CommandSearchSwap.svelte'
import type { SearchPreview } from '../../lib/controller/search'
import { createViewController } from '../../lib/controller/view-controller.svelte'
import type { MoneyflowClient, TransitionBody } from '../../lib/api/client'
import { testProjection } from '../../test/projection'

function host(search = '', valid = true) {
  const snapshot = { id: 'snapshot' }
  const held = vi.fn()
  return {
    held,
    opening: false,
    projection: testProjection({ view: { ...testProjection().view, search } }),
    beginSearch: vi.fn(() => snapshot),
    previewSearch: vi.fn<(search: string) => Promise<SearchPreview>>(async () =>
      valid ? 'applied' : 'invalid',
    ),
    commitSearch: vi.fn(),
    restoreSearch: vi.fn(),
    hold<T>(work: Promise<T>): Promise<T> {
      held(work)
      return work
    },
    settled: async () => undefined,
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

  it('starts no search for text typed in a held leave after the field lost focus', async () => {
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

  it('drops a search still applying after focus left when browser history changes the view', async () => {
    const controller = host()
    let release!: (outcome: SearchPreview) => void
    controller.previewSearch.mockImplementationOnce(
      () => new Promise<SearchPreview>((resolve) => (release = resolve)),
    )
    render(CommandSearch, { controller, ondone: vi.fn() })
    field().focus()
    await fireEvent.input(field(), { target: { value: 'noodle' } })
    await waitFor(() => expect(controller.previewSearch).toHaveBeenCalledWith('noodle'))
    await fireEvent.focusOut(field(), { relatedTarget: document.body })
    window.dispatchEvent(new PopStateEvent('popstate'))
    release('superseded')
    await new Promise((resolve) => setTimeout(resolve, 200))

    expect(controller.previewSearch).toHaveBeenCalledOnce()
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

  it('continues the search being typed in the field that replaces it', async () => {
    const controller = host('noodle')
    const carry = new SearchCarry()
    const { rerender } = render(CommandSearchSwap, { controller, carry, wide: false })
    await fireEvent.input(field(), { target: { value: 'pizza' } })
    await waitFor(() => expect(controller.previewSearch).toHaveBeenCalledWith('pizza'))

    await rerender({ wide: true })

    expect(field().value).toBe('pizza')
    expect(carry.take()).toBeUndefined()
    expect(controller.restoreSearch).not.toHaveBeenCalled()
    await fireEvent.keyDown(field(), { key: 'Escape' })
    expect(controller.beginSearch).toHaveBeenCalledOnce()
    expect(controller.restoreSearch).toHaveBeenCalledWith({ id: 'snapshot' })
    expect(field().value).toBe('noodle')
  })

  it('gives focus to the replacing field when the search being typed had it', async () => {
    const controller = host()
    const carry = new SearchCarry()
    const { rerender } = render(CommandSearchSwap, { controller, carry, wide: true })
    field().focus()
    await fireEvent.input(field(), { target: { value: 'pizz' } })

    await rerender({ wide: false })

    await waitFor(() => expect(document.activeElement).toBe(field()))
    expect(field().value).toBe('pizz')
    expect(field().selectionStart).toBe(4)
    expect(field().selectionEnd).toBe(4)
    await fireEvent.keyDown(field(), { key: 'Enter' })
    await waitFor(() => expect(controller.commitSearch).toHaveBeenCalledOnce())
  })

  it('gives focus to the replacing field when nothing was typed yet', async () => {
    const controller = host('noodle')
    const carry = new SearchCarry()
    const { rerender } = render(CommandSearchSwap, { controller, carry, wide: true })
    field().focus()

    await rerender({ wide: false })

    await waitFor(() => expect(document.activeElement).toBe(field()))
    expect(field().value).toBe('noodle')
    expect(controller.beginSearch).not.toHaveBeenCalled()
    expect(carry.take()).toBeUndefined()
  })

  it('leaves focus alone when the replaced field did not have it', async () => {
    const controller = host()
    const carry = new SearchCarry()
    const { rerender } = render(CommandSearchSwap, { controller, carry, wide: true })
    await fireEvent.input(field(), { target: { value: 'pizz' } })

    await rerender({ wide: false })
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(field().value).toBe('pizz')
    expect(document.activeElement).not.toBe(field())
  })

  it('ends a search left in the carry when its owner is destroyed', async () => {
    const controller = host()
    const carry = new SearchCarry()
    const { unmount } = render(CommandSearch, { controller, ondone: vi.fn(), carry })
    await fireEvent.input(field(), { target: { value: 'pizza' } })
    unmount()
    carry.destroy()
    expect(carry.take()).toBeUndefined()
    await new Promise((resolve) => setTimeout(resolve, 200))
    expect(controller.previewSearch).not.toHaveBeenCalled()
  })

  it('takes no typing while the first load sets the currency', async () => {
    const { rerender } = render(CommandSearch, {
      controller: { ...host(), opening: true },
      ondone: vi.fn(),
    })
    expect(field().disabled).toBe(true)
    await rerender({ controller: host() })
    expect(field().disabled).toBe(false)
  })

  it('searches from the view a change still loading returns, without aborting it', async () => {
    history.replaceState(null, '', '/?v=1')
    let release!: () => void
    const transition = vi.fn(async (body: TransitionBody) => {
      if (body.action === 'search.apply') {
        return testProjection({ canonical_query: `${body.query}&q=${body.search}` })
      }
      await new Promise<void>((resolve) => (release = resolve))
      return testProjection({ canonical_query: `${body.query}&group=category` })
    })
    const client = { view: vi.fn(async () => testProjection()), transition, mutations: {} }
    const controller = createViewController({
      basePath: '/',
      client: client as unknown as MoneyflowClient,
      prefetch: false,
    })
    await controller.hydrate()
    render(CommandSearch, { controller, ondone: vi.fn() })

    const grouping = controller.apply({ action: 'view.cycle-grouping' })
    await fireEvent.input(field(), { target: { value: 'coffee' } })
    await new Promise((resolve) => setTimeout(resolve, 200))
    expect(transition.mock.calls.map(([body]) => body.action)).toEqual(['view.cycle-grouping'])
    release()
    await grouping

    await waitFor(() =>
      expect(controller.projection?.canonical_query).toBe('v=1&group=category&q=coffee'),
    )
  })

  it('applies text entered while an earlier view change loads once it finishes', async () => {
    let release!: () => void
    const controller = {
      ...host(),
      settled: () => new Promise<void>((resolve) => (release = resolve)),
    }
    const ondone = vi.fn()
    render(CommandSearch, { controller, ondone })
    await fireEvent.input(field(), { target: { value: 'noodle' } })
    await fireEvent.keyDown(field(), { key: 'Enter' })
    expect(controller.beginSearch).not.toHaveBeenCalled()
    release()
    await waitFor(() => expect(controller.commitSearch).toHaveBeenCalledOnce())
    expect(controller.previewSearch).toHaveBeenCalledWith('noodle')
    expect(ondone).toHaveBeenCalledOnce()
  })

  it('focuses and selects the field for the / key', async () => {
    const { component } = render(CommandSearch, { controller: host('noodle'), ondone: vi.fn() })
    component.focus()
    expect(document.activeElement).toBe(field())
    expect(field().selectionStart).toBe(0)
    expect(field().selectionEnd).toBe('noodle'.length)
  })
})
