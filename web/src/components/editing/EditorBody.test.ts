import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { tick } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import EditorBody from './EditorBody.svelte'
import type { EditorCatalog, MerchantPreview, ViewProjection } from '../../lib/api/client'
import type { EditingController } from '../../lib/controller/editing'
import { EditorDraft } from '../../lib/editing/draft.svelte'
import { testCatalog, testEditingController } from '../../test/editing'
import { testAggregateRow, testProjection } from '../../test/projection'

type Kind = 'merchant' | 'category'

const merchantRows = testProjection({
  detail_rows: null,
  aggregate_rows: [testAggregateRow({ identity: 'agg-0', count: 4 })],
})

function preview(overrides: Partial<MerchantPreview> = {}): MerchantPreview {
  return {
    version: '1',
    revision: '1',
    affected_transactions: 5,
    outside_view: 0,
    outside_currencies: [],
    transactions: [
      {
        index: 0,
        identity: 'transaction-a',
        date: '2024-01-01',
        account: 'Example Account',
        merchant: 'Current Merchant',
        category: 'Example Category',
        group: 'Example Group',
        amount: { minor: '-100', currency: 'USD', scale: 2, decimal: '-1.00', display: '-1.00' },
        flags: { selected: false, hidden: false, pending: false },
      },
    ],
    ...overrides,
  }
}

interface Setup {
  kind?: Kind
  projection?: ViewProjection
  controller?: EditingController
  draft?: EditorDraft
}

function setup({ kind = 'merchant', projection, controller, draft }: Setup = {}) {
  const editing = controller ?? testEditingController()
  const onstage = vi.fn()
  const oncancel = vi.fn()
  const view = projection ?? testProjection({ revision: '1' })
  const target = view.detail_rows
    ? { kind: 'transaction' as const, identity: 'row-0' }
    : { kind: 'aggregate' as const, identity: 'agg-0' }
  const rendered = render(EditorBody, {
    props: {
      kind,
      target,
      controller: editing,
      projection: view,
      cursorIndex: 0,
      id: 'editor',
      draft: draft ?? new EditorDraft(),
      onstage,
      oncancel,
    },
  })
  const name = kind === 'merchant' ? 'Merchant' : 'Category'
  const input = screen.getByRole('combobox', { name }) as HTMLInputElement
  return { editing, onstage, oncancel, input, unmount: rendered.unmount }
}

async function type(input: HTMLInputElement, text: string): Promise<void> {
  await fireEvent.keyDown(input, { key: text.slice(-1) })
  await fireEvent.input(input, { target: { value: text } })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => (resolve = done))
  return { promise, resolve }
}

/** A merchant editor whose catalog and preview each wait until the test resolves them. */
function loading() {
  const catalog = deferred<EditorCatalog>()
  const previewed = deferred<MerchantPreview>()
  const controller = testEditingController({
    catalog: vi.fn(() => catalog.promise),
    previewMerchant: vi.fn(() => previewed.promise),
  })
  return { ...setup({ controller }), catalog, previewed }
}

const twoGroups = [
  ...(testCatalog.groups ?? []),
  { id: 'group-b', label: 'Second Group', protected: false, transaction_count: 0, in_use: false },
]

const stageButton = (count: string) =>
  screen.findByRole('button', { name: `Stage change to ${count}` })

