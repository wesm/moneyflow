import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ManagerDrawer from './ManagerDrawer.svelte'
import type { EditingController } from '../../lib/controller/editing'
import type { ManagerKind } from '../../lib/editing/manager'
import { OverlayState } from '../../lib/shell/overlay-state.svelte'
import { testEditingController } from '../../test/editing'

function setup(options: { kind?: ManagerKind; controller?: EditingController } = {}) {
  const controller = options.controller ?? testEditingController()
  const overlays = new OverlayState({
    pushScope: () => undefined,
    focusGrid: vi.fn(),
    grid: () => undefined,
  })
  const onclose = vi.fn()
  render(ManagerDrawer, { kind: options.kind ?? 'category', controller, overlays, onclose })
  return { controller, onclose }
}

/** Waits for the catalog, then returns the list with focus on it. */
async function list(name = 'Categories'): Promise<HTMLElement> {
  const element = screen.getByRole('listbox', { name })
  await waitFor(() => expect(element.querySelectorAll('[role="option"]').length).toBeGreaterThan(0))
  element.focus()
  return element
}

/** A promise the test settles, standing in for a staging request in flight. */
function deferred() {
  let settle: (accepted: boolean) => void = () => undefined
  const promise = new Promise<boolean>((resolve) => (settle = resolve))
  return { promise, settle }
}

