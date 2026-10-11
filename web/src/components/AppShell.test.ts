import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, onTestFinished, vi } from 'vitest'
import AppShell from './AppShell.svelte'
import type { ViewController } from '../lib/controller/view-controller.svelte'
import type { ViewProjection } from '../lib/api/client'
import { testProjection } from '../test/projection'
import { testEditingController, testReviewController } from '../test/editing'
import { testProviderWriteController } from '../test/provider-write'
import { SIDEBAR_FULL_QUERY } from '../lib/shell/layout'
import { testInsights } from '../test/insights'
import { testProviderStatus } from '../test/provider'

describe('AppShell', () => {
  afterEach(() => {
    cleanup()
  })
  it('keeps the finance grid primary and routes the TUI cursor shortcut', async () => {
    const controller = stubController(twoRows(testProjection()))
    render(AppShell, { controller })
    expect(screen.getByRole('main', { name: 'Moneyflow workspace' })).not.toBeNull()
    expect(screen.getByRole('grid', { name: 'Financial results' })).not.toBeNull()
    await fireEvent.keyDown(window, { key: 'j' })
    expect(controller.moveCursor).toHaveBeenCalledWith(1)
    vi.mocked(controller.moveCursor).mockClear()
    await fireEvent.keyDown(screen.getByRole('grid', { name: 'Financial results' }), { key: 'j' })
    expect(controller.moveCursorTo).toHaveBeenCalledWith(1)
    expect(controller.moveCursor).not.toHaveBeenCalled()
  })

  it('focuses the command bar search on / and opens the Filters menu on f', async () => {
    render(AppShell, { controller: stubController() })
    await fireEvent.keyDown(window, { key: '/' })
    expect(document.activeElement).toBe(
      screen.getByRole('searchbox', { name: 'Search transactions' }),
    )
    await fireEvent.keyDown(screen.getByRole('searchbox', { name: 'Search transactions' }), {
      key: 'f',
    })
    expect(screen.queryByRole('button', { name: 'Show hidden transactions' })).toBeNull()
    await fireEvent.keyDown(window, { key: 'f' })
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Filters' }))
    expect(await screen.findByRole('button', { name: 'Show hidden transactions' })).not.toBeNull()
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('starts charts on and hides only the chart row with the switch', async () => {
    render(AppShell, { controller: stubController(charted()) })
    const charts = screen.getByRole('switch', { name: 'Charts' }) as HTMLInputElement
    expect(charts.checked).toBe(true)
    expect(screen.getByRole('region', { name: 'Charts' })).not.toBeNull()
    expect(await screen.findByText('Income')).not.toBeNull()

    await fireEvent.click(charts)
    expect(charts.checked).toBe(false)
    expect(screen.queryByRole('region', { name: 'Charts' })).toBeNull()
    expect(screen.getByText('Income')).not.toBeNull()
  })

  it('keeps the table working when insights fail', async () => {
    const controller = stubController(twoRows(charted()))
    vi.mocked(controller.insights).mockRejectedValue(new Error('insights unavailable'))
    render(AppShell, { controller })
    expect(await screen.findByText(/Couldn't load charts/)).not.toBeNull()
    await fireEvent.keyDown(screen.getByRole('grid', { name: 'Financial results' }), { key: 'j' })
    expect(controller.moveCursorTo).toHaveBeenCalledWith(1)
  })

  it('keeps the table full width and opens the charts in a labelled compact drawer', async () => {
    onTestFinished(stubMedia((query) => query.includes('640px')))
    const controller = stubController(charted())
    render(AppShell, { controller })
    expect(screen.getByRole('grid', { name: 'Financial results' })).not.toBeNull()
    expect(screen.queryByText('Income')).toBeNull()
    expect(controller.insights).not.toHaveBeenCalled()

    await fireEvent.click(screen.getByRole('switch', { name: 'Charts' }))
    const dialog = screen.getByRole('dialog', { name: 'Moneyflow visualizations' })
    expect(await within(dialog).findByText('Income')).not.toBeNull()
    expect(within(dialog).getByRole('region', { name: 'Charts' })).not.toBeNull()
    await fireEvent.keyDown(window, { key: 'j' })
    expect(controller.moveCursor).not.toHaveBeenCalled()
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

  it('lets the write drawer close during a confirmation until the phase changes again', async () => {
    const frames = captureFrames()
    const confirming = () =>
      testProviderWriteController({ state: { phase: 'confirmation', announcement: '' } })
    const controller = { ...stubController(), providerWrite: confirming() }
    const rendered = render(AppShell, { controller })
    const name = 'Monarch write status'
    expect(screen.getByRole('dialog', { name })).not.toBeNull()

    await fireEvent.keyDown(window, { key: 'Escape' })
    await frames.flush()
    expect(screen.queryByRole('dialog', { name })).toBeNull()
    await fireEvent.keyDown(window, { key: 'j' })
    expect(screen.queryByRole('dialog', { name })).toBeNull()

    const idle = testProviderWriteController({ state: { phase: 'idle', announcement: '' } })
    await rendered.rerender({ controller: { ...controller, providerWrite: idle } })
    expect(screen.queryByRole('dialog', { name })).toBeNull()
    await rendered.rerender({ controller: { ...controller, providerWrite: confirming() } })
    expect(screen.getByRole('dialog', { name })).not.toBeNull()
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

  it('opens the sidebar in a drawer below 640 px and returns focus to the menu', async () => {
    onTestFinished(stubMedia((query) => query.includes('640px')))
    const frames = captureFrames()
    const controller = stubController()
    render(AppShell, { controller })
    expect(screen.queryByRole('navigation', { name: 'Workspace' })).toBeNull()
    expect(screen.getAllByText(/1 pending/)).toHaveLength(1)

    const menu = screen.getByRole('button', { name: 'Open navigation' })
    menu.focus()
    await fireEvent.click(menu)
    const drawer = screen.getByRole('dialog', { name: 'Navigation' })
    expect(within(drawer).getByRole('navigation', { name: 'Workspace' })).not.toBeNull()
    expect(drawer.contains(document.activeElement)).toBe(true)
    expect(screen.getAllByText(/1 pending/)).toHaveLength(1)
    await fireEvent.keyDown(window, { key: 'j' })
    expect(controller.moveCursor).not.toHaveBeenCalled()

    await fireEvent.keyDown(window, { key: 'Escape' })
    await frames.flush()
    expect(screen.queryByRole('dialog', { name: 'Navigation' })).toBeNull()
    expect(document.activeElement).toBe(menu)
    expect(controller.apply).not.toHaveBeenCalledWith({ action: 'view.back' })
  })

  it('closes the drawer when an entry is chosen and runs the entry', async () => {
    onTestFinished(stubMedia((query) => query.includes('640px')))
    const controller = stubController()
    render(AppShell, { controller })
    await fireEvent.click(screen.getByRole('button', { name: 'Open navigation' }))
    const drawer = screen.getByRole('dialog', { name: 'Navigation' })
    await fireEvent.click(within(drawer).getByRole('button', { name: 'Categories' }))
    await vi.waitFor(() =>
      expect(controller.apply).toHaveBeenCalledWith({
        action: 'view.set-grouping',
        dimension: 'category',
      }),
    )
    expect(screen.queryByRole('dialog', { name: 'Navigation' })).toBeNull()

    await fireEvent.click(screen.getByRole('button', { name: 'Open navigation' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Duplicates' }))
    await vi.waitFor(() =>
      expect(screen.getByRole('dialog', { name: 'Duplicate transactions' })).not.toBeNull(),
    )
    expect(screen.queryByRole('dialog', { name: 'Navigation' })).toBeNull()
  })

  it('moves the view controls into a View sheet below 640 px', async () => {
    onTestFinished(stubMedia((query) => query.includes('640px')))
    const frames = captureFrames()
    const usd = { currency: 'USD', scale: 2, count: 3, first: '2024-01-01', latest: '2026-10-07' }
    const eur = { currency: 'EUR', scale: 2, count: 1, first: '2025-05-01', latest: '2025-05-01' }
    const controller = stubController(
      testProjection({
        currencies: [usd, eur],
        view: { ...testProjection().view, currency: { currency: 'USD', scale: 2 } },
      }),
    )
    render(AppShell, { controller })
    const banner = screen.getByRole('banner')
    expect(within(banner).queryByRole('radiogroup', { name: 'Period' })).toBeNull()
    expect(within(banner).queryByRole('button', { name: 'Filters' })).toBeNull()
    expect(within(banner).getByRole('button', { name: 'Previous period' })).not.toBeNull()

    const view = within(banner).getByRole('button', { name: 'View' })
    view.focus()
    await fireEvent.click(view)
    const sheet = screen.getByRole('dialog', { name: 'View' })
    await fireEvent.click(within(sheet).getByRole('radio', { name: 'Month' }))
    expect(controller.apply).toHaveBeenCalledWith({
      action: 'time.set-period',
      period: { start: '2026-10-01', end: '2026-10-31' },
    })
    expect(within(sheet).getByRole('combobox', { name: /^Currency/ })).not.toBeNull()
    expect(within(sheet).getByRole('button', { name: 'Filters' })).not.toBeNull()

    expect(sheet.contains(document.activeElement)).toBe(true)
    await fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
    await frames.flush()
    expect(screen.queryByRole('dialog', { name: 'View' })).toBeNull()
    expect(document.activeElement).toBe(view)
  })

  it('opens the Filters menu inside the View sheet on f below 640 px', async () => {
    onTestFinished(stubMedia((query) => query.includes('640px')))
    render(AppShell, { controller: stubController() })
    await fireEvent.keyDown(window, { key: 'f' })
    const sheet = screen.getByRole('dialog', { name: 'View' })
    await vi.waitFor(() =>
      expect(document.activeElement).toBe(within(sheet).getByRole('button', { name: 'Filters' })),
    )
    expect(await screen.findByRole('button', { name: 'Show hidden transactions' })).not.toBeNull()
  })

  it('keeps an applied search in view below 640 px', () => {
    onTestFinished(stubMedia((query) => query.includes('640px')))
    const projection = testProjection()
    render(AppShell, {
      controller: stubController(
        testProjection({ view: { ...projection.view, search: 'noodle' } }),
      ),
    })
    const field = screen.getByRole('searchbox', { name: 'Search transactions' }) as HTMLInputElement
    expect(field.value).toBe('noodle')
    expect(screen.getByRole('button', { name: 'Search' }).getAttribute('aria-pressed')).toBe('true')
  })

  it('reveals search from its icon and returns focus to the icon below 640 px', async () => {
    onTestFinished(stubMedia((query) => query.includes('640px')))
    render(AppShell, { controller: stubController() })
    expect(screen.queryByRole('searchbox', { name: 'Search transactions' })).toBeNull()

    const icon = screen.getByRole('button', { name: 'Search' })
    icon.focus()
    await fireEvent.click(icon)
    const field = await screen.findByRole('searchbox', { name: 'Search transactions' })
    expect(document.activeElement).toBe(field)
    expect(icon.getAttribute('aria-pressed')).toBe('true')

    await fireEvent.keyDown(field, { key: 'Escape' })
    await vi.waitFor(() => expect(document.activeElement).toBe(icon))
    expect(screen.queryByRole('searchbox', { name: 'Search transactions' })).toBeNull()

    await fireEvent.keyDown(window, { key: '/' })
    const keyed = await screen.findByRole('searchbox', { name: 'Search transactions' })
    expect(document.activeElement).toBe(keyed)
    await fireEvent.keyDown(keyed, { key: 'Escape' })
    await vi.waitFor(() =>
      expect(document.activeElement).toBe(screen.getByRole('grid', { name: 'Financial results' })),
    )
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
    const confirmation = await screen.findByRole('alertdialog', { name: 'Confirm deletion' })
    expect(confirmation.textContent).toContain('Delete 1 transaction?')
    expect(controller.editing.submit).not.toHaveBeenCalledWith(
      expect.objectContaining({ action: 'transaction.delete' }),
    )
    expect(confirmation.contains(document.activeElement)).toBe(true)
    await fireEvent.keyDown(document.activeElement!, { key: 'Enter' })
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
    const field = screen.getByRole('combobox', { name: 'Merchant' })
    await vi.waitFor(() => expect(document.activeElement).toBe(field))
    await fireEvent.keyDown(field, { key: 'Escape' })
    await vi.waitFor(() => expect(document.activeElement).toBe(grid))
    expect(screen.queryByRole('dialog', { name: 'Edit merchant' })).toBeNull()
  })

  it('uses the profile-wide selection count when choosing merchant edit scope', async () => {
    const controller = stubController(testProjection({ selection_count: 1 }))
    render(AppShell, { controller })
    await fireEvent.keyDown(window, { key: 'm' })
    await vi.waitFor(() =>
      expect(screen.getByRole('radio', { name: /selected transactions/ })).not.toBeNull(),
    )
    expect(screen.getByRole('radio', { name: 'Whole merchant' }).hasAttribute('disabled')).toBe(
      true,
    )
    expect(screen.getByText('Clear the selection to rename the whole merchant')).not.toBeNull()
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
      await fireEvent.keyDown(screen.getByRole('combobox', { name: 'Merchant' }), {
        key: 'Escape',
      })
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

/** A projection in one currency, so the charts load insights for it. */
function charted() {
  const base = testProjection()
  return testProjection({ view: { ...base.view, currency: { currency: 'USD', scale: 2 } } })
}

/** `projection` with a second row, so the cursor has a row to move down to. */
function twoRows(projection: ViewProjection): ViewProjection {
  const first = projection.detail_rows![0]!
  return {
    ...projection,
    total_rows: 2,
    window: { offset: 0, limit: 200, count: 2 },
    detail_rows: [first, { ...first, index: 1, identity: 'row-1' }],
  }
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
          ['overlay.search', '/'],
          ['overlay.filters', 'f'],
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
    neighbors: [],
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
    showIndex: vi.fn(),
    showAnchor: vi.fn(),
    recheckNewer: vi.fn(),
    insights: vi.fn(async (body) => testInsights({ query: body.query, revision: '0' })),
    apply: vi.fn(),
    beginSearch: vi.fn(),
    previewSearch: vi.fn(),
    commitSearch: vi.fn(),
    restoreSearch: vi.fn(),
    hold: <T>(work: Promise<T>) => work,
    restore: vi.fn(),
    retry: vi.fn(),
    reset: vi.fn(),
  }
}
