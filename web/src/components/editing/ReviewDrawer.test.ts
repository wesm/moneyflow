import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte'
import { tick } from 'svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ReviewDrawer from './ReviewDrawer.svelte'
import type { MutationFetch } from '../../lib/api/client'
import type { EditingController } from '../../lib/controller/editing'
import {
  createProviderWriteController,
  type ProviderWriteController,
} from '../../lib/controller/provider-write'
import { createReviewController, type ReviewController } from '../../lib/controller/review'
import { testEditingController, testReviewController } from '../../test/editing'
import { testProviderWriteController } from '../../test/provider-write'

interface Setup {
  mode?: 'review' | 'write'
  editing?: EditingController
  review?: ReviewController
  providerWrite?: ProviderWriteController
  providerName?: string
  onreconnect?: () => void
}

function setup(options: Setup = {}) {
  const editing = options.editing ?? testEditingController()
  const onclose = vi.fn()
  const onwrite = vi.fn()
  const view = render(ReviewDrawer, {
    mode: options.mode ?? 'review',
    editing,
    review: options.review ?? testReviewController(),
    providerWrite: options.providerWrite ?? testProviderWriteController(),
    providerName: options.providerName ?? 'Monarch',
    onclose,
    onwrite,
    onreconnect: options.onreconnect,
  })
  return { editing, onclose, onwrite, view }
}

/** Waits until the review has loaded and Commit has focus, as after `w`. */
async function loaded(): Promise<HTMLElement> {
  const commit = screen.getByRole('button', { name: 'Commit reviewed changes' })
  await waitFor(() => expect(document.activeElement).toBe(commit))
  return commit
}

/** A promise the test settles, standing in for a commit request in flight. */
function deferred() {
  let settle: (accepted: boolean) => void = () => undefined
  const promise = new Promise<boolean>((resolve) => (settle = resolve))
  return { promise, settle }
}

/** A rename that affects `affected` transactions. */
function renameOperation(affected: number) {
  return {
    operation_id: 'operation-a',
    type: 'merchant.label',
    label: 'Rename merchant',
    active: true,
    affected_count: affected,
    before: 'Old Merchant',
    after: 'New Merchant',
  }
}

/** Transactions `offset + 1` onward, at most `limit` of `affected`, named by row number. */
function affectedRows(affected: number, offset: number, limit: number) {
  const count = Math.max(0, Math.min(limit, affected - offset))
  return Array.from({ length: count }, (_, index) => ({
    date: '2024-01-01',
    merchant: `Example Merchant ${offset + index + 1}`,
    category: 'Example Category',
    amount: {
      minor: `-${offset + index + 1}05`,
      currency: 'USD',
      scale: 2,
      decimal: `-${offset + index + 1}.05`,
      display: `-${offset + index + 1}.05`,
    },
    hidden: offset + index === 0,
  }))
}

/**
 * A review server whose one operation affects `affected` transactions. As the server does,
 * the review carries the operation's first 20 rows. Each target request waits for `wait`,
 * when given.
 */