describe('ManagerDrawer', () => {
  beforeEach(() => {
    // jsdom has no layout; the destination list scrolls its active option into view.
    Element.prototype.scrollIntoView ??= () => undefined
  })
  afterEach(cleanup)

  it('opens on the filter and lists each category with its group and count', async () => {
    setup()
    expect(screen.getByRole('dialog', { name: 'Manage categories' })).not.toBeNull()
    expect(document.activeElement).toBe(screen.getByLabelText('Filter categories'))
    await list()
    expect(
      screen.getByRole('option', { name: 'Example Category, Example Group, 4 transactions' }),
    ).not.toBeNull()
    expect(
      screen.getByRole('option', { name: 'Uncategorized, protected, 0 transactions' }),
    ).not.toBeNull()
  })

  it('stages a new category from the toolbar and reloads the list', async () => {
    const { controller } = setup()
    await list()
    await fireEvent.click(screen.getByRole('button', { name: 'New' }))
    const name = screen.getByRole('textbox', { name: 'Name' })
    expect(document.activeElement).toBe(name)
    expect(screen.getByRole('button', { name: 'Create' })).toHaveProperty('disabled', true)
    await fireEvent.input(name, { target: { value: 'New Category' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(screen.queryByRole('textbox', { name: 'Name' })).toBeNull())
    expect(controller.submit).toHaveBeenCalledWith({
      action: 'category.manage',
      input: expect.objectContaining({
        taxonomy: 'create',
        entity_id: expect.stringMatching(/^category_/),
        label: 'New Category',
        group_id: 'group-a',
      }),
    })
    expect(controller.catalog).toHaveBeenCalledTimes(2)
  })

  it('starts steps from n, r, m, and d on the list but not from letters typed in the filter', async () => {
    setup()
    const options = await list()
    const filter = screen.getByLabelText('Filter categories')
    await fireEvent.keyDown(filter, { key: 'n' })
    expect(screen.queryByRole('heading', { name: 'New category' })).toBeNull()
    await fireEvent.keyDown(options, { key: 'ArrowDown' })
    await fireEvent.keyDown(options, { key: 'r' })
    expect(screen.getByRole('heading', { name: 'Rename Example Category' })).not.toBeNull()
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveProperty(
      'value',
      'Example Category',
    )
  })

  it('starts nothing on a protected category', async () => {
    setup()
    const options = await list()
    for (const key of ['r', 'g', 'm', 'd']) await fireEvent.keyDown(options, { key })
    expect(screen.queryByRole('heading', { level: 2 })).toBeNull()
    expect(screen.getByRole('button', { name: 'Rename' })).toHaveProperty('disabled', true)
  })

  it('steps back with Esc, then clears the filter, then closes', async () => {
    const { onclose } = setup()
    const options = await list()
    await fireEvent.keyDown(options, { key: 'n' })
    const name = screen.getByRole('textbox', { name: 'Name' })
    await fireEvent.keyDown(name, { key: 'Escape' })
    expect(screen.queryByRole('textbox', { name: 'Name' })).toBeNull()
    await waitFor(() => expect(document.activeElement).toBe(options))
    const filter = screen.getByLabelText('Filter categories')
    await fireEvent.input(filter, { target: { value: 'Exam' } })
    await fireEvent.keyDown(options, { key: 'Escape' })
    expect(filter).toHaveProperty('value', '')
    expect(onclose).not.toHaveBeenCalled()
    await fireEvent.keyDown(options, { key: 'Escape' })
    expect(onclose).toHaveBeenCalledOnce()
  })

  it('confirms with Enter once, ignoring held and in-flight Enters', async () => {
    const pending = deferred()
    const controller = testEditingController({ submit: vi.fn(() => pending.promise) })
    setup({ controller })
    const options = await list()
    await fireEvent.keyDown(options, { key: 'n' })
    const name = screen.getByRole('textbox', { name: 'Name' })
    await fireEvent.input(name, { target: { value: 'New Category' } })
    await fireEvent.keyDown(name, { key: 'Enter', repeat: true })
    await fireEvent.keyDown(name, { key: 'Enter', ctrlKey: true })
    expect(controller.submit).not.toHaveBeenCalled()
    await fireEvent.keyDown(name, { key: 'Enter' })
    await fireEvent.keyDown(name, { key: 'Enter' })
    expect(controller.submit).toHaveBeenCalledOnce()
    pending.settle(true)
    await waitFor(() => expect(screen.queryByRole('textbox', { name: 'Name' })).toBeNull())
  })

  it('keeps the step open with the reason the server gave', async () => {
    const controller = testEditingController({ submit: vi.fn(async () => false) })
    controller.state.announcement = 'That action is not available for the current target.'
    setup({ controller })
    const options = await list()
    await fireEvent.keyDown(options, { key: 'n' })
    const name = screen.getByRole('textbox', { name: 'Name' })
    await fireEvent.input(name, { target: { value: 'Example Category' } })
    await fireEvent.keyDown(name, { key: 'Enter' })
    await waitFor(() =>
      expect(screen.getByRole('alert').textContent).toBe(
        'That action is not available for the current target.',
      ),
    )
    expect(screen.getByRole('textbox', { name: 'Name' })).not.toBeNull()
  })

  it('needs a chosen destination before a merge can stage', async () => {
    const { controller } = setup()
    const options = await list()
    await fireEvent.keyDown(options, { key: 'ArrowDown' })
    await fireEvent.keyDown(options, { key: 'm' })
    const merge = screen.getByRole('button', { name: 'Merge' })
    expect(merge).toHaveProperty('disabled', true)
    const destination = screen.getByRole('combobox', { name: 'Destination' })
    await fireEvent.input(destination, { target: { value: 'uncategorized' } })
    await waitFor(() => expect(merge).toHaveProperty('disabled', false))
    await fireEvent.click(merge)
    expect(controller.submit).toHaveBeenCalledWith({
      action: 'category.manage',
      input: {
        taxonomy: 'merge',
        entity_id: 'category-a',
        destination_id: 'category-uncategorized',
      },
    })
  })

  it('manages groups without a move action', async () => {
    const { controller } = setup({ kind: 'group' })
    expect(screen.getByRole('dialog', { name: 'Manage category groups' })).not.toBeNull()
    const options = await list('Category groups')
    expect(screen.queryByRole('button', { name: 'Move to group' })).toBeNull()
    await fireEvent.keyDown(options, { key: 'g' })
    expect(screen.queryByRole('heading', { level: 2 })).toBeNull()
    await fireEvent.keyDown(options, { key: 'd' })
    expect(screen.getByRole('heading', { name: 'Delete Example Group' })).not.toBeNull()
    expect(screen.getByText('Its categories move to the replacement.')).not.toBeNull()
    expect(controller.submit).not.toHaveBeenCalled()
  })
})
