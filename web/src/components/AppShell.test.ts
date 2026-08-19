import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AppShell from './AppShell.svelte'
import type { ViewController } from '../lib/controller/view-controller.svelte'
import { testProjection } from '../test/projection'
import { testEditingController, testReviewController } from '../test/editing'
import { testProviderWriteController } from '../test/provider-write'

describe('AppShell', () => {
  afterEach(cleanup)
  it('keeps the finance grid primary and routes the TUI cursor shortcut', async () => {
    const controller = stubController()
    render(AppShell, { controller })
    expect(screen.getByRole('main', { name: 'Moneyflow workspace' })).not.toBeNull()
    expect(screen.getByRole('grid', { name: 'Financial results' })).not.toBeNull()
    await fireEvent.keyDown(window, { key: 'j' })
    expect(controller.moveCursor).toHaveBeenCalledWith(1)
    vi.mocked(controller.moveCursor).mockClear()
    await fireEvent.keyDown(screen.getByRole('grid', { name: 'Financial results' }), { key: 'j' })
    expect(controller.moveCursor).toHaveBeenCalledTimes(1)
    expect((screen.getByRole('switch', { name: 'Charts' }) as HTMLInputElement).checked).toBe(true)
  })

  it('keeps the table full width and opens charts in a labelled compact drawer', async () => {
    const original = window.matchMedia
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: (query: string) => ({
        matches: query.includes('640px'),
        media: query,
        onchange: null,
        addEventListener: () => undefined,
        removeEventListener: () => undefined,
        addListener: () => undefined,
        removeListener: () => undefined,
        dispatchEvent: () => false,
      }),
    })
    try {
      const controller = stubController()
      render(AppShell, { controller })
      expect(screen.getByRole('grid', { name: 'Financial results' })).not.toBeNull()
      expect(screen.queryByRole('complementary', { name: 'Visualizations' })).toBeNull()
      await fireEvent.click(screen.getByRole('switch', { name: 'Charts' }))
      expect(screen.getByRole('dialog', { name: 'Moneyflow visualizations' })).not.toBeNull()
      await fireEvent.keyDown(window, { key: 'j' })
      expect(controller.moveCursor).not.toHaveBeenCalled()
    } finally {
      Object.defineProperty(window, 'matchMedia', { configurable: true, value: original })
    }
  })

  it('closes the compact chart scope when resizing to desktop', async () => {
    const original = window.matchMedia
    const target = new EventTarget()
    const media = Object.assign(target, {
      matches: true,
      media: '(max-width: 640px)',
      onchange: null,
      addListener: () => undefined,
      removeListener: () => undefined,
    }) as MediaQueryList
    Object.defineProperty(window, 'matchMedia', { configurable: true, value: () => media })
    try {
      const controller = stubController()
      render(AppShell, { controller })
      await fireEvent.click(screen.getByRole('switch', { name: 'Charts' }))
      expect(screen.getByRole('dialog', { name: 'Moneyflow visualizations' })).not.toBeNull()

      Object.defineProperty(media, 'matches', { configurable: true, value: false })
      media.dispatchEvent(new Event('change'))
      await vi.waitFor(() => {
        expect(screen.queryByRole('dialog', { name: 'Moneyflow visualizations' })).toBeNull()
      })
      await fireEvent.keyDown(window, { key: 'j' })
      expect(controller.moveCursor).toHaveBeenCalledWith(1)
    } finally {
      Object.defineProperty(window, 'matchMedia', { configurable: true, value: original })
    }
  })

  it('routes direct hide, undo, redo, and review keys through the editing surface', async () => {
    const controller = stubController()
    render(AppShell, { controller })
    await fireEvent.keyDown(window, { key: 'h' })
    expect(controller.editing.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        action: 'transaction.toggle-hidden',
        target: { kind: 'transaction', identity: 'row-0' },
      }),
    )
    await fireEvent.keyDown(window, { key: 'u' })
    expect(controller.editing.undo).toHaveBeenCalled()
    await fireEvent.keyDown(window, { key: 'U', shiftKey: true })
    expect(controller.editing.redo).toHaveBeenCalled()
    await fireEvent.keyDown(window, { key: 'w' })
    expect(screen.getByRole('dialog', { name: 'Review pending changes' })).not.toBeNull()
  })

  it('opens duplicate review with D and confirms direct deletion with x', async () => {
    const controller = stubController()
    render(AppShell, { controller })

    await fireEvent.keyDown(window, { key: 'D', shiftKey: true })
    expect(screen.getByRole('dialog', { name: 'Duplicate transactions' })).not.toBeNull()
    await fireEvent.keyDown(window, { key: 'Escape' })
    await vi.waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Duplicate transactions' })).toBeNull(),
    )

    await fireEvent.keyDown(window, { key: 'x' })
    expect(screen.getByRole('dialog', { name: 'Confirm deletion' }).textContent).toContain(
      'Delete 1 transaction?',
    )
    expect(controller.editing.submit).not.toHaveBeenCalledWith(
      expect.objectContaining({ action: 'transaction.delete' }),
    )
    await fireEvent.keyDown(window, { key: 'Enter' })
    expect(controller.editing.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        action: 'transaction.delete',
        target: { kind: 'transaction', identity: 'row-0' },
      }),
    )
  })

  it('routes r through the provider refresh controller', async () => {
    const controller = stubController()
    render(AppShell, { controller })

    await fireEvent.keyDown(window, { key: 'r' })

    expect(controller.provider.refresh).toHaveBeenCalledTimes(1)
  })

  it('opens an editor from the table and restores stable grid focus on cancel', async () => {
    const controller = stubController()
    render(AppShell, { controller })
    const grid = screen.getByRole('grid', { name: 'Financial results' })
    await fireEvent.keyDown(window, { key: 'm' })
    expect(screen.getByRole('dialog', { name: 'Edit merchant' })).not.toBeNull()
    await fireEvent.keyDown(window, { key: 'Escape' })
    await vi.waitFor(() => expect(document.activeElement).toBe(grid))
  })

  it('uses the profile-wide selection count when choosing merchant edit scope', async () => {
    const controller = stubController(testProjection({ selection_count: 1 }))
    render(AppShell, { controller })
    await fireEvent.keyDown(window, { key: 'm' })
    await vi.waitFor(() =>
      expect(screen.getByRole('combobox', { name: /Selected transactions/ })).not.toBeNull(),
    )
  })

  it('does not trap shortcuts when an editor has no focused row', async () => {
    const controller = stubController(testProjection({ total_rows: 0, detail_rows: [] }))
    render(AppShell, { controller })
    await fireEvent.keyDown(window, { key: 'm' })
    expect(screen.queryByRole('dialog', { name: 'Edit merchant' })).toBeNull()
    await fireEvent.keyDown(window, { key: 'j' })
    expect(controller.moveCursor).toHaveBeenCalledWith(1)
  })

  it('keeps overlay scope active when a newer projection rebuilds shortcuts', async () => {
    const initial = stubController()
    const rendered = render(AppShell, { controller: initial })
    await fireEvent.keyDown(window, { key: 'm' })
    expect(screen.getByRole('dialog', { name: 'Edit merchant' })).not.toBeNull()

    const refreshed = stubController(testProjection({ revision: '2' }))
    await rendered.rerender({ controller: refreshed })
    await fireEvent.keyDown(window, { key: 'j' })
    expect(refreshed.moveCursor).not.toHaveBeenCalled()
  })

  it('does not restore grid focus through a newly opened overlay', async () => {
    const callbacks: FrameRequestCallback[] = []
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      callbacks.push(callback)
      return callbacks.length
    })
    vi.stubGlobal('cancelAnimationFrame', (id: number) => {
      callbacks[id - 1] = () => undefined
    })
    try {
      const controller = stubController()
      render(AppShell, { controller })
      const grid = screen.getByRole('grid', { name: 'Financial results' })
      await fireEvent.keyDown(window, { key: 'm' })
      await fireEvent.keyDown(window, { key: 'Escape' })
      await vi.waitFor(() => expect(callbacks.length).toBe(1))
      await vi.waitFor(() =>
        expect(screen.queryByRole('dialog', { name: 'Edit merchant' })).toBeNull(),
      )
      await fireEvent.keyDown(window, { key: '?' })
      screen.getByRole('dialog', { name: 'Keyboard shortcuts' }).focus()
      callbacks[0]?.(0)
      expect(document.activeElement).not.toBe(grid)
      expect(screen.getByRole('dialog', { name: 'Keyboard shortcuts' })).not.toBeNull()
    } finally {
      vi.unstubAllGlobals()
    }
  })
})