describe('EditorBody', () => {
  beforeEach(() => {
    Element.prototype.scrollIntoView = vi.fn()
  })
  afterEach(cleanup)

  it('titles the editor, focuses the field, and previews the affected transactions', async () => {
    const { input } = setup()
    expect(screen.getByRole('heading', { name: 'Edit merchant' }).id).toBe('editor-title')
    expect(document.getElementById('editor-count')?.textContent).toBe('· 1 transaction')
    expect(document.activeElement).toBe(input)
    expect(await stageButton('5 transactions')).toBeTruthy()
    const rows = screen.getByRole('list', { name: 'Affected transactions' })
    expect(within(rows).getByText('2024-01-01')).toBeTruthy()
    expect(within(rows).getByText('-$1.00 USD')).toBeTruthy()
  })

  it('disables the whole merchant with its reason outside a merchant row', async () => {
    setup()
    const whole = await screen.findByRole('radio', { name: 'Whole merchant' })
    expect(whole.hasAttribute('disabled')).toBe(true)
    expect(screen.getByText('Open a merchant to rename all its transactions')).toBeTruthy()
  })

  it('stages a new merchant name on Enter and reports it staged', async () => {
    const { editing, input, onstage } = setup()
    await stageButton('5 transactions')
    await type(input, 'Renamed Example')
    await fireEvent.keyDown(input, { key: 'Escape' })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await vi.waitFor(() => expect(onstage).toHaveBeenCalledTimes(1))
    expect(editing.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        action: 'transaction.edit-merchant',
        target: { kind: 'transaction', identity: 'row-0' },
        input: expect.objectContaining({ scope: 'transactions', label: 'Renamed Example' }),
      }),
    )
  })

  it('accepts and stages a new merchant name on Ctrl+Enter from the open list', async () => {
    const { editing, input, onstage } = setup()
    await stageButton('5 transactions')
    await type(input, 'Renamed Example')
    expect(screen.getByRole('option', { name: /Create "Renamed Example"/ })).toBeTruthy()
    await fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true })
    await vi.waitFor(() => expect(onstage).toHaveBeenCalledTimes(1))
    expect(editing.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        action: 'transaction.edit-merchant',
        input: expect.objectContaining({ label: 'Renamed Example' }),
      }),
    )
  })

  it('re-requests the preview on a scope change and aborts the earlier request', async () => {
    const signals: Record<string, AbortSignal[]> = { transactions: [], entity: [] }
    const editing = testEditingController({
      previewMerchant: vi.fn((_target, scope: string, signal?: AbortSignal) => {
        if (signal) signals[scope]?.push(signal)
        if (scope !== 'entity') return new Promise<MerchantPreview>(() => undefined)
        return Promise.resolve(
          preview({
            affected_transactions: 141,
            outside_view: 127,
            outside_currencies: [{ currency: 'EUR', scale: 2 }],
          }),
        )
      }),
    })
    setup({ projection: { ...merchantRows, revision: '1' }, controller: editing })
    const whole = await screen.findByRole('radio', { name: 'Whole merchant · 141 transactions' })
    expect(signals['transactions']?.[0]?.aborted).toBe(false)
    await fireEvent.click(whole)
    expect(signals['transactions']?.[0]?.aborted).toBe(true)
    expect(signals['entity']).toHaveLength(2)
    expect(await stageButton('141 transactions')).toBeTruthy()
    expect(screen.getByText('Changes all 141 transactions of this merchant.')).toBeTruthy()
    expect(
      screen.getByText(
        '127 of these are outside the current view (other periods, filters, or EUR).',
      ),
    ).toBeTruthy()
  })

  it('offers no choices until they load, then stages an Enter pressed before them', async () => {
    const { editing, input, onstage, catalog, previewed } = loading()
    await type(input, 'example merchant')
    expect(screen.queryAllByRole('option')).toHaveLength(0)
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(screen.getByRole('button', { name: /^Stage change/ }).hasAttribute('disabled')).toBe(
      true,
    )
    catalog.resolve(testCatalog)
    previewed.resolve(preview())
    await vi.waitFor(() => expect(onstage).toHaveBeenCalledTimes(1))
    // The queued stage uses the loaded choices, so it merges instead of creating a duplicate.
    expect(editing.submit).toHaveBeenCalledTimes(1)
    expect(editing.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        input: expect.objectContaining({ destination_id: 'merchant-a' }),
      }),
    )
    expect(input.getAttribute('aria-expanded')).toBe('false')
  })

  it('stages an Enter pressed after accepting a name but before the preview', async () => {
    const { editing, input, onstage, catalog, previewed } = loading()
    catalog.resolve(testCatalog)
    await type(input, 'Local Merchant')
    await screen.findByRole('option', { name: 'Create "Local Merchant"' })
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(input.getAttribute('aria-expanded')).toBe('false')
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(editing.submit).not.toHaveBeenCalled()
    previewed.resolve(preview())
    await vi.waitFor(() => expect(onstage).toHaveBeenCalledTimes(1))
    expect(editing.submit).toHaveBeenCalledTimes(1)
    expect(editing.submit).toHaveBeenCalledWith(
      expect.objectContaining({ input: expect.objectContaining({ label: 'Local Merchant' }) }),
    )
  })

  it('drops a stage queued while loading once the text changes', async () => {
    const { editing, input, onstage, catalog, previewed } = loading()
    await type(input, 'Local Merchant')
    await fireEvent.keyDown(input, { key: 'Enter' })
    await type(input, 'Local Merchants')
    catalog.resolve(testCatalog)
    previewed.resolve(preview())
    await stageButton('5 transactions')
    expect(editing.submit).not.toHaveBeenCalled()
    expect(onstage).not.toHaveBeenCalled()
  })

  it('says a rename onto an existing merchant merges into it', async () => {
    const { input } = setup()
    await type(input, 'example merchant')
    expect(screen.getByText('Merges into Example Merchant')).toBeTruthy()
  })

  it('does not say a rename merges into the merchant being renamed', async () => {
    const own = { ...testCatalog.merchants![0]!, id: 'merchant-current', label: 'Current Merchant' }
    const editing = testEditingController({
      catalog: vi.fn(async () => ({ ...testCatalog, merchants: [own] })),
      previewMerchant: vi.fn(async () =>
        preview({ affected_transactions: 1, source_merchant_id: 'merchant-current' }),
      ),
    })
    const { input } = setup({ controller: editing })
    await stageButton('1 transaction')
    await type(input, 'current merchant')
    expect(screen.getByRole('option', { name: /^Current Merchant, / })).toBeTruthy()
    expect(screen.queryByText(/^Merges into/)).toBeNull()
  })

  it('remounts with the draft and the view it opened over, focusing the field', async () => {
    const draft = new EditorDraft()
    const first = setup({ projection: { ...merchantRows, revision: '1' }, draft })
    await type(first.input, 'Renamed Example')
    await fireEvent.click(await screen.findByRole('radio', { name: /^Whole merchant/ }))
    first.unmount()

    const moved = { ...merchantRows, revision: '1', selection_count: 3 }
    const { editing, input, onstage } = setup({ projection: moved, draft })
    expect(input.value).toBe('Renamed Example')
    expect(document.activeElement).toBe(input)
    expect(document.getElementById('editor-count')?.textContent).toBe('· 4 transactions')
    expect(
      screen.getByRole('radio', { name: /^Whole merchant/ }).getAttribute('aria-checked'),
    ).toBe('true')
    await fireEvent.click(await stageButton('5 transactions'))
    await vi.waitFor(() => expect(onstage).toHaveBeenCalledTimes(1))
    expect(editing.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        target: { kind: 'aggregate', identity: 'agg-0' },
        input: { scope: 'entity', label: 'Renamed Example' },
      }),
    )
  })

  it('remounts on the group step of an accepted Create with its group', async () => {
    const editing = testEditingController({
      catalog: vi.fn(async () => ({ ...testCatalog, groups: twoGroups })),
    })
    const draft = new EditorDraft()
    const first = setup({ kind: 'category', controller: editing, draft })
    await stageButton('5 transactions')
    await type(first.input, 'Pet Care')
    await fireEvent.keyDown(first.input, { key: 'Enter' })
    await fireEvent.click(await screen.findByRole('combobox', { name: /Category group/ }))
    await fireEvent.click(screen.getByRole('option', { name: 'Second Group' }))
    first.unmount()

    const { input, onstage } = setup({ kind: 'category', controller: editing, draft })
    expect(input.value).toBe('Pet Care')
    expect(document.activeElement).toBe(input)
    expect(
      await screen.findByRole('combobox', { name: 'Category group: Second Group' }),
    ).toBeTruthy()
    await fireEvent.click(await stageButton('5 transactions'))
    await vi.waitFor(() => expect(onstage).toHaveBeenCalledTimes(1))
    expect(vi.mocked(editing.submit).mock.calls[0]?.[0].input).toMatchObject({
      label: 'Pet Care',
      group_id: 'group-b',
    })
  })

  it('does not report a stage that finishes after the editor closed', async () => {
    const request = deferred<boolean>()
    const editing = testEditingController({ submit: vi.fn(() => request.promise) })
    const { input, onstage, unmount } = setup({ controller: editing })
    await type(input, 'Renamed')
    await fireEvent.click(await stageButton('5 transactions'))
    expect(editing.submit).toHaveBeenCalledTimes(1)
    unmount()
    request.resolve(true)
    await request.promise
    await tick()
    expect(onstage).not.toHaveBeenCalled()
  })

  it('cancels once on Escape with the list closed and claims the key', async () => {
    const { input, oncancel } = setup()
    const outer = vi.fn((event: KeyboardEvent) => event.defaultPrevented)
    document.body.addEventListener('keydown', outer)
    expect(await fireEvent.keyDown(input, { key: 'Escape' })).toBe(false)
    document.body.removeEventListener('keydown', outer)
    expect(oncancel).toHaveBeenCalledTimes(1)
    expect(outer).not.toHaveBeenCalled()
  })

  it('cancels on Escape from a button and stages on Enter from the scope group', async () => {
    const { input, oncancel, onstage } = setup({
      projection: { ...merchantRows, revision: '1' },
    })
    await stageButton('5 transactions')
    await type(input, 'Renamed')
    const scope = screen.getByRole('radio', { name: /^\d+ transactions$/ })
    expect(await fireEvent.keyDown(scope, { key: 'Enter' })).toBe(false)
    await vi.waitFor(() => expect(onstage).toHaveBeenCalledTimes(1))
    await fireEvent.keyDown(screen.getByRole('button', { name: 'Cancel' }), { key: 'Escape' })
    expect(oncancel).toHaveBeenCalledTimes(1)
  })

  it('stages nothing while the name is blank', async () => {
    const { editing, input } = setup()
    await stageButton('5 transactions')
    await fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true })
    const stage = screen.getByRole('button', { name: 'Stage change to 5 transactions' })
    expect(stage.hasAttribute('disabled')).toBe(true)
    expect(editing.submit).not.toHaveBeenCalled()
  })

  it('shows why a staging attempt failed', async () => {
    const editing = testEditingController({ submit: vi.fn(async () => false) })
    editing.state.announcement = 'The profile request was rejected.'
    const { input, onstage } = setup({ controller: editing })
    await stageButton('5 transactions')
    await type(input, 'Renamed')
    await fireEvent.click(screen.getByRole('button', { name: 'Stage change to 5 transactions' }))
    expect((await screen.findByRole('alert')).textContent).toBe('The profile request was rejected.')
    expect(onstage).not.toHaveBeenCalled()
  })

  it('re-enables Stage and explains a refusal that has no announcement', async () => {
    const editing = testEditingController({ submit: vi.fn(async () => false) })
    editing.state.announcement = ''
    const { input, onstage } = setup({ controller: editing })
    await type(input, 'Renamed')
    const stage = await stageButton('5 transactions')
    await fireEvent.click(stage)
    expect((await screen.findByRole('alert')).textContent).toBe('The change could not be staged.')
    expect(stage.hasAttribute('disabled')).toBe(false)
    expect(onstage).not.toHaveBeenCalled()
  })

  it('moves transactions to an existing category, with no scope choice', async () => {
    const { editing, input, onstage } = setup({ kind: 'category' })
    expect(screen.getByRole('heading', { name: 'Change category' })).toBeTruthy()
    expect(screen.queryByRole('radiogroup')).toBeNull()
    await stageButton('5 transactions')
    await type(input, 'example')
    expect(screen.getByRole('option', { name: /Example Category, Example Group/ })).toBeTruthy()
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(input.value).toBe('Example Category')
    await fireEvent.keyDown(input, { key: 'Enter' })
    await vi.waitFor(() => expect(onstage).toHaveBeenCalledTimes(1))
    expect(editing.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        action: 'transaction.edit-category',
        input: { scope: 'transactions', destination_id: 'category-a' },
      }),
    )
  })

  it('asks for a group after Create, and Escape there returns to the field', async () => {
    const { editing, input, oncancel, onstage } = setup({ kind: 'category' })
    await stageButton('5 transactions')
    await type(input, 'Pet Care')
    await fireEvent.keyDown(input, { key: 'Enter' })
    const group = await screen.findByRole('combobox', { name: /Category group/ })
    await tick()
    expect(document.activeElement).toBe(group)
    expect(await fireEvent.keyDown(group, { key: 'Escape' })).toBe(false)
    expect(oncancel).not.toHaveBeenCalled()
    await vi.waitFor(() => expect(document.activeElement).toBe(input))
    expect(screen.queryByRole('combobox', { name: /Category group/ })).toBeNull()

    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    await fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true })
    expect(onstage).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByRole('button', { name: 'Stage change to 5 transactions' }))
    await vi.waitFor(() => expect(onstage).toHaveBeenCalledTimes(1))
    expect(vi.mocked(editing.submit).mock.calls[0]?.[0].input).toEqual({
      scope: 'transactions',
      destination_id: expect.stringMatching(/^category_/),
      label: 'Pet Care',
      group_id: testCatalog.groups?.[0]?.id,
    })
  })
  it('drops a stage queued on the group step once Escape leaves it', async () => {
    const previewed = deferred<MerchantPreview>()
    const controller = testEditingController({ previewMerchant: vi.fn(() => previewed.promise) })
    const { editing, input, onstage } = setup({ kind: 'category', controller })
    await type(input, 'Pet Care')
    await screen.findByRole('option', { name: 'Create "Pet Care"' })
    await fireEvent.keyDown(input, { key: 'Enter' })
    const group = await screen.findByRole('combobox', { name: /Category group/ })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await fireEvent.keyDown(group, { key: 'Escape' })
    await vi.waitFor(() => expect(document.activeElement).toBe(input))
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await screen.findByRole('combobox', { name: /Category group/ })
    previewed.resolve(preview())
    await stageButton('5 transactions')
    expect(editing.submit).not.toHaveBeenCalled()
    expect(onstage).not.toHaveBeenCalled()
  })

  it('closes only the open group list on Escape and keeps the editor open', async () => {
    const { input, oncancel } = setup({ kind: 'category' })
    await stageButton('5 transactions')
    await type(input, 'Pet Care')
    await fireEvent.keyDown(input, { key: 'Enter' })
    const group = await screen.findByRole('combobox', { name: /Category group/ })
    await fireEvent.click(group)
    expect(group.getAttribute('aria-expanded')).toBe('true')
    await fireEvent.keyDown(group, { key: 'Escape' })
    expect(group.getAttribute('aria-expanded')).toBe('false')
    expect(screen.getByRole('combobox', { name: /Category group/ })).toBe(group)
    expect(document.activeElement).toBe(group)
    expect(oncancel).not.toHaveBeenCalled()
  })

  it('keeps the chosen group when Create is accepted again', async () => {
    const editing = testEditingController({
      catalog: vi.fn(async () => ({ ...testCatalog, groups: twoGroups })),
    })
    const { input, onstage } = setup({ kind: 'category', controller: editing })
    await stageButton('5 transactions')
    await type(input, 'Pet Care')
    await fireEvent.keyDown(input, { key: 'Enter' })
    await fireEvent.click(await screen.findByRole('combobox', { name: /Category group/ }))
    await fireEvent.click(screen.getByRole('option', { name: 'Second Group' }))
    await fireEvent.keyDown(screen.getByRole('combobox', { name: /Category group/ }), {
      key: 'Escape',
    })
    await vi.waitFor(() => expect(document.activeElement).toBe(input))
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(
      await screen.findByRole('combobox', { name: 'Category group: Second Group' }),
    ).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Stage change to 5 transactions' }))
    await vi.waitFor(() => expect(onstage).toHaveBeenCalledTimes(1))
    expect(vi.mocked(editing.submit).mock.calls[0]?.[0].input).toMatchObject({
      group_id: 'group-b',
    })
  })
})