function reviewServer(
  affected: number,
  operation: ReturnType<typeof renameOperation> = renameOperation(affected),
  wait?: () => Promise<void>,
): MutationFetch {
  return {
    request: vi.fn(async (_path: string, body: string) => {
      const window = (JSON.parse(body) as { window?: { offset: number; limit: number } }).window
      if (window) await wait?.()
      const targets = window ? affectedRows(affected, window.offset, window.limit) : undefined
      const response = {
        version: '1',
        revision: '1',
        pending: { active_operations: 1, inactive_operations: 0, affected_transactions: affected },
        active_operations: [{ ...operation, targets: affectedRows(affected, 0, 20) }],
        inactive_operations: [],
        window: { offset: window?.offset ?? 0, limit: window?.limit ?? 0, count: targets?.length },
        targets,
      }
      return new Response(JSON.stringify(response), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }),
  }
}

describe('ReviewDrawer in review mode', () => {
  afterEach(cleanup)

  it('shows each operation as old → new with its affected count, and the redo section', () => {
    const review = testReviewController({
      state: {
        ...testReviewController().state,
        activeOperations: [
          {
            operation_id: 'operation-a',
            type: 'merchant.label',
            label: 'Rename merchant',
            active: true,
            affected_count: 2,
            before: 'Old Merchant',
            after: 'New Merchant',
          },
          {
            operation_id: 'operation-c',
            type: 'category.assign',
            label: 'Change category',
            active: true,
            affected_count: 1,
            after: 'Example Category',
          },
        ],
      },
    })
    setup({ review })
    const drawer = screen.getByRole('dialog', { name: 'Review pending changes' })
    const rename = within(drawer).getByRole('button', { name: /^Rename merchant/ })
    expect(rename.textContent).toMatch(/Old Merchant\s*→\s*New Merchant/)
    expect(rename.textContent).toContain('2 transactions')
    const category = within(drawer).getByRole('button', { name: /^Change category/ })
    expect(category.textContent).toMatch(/→\s*Example Category/)
    expect(category.textContent).toContain('1 transaction')
    expect(within(drawer).getByRole('heading', { name: 'Inactive redo operations' })).toBeTruthy()
    expect(drawer.textContent).toContain('Hide transactions')
  })

  it('counts a structural operation that affects no transactions', () => {
    const review = testReviewController({
      state: {
        ...testReviewController().state,
        activeOperations: [
          {
            operation_id: 'operation-empty',
            type: 'category.create',
            label: 'Create category',
            active: true,
            affected_count: 0,
          },
        ],
      },
    })
    setup({ review })
    expect(screen.getByRole('button', { name: /^Create category/ }).textContent).toContain(
      '0 transactions',
    )
    expect(screen.queryByText(/provider item/i)).toBeNull()
  })

  it('lists each operation’s first 20 transactions without a click, then 400 more per click', async () => {
    const transport = reviewServer(445)
    const review = createReviewController({ transport, revision: () => 1n })
    setup({ review })
    await loaded()
    const operation = screen.getByRole('button', { name: /^Rename merchant/ })
    expect(operation.getAttribute('aria-expanded')).toBe('true')
    // A header row and the first 20 transactions.
    await waitFor(() => expect(screen.getAllByRole('row')).toHaveLength(21))
    expect(screen.getByText('Example Merchant 20')).toBeTruthy()
    expect(screen.queryByText('Example Merchant 21')).toBeNull()
    expect(screen.getByText('425 transactions not shown')).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'Show 400 more' }))
    await screen.findByText('Example Merchant 420')
    expect(screen.getAllByRole('row')).toHaveLength(421)
    expect(screen.getByText('25 transactions not shown')).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Show 25 more' }))
    await screen.findByText('Example Merchant 445')
    expect(screen.getAllByRole('row')).toHaveLength(446)
    expect(screen.queryByRole('button', { name: /^Show \d+ more/ })).toBeNull()
    expect(screen.queryByText(/not shown/)).toBeNull()
    const bodies = vi.mocked(transport.request).mock.calls.map(([, body]) => JSON.parse(body))
    expect(bodies.map((body: { window?: unknown }) => body.window)).toEqual([
      undefined,
      { offset: 20, limit: 400 },
      { offset: 420, limit: 400 },
    ])
  })

  it('opens with one request however many operations are pending', async () => {
    const operations = ['operation-a', 'operation-b', 'operation-c'].map((id, index) => ({
      ...renameOperation(2),
      operation_id: id,
      before: `Old Merchant ${index + 1}`,
      targets: affectedRows(2, 0, 20),
    }))
    const transport: MutationFetch = {
      request: vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              version: '1',
              revision: '1',
              pending: { active_operations: 3, inactive_operations: 0, affected_transactions: 6 },
              active_operations: operations,
              inactive_operations: [],
              window: { offset: 0, limit: 0, count: 0 },
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
      ),
    }
    setup({ review: createReviewController({ transport, revision: () => 1n }) })
    await loaded()

    expect(screen.getAllByRole('table', { name: 'Affected transactions' })).toHaveLength(3)
    // A header row and two transactions for each operation.
    expect(screen.getAllByRole('row')).toHaveLength(9)
    await tick()
    expect(vi.mocked(transport.request).mock.calls.map(([path]) => path)).toEqual(['api/v1/review'])
  })

  it('collapses and expands an operation without loading its rows again', async () => {
    const transport = reviewServer(2)
    const review = createReviewController({ transport, revision: () => 1n })
    setup({ review })
    await loaded()
    await screen.findByText('Example Merchant 2')
    expect(screen.queryByRole('button', { name: /^Show \d+ more/ })).toBeNull()
    const operation = screen.getByRole('button', { name: /^Rename merchant/ })

    await fireEvent.click(operation)
    expect(operation.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByText('Example Merchant 2')).toBeNull()
    await fireEvent.click(operation)
    expect(operation.getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByText('Example Merchant 2')).toBeTruthy()
    expect(transport.request).toHaveBeenCalledTimes(1)
  })

  it('shows a loading line while more of an operation’s rows load', async () => {
    let release = (): void => undefined
    const transport = reviewServer(
      22,
      renameOperation(22),
      () => new Promise<void>((resolve) => (release = resolve)),
    )
    const review = createReviewController({ transport, revision: () => 1n })
    setup({ review })
    await loaded()
    expect(screen.queryByText('Loading transactions…')).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Show 2 more' }))
    expect(await screen.findByText('Loading transactions…')).toBeTruthy()
    release()
    await screen.findByText('Example Merchant 22')
    expect(screen.queryByText('Loading transactions…')).toBeNull()
  })

  it('shows each transaction’s date, merchant, category, and amount with its currency', async () => {
    const review = createReviewController({ transport: reviewServer(2), revision: () => 1n })
    setup({ review })
    await loaded()
    await waitFor(() => expect(screen.getAllByRole('row')).toHaveLength(3))
    const [header, first, second] = screen.getAllByRole('row')
    const texts = (row: HTMLElement | undefined, role: 'columnheader' | 'cell') =>
      within(row!)
        .getAllByRole(role)
        .map((cell) => cell.textContent?.replace(/\s+/g, ' ').trim())
    expect(texts(header, 'columnheader')).toEqual(['Date', 'Merchant', 'Category', 'Amount'])
    expect(texts(first, 'cell')).toEqual([
      '2024-01-01',
      'Example Merchant 1 → New Merchant Hidden',
      'Example Category',
      '-1.05 USD',
    ])
    expect(texts(second, 'cell')).toEqual([
      '2024-01-01',
      'Example Merchant 2 → New Merchant',
      'Example Category',
      '-2.05 USD',
    ])
  })

  it('shows a hidden or shown row’s visibility as old → new', async () => {
    const hide = {
      ...renameOperation(2),
      type: 'transaction.hide-toggle',
      label: 'Hide transactions',
      before: 'Visible',
      after: 'Hidden',
    }
    const review = createReviewController({ transport: reviewServer(2, hide), revision: () => 1n })
    setup({ review })
    await loaded()
    await waitFor(() => expect(screen.getAllByRole('row')).toHaveLength(3))
    const [, first, second] = screen.getAllByRole('row')
    const merchant = (row: HTMLElement | undefined) =>
      within(row!).getAllByRole('cell')[1]!.textContent?.replace(/\s+/g, ' ').trim()
    // The first row was already hidden; the hide leaves it so.
    expect(merchant(first)).toBe('Example Merchant 1 Hidden')
    expect(merchant(second)).toBe('Example Merchant 2 Visible → Hidden')
  })

  it('puts Commit with its Enter hint in the footer and focuses it after loading', async () => {
    setup()
    const commit = await loaded()
    expect(commit.closest('.kit-detail-drawer__footer')).not.toBeNull()
    expect(commit.querySelector('kbd')?.getAttribute('aria-label')).toBe('Enter')
  })

  it('commits the captured revision with Enter from the drawer or Commit, once', async () => {
    for (const target of ['drawer', 'commit'] as const) {
      const { editing, onclose } = setup()
      const commit = await loaded()
      const element =
        target === 'drawer'
          ? screen.getByRole('dialog', { name: 'Review pending changes' })
          : commit
      element.focus()
      await fireEvent.keyDown(element, { key: 'Enter' })
      await waitFor(() => expect(onclose).toHaveBeenCalledTimes(1))
      expect(editing.commit).toHaveBeenCalledTimes(1)
      expect(editing.commit).toHaveBeenCalledWith(1n)
      cleanup()
    }
  })

  it('leaves Enter to controls with their own Enter action', async () => {
    const review = createReviewController({ transport: reviewServer(25), revision: () => 1n })
    const { editing } = setup({ review })
    await loaded()
    const drawer = screen.getByRole('dialog', { name: 'Review pending changes' })
    await screen.findByRole('button', { name: 'Show 5 more' })
    for (const name of [/^Rename merchant/, 'Close', 'Cancel', 'Show 5 more']) {
      const control = within(drawer).getByRole('button', { name })
      control.focus()
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
      control.dispatchEvent(event)
      expect(event.defaultPrevented, String(name)).toBe(false)
    }
    expect(editing.commit).not.toHaveBeenCalled()
  })

  it('ignores a held Enter, a modified Enter, and Enter while a commit is in flight', async () => {
    const request = deferred()
    const editing = testEditingController({ commit: vi.fn(() => request.promise) })
    setup({ editing })
    await loaded()
    const drawer = screen.getByRole('dialog', { name: 'Review pending changes' })
    drawer.focus()
    await fireEvent.keyDown(drawer, { key: 'Enter', repeat: true })
    for (const modifier of ['shiftKey', 'ctrlKey', 'altKey', 'metaKey']) {
      await fireEvent.keyDown(drawer, { key: 'Enter', [modifier]: true })
    }
    expect(editing.commit).not.toHaveBeenCalled()

    await fireEvent.keyDown(drawer, { key: 'Enter' })
    await fireEvent.keyDown(drawer, { key: 'Enter' })
    await fireEvent.click(screen.getByRole('button', { name: 'Commit reviewed changes' }))
    expect(editing.commit).toHaveBeenCalledTimes(1)
    request.settle(false)
    await request.promise
    await tick()

    // A rejected commit can be tried again.
    await fireEvent.keyDown(drawer, { key: 'Enter' })
    expect(editing.commit).toHaveBeenCalledTimes(2)
  })

  it('shows why the server refused a commit and keeps the drawer open', async () => {
    const editing = testEditingController({ commit: vi.fn(async () => false) })
    editing.state.announcement =
      'The profile changed in another session. Review the current data and try again.'
    const { onclose } = setup({ editing })
    const button = await loaded()
    await fireEvent.keyDown(button, { key: 'Enter' })
    const drawer = screen.getByRole('dialog', { name: 'Review pending changes' })
    expect((await within(drawer).findByRole('alert')).textContent).toBe(
      'The profile changed in another session. Review the current data and try again.',
    )
    expect(onclose).not.toHaveBeenCalled()
  })

  it('reports a refused commit and lets Enter try again', async () => {
    const commit = vi
      .fn<EditingController['commit']>()
      .mockResolvedValueOnce(false)
      .mockResolvedValueOnce(true)
    const { onclose } = setup({ editing: testEditingController({ commit }) })
    const button = await loaded()
    await fireEvent.keyDown(button, { key: 'Enter' })
    const drawer = screen.getByRole('dialog', { name: 'Review pending changes' })
    expect((await within(drawer).findByRole('alert')).textContent).toBe(
      'The profile request failed.',
    )
    expect(onclose).not.toHaveBeenCalled()

    await fireEvent.keyDown(button, { key: 'Enter' })
    await waitFor(() => expect(onclose).toHaveBeenCalledTimes(1))
    expect(commit).toHaveBeenCalledTimes(2)
  })

  it('retains one fast Enter during initial review loading, but never after closing', async () => {
    for (const closeBeforeReady of [false, true]) {
      const ready = deferred()
      const review = testReviewController({ load: vi.fn(() => ready.promise) })
      const { editing, view } = setup({ review })
      const drawer = screen.getByRole('dialog', { name: 'Review pending changes' })
      drawer.focus()
      await fireEvent.keyDown(drawer, { key: 'Enter' })
      await fireEvent.keyDown(drawer, { key: 'Enter', repeat: true })
      expect(editing.commit).not.toHaveBeenCalled()
      if (closeBeforeReady) view.unmount()
      ready.settle(true)
      if (closeBeforeReady) {
        await ready.promise
        expect(editing.commit).not.toHaveBeenCalled()
      } else {
        await waitFor(() => expect(editing.commit).toHaveBeenCalledTimes(1))
        view.unmount()
      }
    }
  })

  it('asks to switch to write mode when the commit starts a provider write', async () => {
    const editing = testEditingController({
      state: {
        ...testEditingController().state,
        providerWrite: testProviderWriteController().state.status!,
      },
    })
    const { onclose, onwrite } = setup({ editing })
    const commit = await loaded()
    await fireEvent.keyDown(commit, { key: 'Enter' })
    await waitFor(() => expect(onwrite).toHaveBeenCalledTimes(1))
    expect(onclose).not.toHaveBeenCalled()
  })

  it('turns the same drawer into write status and keeps focus inside it', async () => {
    const { view } = setup()
    const commit = await loaded()
    const drawer = screen.getByRole('dialog', { name: 'Review pending changes' })
    await view.rerender({ mode: 'write' })
    expect(screen.getByRole('dialog', { name: 'Monarch write status' })).toBe(drawer)
    expect(commit.isConnected).toBe(false)
    await waitFor(() => expect(drawer.contains(document.activeElement)).toBe(true))
  })
})

