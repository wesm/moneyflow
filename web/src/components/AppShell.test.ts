import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, onTestFinished, vi } from 'vitest'
import AppShell from './AppShell.svelte'
import type { ViewController } from '../lib/controller/view-controller.svelte'
import { testProjection } from '../test/projection'
import { testEditingController, testReviewController } from '../test/editing'
import { testProviderWriteController } from '../test/provider-write'
import { loadRail } from '../lib/shell/rail'
import { SIDEBAR_FULL_QUERY } from '../lib/shell/layout'
import { testProviderStatus } from '../test/provider'

vi.mock('../lib/shell/rail', async (importOriginal) => {
  const original = await importOriginal<typeof import('../lib/shell/rail')>()
  return { loadRail: vi.fn(original.loadRail) }
})

const failureText = 'Charts could not load. Reload the page.'

describe('AppShell', () => {
  afterEach(() => {
    cleanup()
    vi.mocked(loadRail).mockClear()
  })
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
  })

  it('starts charts off at rail widths so the table keeps the full width', async () => {
    render(AppShell, { controller: stubController() })
    const charts = screen.getByRole('switch', { name: 'Charts' }) as HTMLInputElement
    expect(charts.checked).toBe(false)
    expect(screen.queryByRole('complementary', { name: 'Visualizations' })).toBeNull()
    await fireEvent.click(charts)
    expect(screen.getByRole('complementary', { name: 'Visualizations' })).not.toBeNull()
  })

  it('starts charts on with the full sidebar', () => {
    onTestFinished(stubMedia((query) => query === SIDEBAR_FULL_QUERY))
    render(AppShell, { controller: stubController() })
    expect((screen.getByRole('switch', { name: 'Charts' }) as HTMLInputElement).checked).toBe(true)
    expect(screen.getByRole('complementary', { name: 'Visualizations' })).not.toBeNull()
  })

  it('loads the chart rail into the Visualizations region', async () => {
    onTestFinished(stubMedia((query) => query === SIDEBAR_FULL_QUERY))
    render(AppShell, { controller: stubController() })

    const region = screen.getByRole('complementary', { name: 'Visualizations' })
    // The first dynamic import compiles the chart library, which is slow in a cold test run.
    const heading = await within(region).findByRole(
      'heading',
      { name: 'Visualizations' },
      { timeout: 10_000 },
    )
    expect(heading.tagName).toBe('H2')
  })

  it('shows a loading line until the chart rail arrives', async () => {
    vi.mocked(loadRail).mockImplementationOnce(() => new Promise(() => undefined))
    onTestFinished(stubMedia((query) => query === SIDEBAR_FULL_QUERY))
    render(AppShell, { controller: stubController() })

    const region = screen.getByRole('complementary', { name: 'Visualizations' })
    expect(within(region).getByText('Loading charts…')).not.toBeNull()
    expect(chartAnnouncement().textContent).toBe('Loading charts…')
  })

  it('explains a failed chart load and retries when charts are shown again', async () => {
    vi.mocked(loadRail).mockRejectedValueOnce(new Error('chunk 404'))
    onTestFinished(stubMedia((query) => query === SIDEBAR_FULL_QUERY))
    render(AppShell, { controller: stubController() })

    const errors = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const region = screen.getByRole('complementary', { name: 'Visualizations' })
    expect(await within(region).findByText(failureText)).not.toBeNull()
    expect(within(region).queryByText('Loading charts…')).toBeNull()
    expect(chartAnnouncement().textContent).toBe(failureText)
    expect(errors).toHaveBeenCalledWith('moneyflow: chart rail failed to load', expect.any(Error))
    errors.mockRestore()

    const toggle = screen.getByRole('switch', { name: 'Charts' })
    await fireEvent.click(toggle)
    await fireEvent.click(toggle)
    const heading = await screen.findByRole(
      'heading',
      { name: 'Visualizations' },
      { timeout: 10_000 },
    )
    expect(heading.tagName).toBe('H2')
    expect(screen.queryByText(failureText)).toBeNull()
    expect(loadRail).toHaveBeenCalledTimes(2)
  })

  it('explains a failed chart load inside the compact dialog', async () => {
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
      vi.mocked(loadRail).mockRejectedValueOnce(new Error('chunk 404'))
      vi.spyOn(console, 'error').mockImplementationOnce(() => undefined)
      render(AppShell, { controller: stubController() })
      await fireEvent.click(screen.getByRole('switch', { name: 'Charts' }))
      const dialog = screen.getByRole('dialog', { name: 'Moneyflow visualizations' })
      expect(await within(dialog).findByText(failureText)).not.toBeNull()
      expect(chartAnnouncement().textContent).toBe(failureText)
    } finally {
      Object.defineProperty(window, 'matchMedia', { configurable: true, value: original })
    }
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
      const dialog = screen.getByRole('dialog', { name: 'Moneyflow visualizations' })
      expect(await within(dialog).findByRole('heading', { name: 'Visualizations' })).not.toBeNull()
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

  it('frames the workspace with the full sidebar at 1280 px and wider', async () => {
    const restore = stubMedia((query) => query === SIDEBAR_FULL_QUERY)
    try {
      const controller = stubController()
      render(AppShell, { controller })
      const sidebar = screen.getByRole('complementary', { name: 'Moneyflow sidebar' })
      const nav = within(sidebar).getByRole('navigation', { name: 'Workspace' })
      expect(within(screen.getByRole('banner')).queryByText('Moneyflow')).toBeNull()
      expect(screen.getByRole('separator', { name: 'Resize sidebar' })).not.toBeNull()
      expect(screen.getAllByText(/1 pending/)).toHaveLength(1)
      expect(within(sidebar).getByText(/1 pending/)).not.toBeNull()

      await fireEvent.click(within(nav).getByRole('button', { name: 'Categories' }))
      await vi.waitFor(() =>
        expect(controller.apply).toHaveBeenCalledWith({
          action: 'view.set-grouping',
          dimension: 'category',
        }),
      )
      await vi.waitFor(() =>
        expect(document.activeElement).toBe(
          screen.getByRole('grid', { name: 'Financial results' }),
        ),
      )
    } finally {
      restore()
    }
  })

  it('opens overlays from Maintain entries and returns focus to the entry on close', async () => {
    onTestFinished(stubMedia((query) => query === SIDEBAR_FULL_QUERY))
    const frames = captureFrames()
    render(AppShell, { controller: stubController() })
    const nav = screen.getByRole('navigation', { name: 'Workspace' })
    const duplicates = within(nav).getByRole('button', { name: 'Duplicates' })
    duplicates.focus()
    await fireEvent.click(duplicates)
    const dialog = screen.getByRole('dialog', { name: 'Duplicate transactions' })
    dialog.focus()
    await fireEvent.keyDown(window, { key: 'Escape' })
    await frames.flush()
    expect(screen.queryByRole('dialog', { name: 'Duplicate transactions' })).toBeNull()
    expect(document.activeElement).toBe(duplicates)

    await fireEvent.click(within(nav).getByRole('button', { name: 'Edit groups' }))
    expect(screen.getByRole('dialog', { name: 'Manage category groups' })).not.toBeNull()
  })

  it('returns focus to the grid when the opener is gone', async () => {
    onTestFinished(stubMedia((query) => query === SIDEBAR_FULL_QUERY))
    const frames = captureFrames()
    render(AppShell, { controller: stubController() })
    const grid = screen.getByRole('grid', { name: 'Financial results' })
    const duplicates = screen.getByRole('button', { name: 'Duplicates' })
    duplicates.focus()
    await fireEvent.click(duplicates)
    screen.getByRole('dialog', { name: 'Duplicate transactions' }).focus()
    duplicates.remove()
    await fireEvent.keyDown(window, { key: 'Escape' })
    await frames.flush()
    expect(document.activeElement).toBe(grid)
  })

  it('shows the icon rail between 640 and 1279 px and moves pending text to the status line', () => {
    render(AppShell, { controller: stubController() })
    expect(screen.queryByRole('separator', { name: 'Resize sidebar' })).toBeNull()
    const rail = screen.getByRole('complementary', { name: 'Moneyflow sidebar' })
    const nav = within(rail).getByRole('navigation', { name: 'Workspace' })
    expect(within(nav).getByRole('button', { name: 'Merchants' })).not.toBeNull()
    const pending = screen.getAllByText(/1 pending/)
    expect(pending).toHaveLength(1)
    expect(pending[0]?.closest('footer')).not.toBeNull()
  })

  it('renders no sidebar or rail below 640 px', () => {
    const restore = stubMedia((query) => query.includes('640px'))
    try {
      render(AppShell, { controller: stubController() })
      expect(screen.queryByRole('navigation', { name: 'Workspace' })).toBeNull()
      expect(screen.getAllByText(/1 pending/)).toHaveLength(1)
    } finally {
      restore()
    }
  })

  it('reports the connection and profile revision in the status line', async () => {
    const connected = stubController()
    const rendered = render(AppShell, { controller: connected })
    expect(screen.getByText('profile revision 1')).not.toBeNull()
    expect(screen.getByText('Connected')).not.toBeNull()

    const offline = { ...stubController(), problem: { kind: 'request', code: 'network_error' } }
    await rendered.rerender({ controller: offline as ViewController })
    expect(screen.getByText('Offline')).not.toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(offline.retry).toHaveBeenCalledTimes(1)
  })

  it('hints the keys of the current view', () => {
    render(AppShell, { controller: stubController() })
    const footer = screen.getByText('profile revision 1').closest('footer')!
    expect(within(footer).getByText('Merchant')).not.toBeNull()
    expect(within(footer).getByText('Help')).not.toBeNull()
  })

  it('shows the provider confirmation at every width', () => {
    const restore = stubMedia((query) => query.includes('640px'))
    try {
      const controller = stubController()
      controller.provider.state.phase = 'confirmation'
      controller.provider.state.status = testProviderStatus({ confirmation_token: 'opaque' })
      render(AppShell, { controller })
      expect(screen.getByRole('dialog', { name: 'Confirm provider refresh' })).not.toBeNull()
    } finally {
      restore()
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

  it('opens committed export with E and leaves view state unchanged when closed', async () => {
    const controller = stubController()
    const historyBefore = history.state
    const urlBefore = location.href
    render(AppShell, { controller })
    const grid = screen.getByRole('grid', { name: 'Financial results' })

    await fireEvent.keyDown(window, { key: 'E', shiftKey: true })

    expect(controller.export.open).toHaveBeenCalledWith('v=1')
    expect(screen.getByRole('dialog', { name: 'Export transactions' })).not.toBeNull()
    await fireEvent.keyDown(window, { key: 'Escape' })
    await vi.waitFor(() => expect(document.activeElement).toBe(grid))
    expect(location.href).toBe(urlBefore)
    expect(history.state).toBe(historyBefore)
    expect(controller.cursorIndex).toBe(0)
    expect(controller.projection?.selection_count).toBe(0)
  })

  it('announces empty committed export without mounting the chooser', async () => {
    const controller = stubController()
    controller.export.state.announcement = 'No data to export.'
    vi.mocked(controller.export.open).mockResolvedValue(false)
    render(AppShell, { controller })

    await fireEvent.keyDown(window, { key: 'E', shiftKey: true })

    expect(screen.queryByRole('dialog', { name: 'Export transactions' })).toBeNull()
    expect(screen.getByText('No data to export.')).not.toBeNull()
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

/** Holds animation frames until `flush`, so a test can check focus after the restore frame. */
function captureFrames(): { flush: () => Promise<void> } {
  const callbacks: FrameRequestCallback[] = []
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    callbacks.push(callback)
    return callbacks.length
  })
  vi.stubGlobal('cancelAnimationFrame', (id: number) => {
    callbacks[id - 1] = () => undefined
  })
  onTestFinished(() => {
    vi.unstubAllGlobals()
  })
  return {
    flush: async () => {
      await vi.waitFor(() => expect(callbacks.length).toBeGreaterThan(0))
      for (const callback of callbacks.splice(0)) callback(0)
    },
  }
}

function chartAnnouncement(): HTMLElement {
  const region = document.querySelector<HTMLElement>('[data-testid="chart-announcement"]')
  if (!region) throw new Error('chart announcement region is missing')
  return region
}

function stubMedia(matches: (query: string) => boolean): () => void {
  const original = window.matchMedia
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: (query: string) => ({
      matches: matches(query),
      media: query,
      onchange: null,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      addListener: () => undefined,
      removeListener: () => undefined,
      dispatchEvent: () => false,
    }),
  })
  return () => Object.defineProperty(window, 'matchMedia', { configurable: true, value: original })
}

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
          ['view.set-grouping', '1-5'],
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
          ['transactions.export', 'E'],
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
    export: {
      state: {
        phase: 'ready',
        format: 'parquet',
        scope: 'full',
        count: 1,
        announcement: '',
        filename: undefined,
        canCancel: true,
        preview: {
          version: '2',
          revision: '1',
          full_count: 1,
          filtered_count: 1,
          active_operations: 0,
          inactive_operations: 0,
          commit_available: true,
          temporary_profile: false,
          canonical_query: 'v=1',
        },
      },
      open: vi.fn(async () => true),
      close: vi.fn(),
      setFormat: vi.fn(),
      setScope: vi.fn(),
      export: vi.fn(async () => true),
      cancel: vi.fn(),
    },
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