function stubController(projection = testProjection()): ViewController {
  return {
    projection: testProjection({
      ...projection,
      capabilities: [
        {
          id: 'cursor.down',
          key_display: '↓/j',
          description: 'Move cursor down',
          category: '',
          available: true,
        },
        ...[
          ['overlay.help', '?'],
          ['transaction.edit-merchant', 'm'],
          ['transaction.edit-category', 'c'],
          ['category.manage', 'C'],
          ['category-group.manage', 'G'],
          ['transaction.toggle-hidden', 'h'],
          ['transaction.delete', 'x'],
          ['view.find-duplicates', 'D'],
          ['changes.undo', 'u'],
          ['changes.redo', 'U'],
          ['changes.review', 'w'],
          ['provider.refresh', 'r'],
        ].map(([id, key_display]) => ({
          id: id!,
          key_display: key_display!,
          description: id!,
          category: 'Actions',
          available: true,
        })),
      ],
    }),
    loading: false,
    announcement: '',
    cursorIdentity: 'row-0',
    cursorIndex: 0,
    problem: undefined,
    editing: testEditingController(),
    review: testReviewController(),
    provider: {
      state: {
        phase: 'idle',
        announcement: '',
        capability: {
          id: 'provider.refresh',
          key_display: 'r',
          description: 'Refresh provider data',
          category: 'System',
          available: true,
        },
      },
      sync: vi.fn(),
      poll: vi.fn(),
      refresh: vi.fn(),
      confirm: vi.fn(),
      dismissConfirmation: vi.fn(),
    },
    providerWrite: testProviderWriteController({
      state: { phase: 'idle', announcement: '' },
      can: vi.fn(() => false),
    }),
    duplicates: {
      state: {
        phase: 'ready',
        cursor: 0,
        announcement: '',
        confirmationCount: 0,
        projection: {
          version: '1',
          revision: '1',
          canonical_query: 'v=1',
          selection: 'mfsel1.empty',
          selection_count: 0,
          total_groups: 0,
          total_transactions: 0,
          window_transactions: 0,
          group_window: { offset: 0, limit: 200, count: 0 },
          row_window: { offset: 0, limit: 200, count: 0 },
          groups: [],
          status: 'No duplicate transactions match the current view.',
        },
      },
      open: vi.fn(async () => true),
      close: vi.fn(),
      move: vi.fn(),
      focus: vi.fn(),
      home: vi.fn(),
      end: vi.fn(),
      page: vi.fn(async () => true),
      focused: vi.fn(),
      toggleFocused: vi.fn(async () => true),
      hideFocused: vi.fn(async () => true),
      requestDelete: vi.fn(),
      cancelDelete: vi.fn(),
      confirmDelete: vi.fn(async () => true),
    },
    hydrate: vi.fn(),
    recheck: vi.fn(),
    moveCursor: vi.fn(),
    moveCursorTo: vi.fn(),
    moveHome: vi.fn(),
    apply: vi.fn(),
    beginSearch: vi.fn(),
    previewSearch: vi.fn(),
    commitSearch: vi.fn(),
    restoreSearch: vi.fn(),
    restore: vi.fn(),
    retry: vi.fn(),
    reset: vi.fn(),
  }
}