describe('ReviewDrawer in write mode', () => {
  afterEach(cleanup)

  it('shows counts-only progress and pauses without treating close as cancel', async () => {
    const providerWrite = testProviderWriteController()
    const { onclose, editing } = setup({ mode: 'write', providerWrite })
    const drawer = screen.getByRole('dialog', { name: 'Monarch write status' })
    expect(within(drawer).getByText('2 of 5 complete')).toBeTruthy()
    expect(within(drawer).getByRole('status').textContent).toBe(
      'Writing pending changes to Monarch.',
    )
    expect(within(drawer).queryByRole('button', { name: 'Commit reviewed changes' })).toBeNull()
    await fireEvent.click(within(drawer).getByRole('button', { name: 'Pause' }))
    expect(providerWrite.pause).toHaveBeenCalledTimes(1)
    expect(onclose).not.toHaveBeenCalled()

    drawer.focus()
    await fireEvent.keyDown(drawer, { key: 'Enter' })
    expect(editing.commit).not.toHaveBeenCalled()
  })

  it('replaces Pause with recovery controls when a write is rejected', async () => {
    const initial = testProviderWriteController().state.status!
    const providerWrite = createProviderWriteController({
      transport: { mutations: { request: vi.fn() }, status: vi.fn() },
      host: { current: () => undefined, accept: vi.fn(), reload: vi.fn() },
    })
    providerWrite.install(initial)
    setup({ mode: 'write', providerWrite })
    expect(screen.getByRole('button', { name: 'Pause' })).toBeTruthy()
    providerWrite.install({ ...initial, phase: 'attention_required', actions: ['reconcile'] })
    expect(await screen.findByRole('button', { name: 'Stop and reconcile' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Pause' })).toBeNull()
  })

  it('offers Check and resume for an uncertain write', async () => {
    const providerWrite = testProviderWriteController({
      state: {
        phase: 'attention',
        announcement: 'A provider request may have succeeded.',
        status: {
          ...testProviderWriteController().state.status!,
          phase: 'attention_required',
          reason: 'provider_write_outcome_unknown',
          actions: ['resume', 'reconcile'],
        },
      },
      can: vi.fn((action: string) => action === 'resume' || action === 'reconcile'),
    })
    setup({ mode: 'write', providerWrite })
    await fireEvent.click(screen.getByRole('button', { name: 'Check and resume' }))
    expect(providerWrite.resume).toHaveBeenCalledTimes(1)
    await fireEvent.click(screen.getByRole('button', { name: 'Stop and reconcile' }))
    expect(providerWrite.reconcile).toHaveBeenCalledTimes(1)
  })

  it('hides retry for reconcile-only attention and confirms removals explicitly', async () => {
    const providerWrite = testProviderWriteController({
      state: {
        phase: 'confirmation',
        announcement: 'Confirm provider removals.',
        status: {
          ...testProviderWriteController().state.status!,
          phase: 'reconcile_confirmation_required',
          actions: ['confirm'],
        },
      },
      can: vi.fn((action: string) => action === 'confirm'),
    })
    setup({ mode: 'write', providerWrite, providerName: 'YNAB', onreconnect: vi.fn() })
    expect(screen.queryByRole('button', { name: /Resume/ })).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm reconciliation' }))
    expect(providerWrite.confirm).toHaveBeenCalledTimes(1)
  })

  it('offers Reconnect when the provider needs it', async () => {
    const onreconnect = vi.fn()
    const providerWrite = testProviderWriteController({
      can: vi.fn((action: string) => action === 'reconnect'),
    })
    setup({ mode: 'write', providerWrite, onreconnect })
    await fireEvent.click(screen.getByRole('button', { name: 'Reconnect Monarch' }))
    expect(onreconnect).toHaveBeenCalledTimes(1)
  })

  it('shows the YNAB rate-limit time in the local format without inventing an estimate', () => {
    const nextEligible = '2026-09-07T13:00:00Z'
    const providerWrite = testProviderWriteController({
      state: {
        phase: 'active',
        announcement: 'YNAB asked Moneyflow to wait.',
        status: {
          ...testProviderWriteController().state.status!,
          phase: 'rate_limited',
          next_eligible: nextEligible,
        },
      },
    })
    setup({ mode: 'write', providerWrite, providerName: 'YNAB' })
    const drawer = screen.getByRole('dialog', { name: 'YNAB write status' })
    const time = drawer.querySelector('time')
    expect(time?.getAttribute('datetime')).toBe(nextEligible)
    expect(time?.textContent).toBe(new Date(nextEligible).toLocaleString())
    expect(time?.textContent).not.toBe(nextEligible)
    expect(drawer.textContent).not.toMatch(/Monarch/)
  })
})
