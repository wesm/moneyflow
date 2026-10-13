import { dismissFlash } from '@kenn-io/kit-ui'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, onTestFinished, vi } from 'vitest'
import { tick } from 'svelte'
import AppShell from './AppShell.svelte'
import type { TransactionInformationResponse, ViewProjection } from '../lib/api/client'
import { createProviderController } from '../lib/controller/provider'
import type { ViewController } from '../lib/controller/view-controller.svelte'
import { testAggregateRow, testProjection } from '../test/projection'
import { testEditingController, testReviewController } from '../test/editing'
import { testProviderWriteController } from '../test/provider-write'
import { SIDEBAR_FULL_QUERY } from '../lib/shell/layout'
import { testInsights } from '../test/insights'
import { testProviderStatus } from '../test/provider'

const escapeCapability = {
  id: 'view.back',
  key_display: 'esc',
  description: 'Go back',
  category: 'Navigation',
  available: true,
}

describe('AppShell', () => {
  afterEach(() => {
    cleanup()
    dismissFlash()
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

  it('leaves focus in search when a grouping finishes, until Enter returns it', async () => {
    const controller = stubController()
    let finish!: (applied: boolean) => void
    vi.mocked(controller.apply).mockImplementationOnce(
      () => new Promise<boolean>((resolve) => (finish = resolve)),
    )
    render(AppShell, { controller })
    const search = screen.getByRole('searchbox', { name: 'Search transactions' })

    await fireEvent.keyDown(window, { key: '2' })
    await vi.waitFor(() => expect(controller.apply).toHaveBeenCalled())
    await fireEvent.keyDown(window, { key: '/' })
    expect(document.activeElement).toBe(search)
    finish(true)
    await tick()
    await tick()
    expect(document.activeElement).toBe(search)

    await fireEvent.keyDown(search, { key: 'Enter' })
    await vi.waitFor(() =>
      expect(document.activeElement).toBe(screen.getByRole('grid', { name: 'Financial results' })),
    )
  })

  it('leaves focus in search when a breadcrumb return finishes', async () => {
    const controller = stubController(
      testProjection({
        canonical_query: 'v=1&mode=detail&drill=merchant%3AUSD%3A2%3Aexample-key',
        view: {
          ...testProjection().view,
          mode: 'detail',
          drill_depth: 1,
          root: { mode: 'aggregate', grouping: 'merchant', time_granularity: 'year' },
        },
        breadcrumbs: [{ dimension: 'merchant', label: 'Corner Grocery' }],
      }),
    )
    let finish!: (applied: boolean) => void
    vi.mocked(controller.apply).mockImplementationOnce(
      () => new Promise<boolean>((resolve) => (finish = resolve)),
    )
    render(AppShell, { controller })
    const search = screen.getByRole('searchbox', { name: 'Search transactions' })

    const trail = within(screen.getByRole('navigation', { name: 'Breadcrumb' }))
    await fireEvent.click(trail.getByRole('button', { name: 'Merchants' }))
    expect(controller.apply).toHaveBeenCalledWith({ action: 'view.back-to', depth: 0 })
    await fireEvent.keyDown(window, { key: '/' })
    expect(document.activeElement).toBe(search)
    finish(true)
    await tick()
    await tick()
    expect(document.activeElement).toBe(search)
  })

  it('runs a command-bar period change after the table toggles queued before it', async () => {
    const controller = heldFirstApply(stubController(periodProjection()))
    render(AppShell, { controller })

    await fireEvent.keyDown(window, { key: ' ' })
    await fireEvent.keyDown(window, { key: ' ' })
    await fireEvent.click(screen.getByRole('button', { name: 'Previous period' }))
    controller.finish()

    await vi.waitFor(() => expect(controller.apply).toHaveBeenCalledTimes(3))
    expect(vi.mocked(controller.apply).mock.calls.map(([action]) => action.action)).toEqual([
      'selection.toggle',
      'selection.toggle',
      'time.previous-period',
    ])
  })

  it('moves the cursor only after a pending Esc return restores the parent view', async () => {
    const controller = heldFirstApply(stubController(twoRows(testProjection())))
    controller.projection!.capabilities!.push(
      escapeCapability,
      {
        id: 'cursor.home',
        key_display: 'Home/T',
        description: 'Move to the first row',
        category: '',
        available: true,
      },
      {
        id: 'cursor.end',
        key_display: 'End/B',
        description: 'Move to the last row',
        category: '',
        available: true,
      },
    )
    const moves: string[] = []
    vi.mocked(controller.moveCursorTo).mockImplementation(async (index) => {
      moves.push(`to ${index}`)
    })
    vi.mocked(controller.moveCursor).mockImplementation(async (delta) => {
      moves.push(`move ${delta}`)
    })
    vi.mocked(controller.moveHome).mockImplementation(async () => {
      moves.push('home')
    })
    render(AppShell, { controller })
    const grid = screen.getByRole('grid', { name: 'Financial results' })

    await fireEvent.keyDown(grid, { key: 'Escape' })
    await vi.waitFor(() => expect(controller.apply).toHaveBeenCalledWith({ action: 'view.back' }))
    await fireEvent.keyDown(grid, { key: 'ArrowDown' })
    await fireEvent.keyDown(window, { key: 'j' })
    await fireEvent.keyDown(window, { key: 'Home' })
    await fireEvent.keyDown(window, { key: 'End' })
    await tick()
    expect(moves).toEqual([])
    controller.finish()

    await vi.waitFor(() => expect(moves).toEqual(['to 1', 'move 1', 'home', 'to 1']))
  })

  it('anchors a period segment on the view a pending period change returns', async () => {
    const controller = heldFirstApply(stubController(periodProjection()))
    render(AppShell, { controller })

    await fireEvent.click(screen.getByRole('button', { name: 'Previous period' }))
    await fireEvent.click(screen.getByRole('radio', { name: 'Month' }))
    showPeriod(controller, { from: '2025-01-01', to: '2025-12-31' })
    controller.finish()

    await vi.waitFor(() => expect(controller.apply).toHaveBeenCalledTimes(2))
    expect(controller.apply).toHaveBeenLastCalledWith({
      action: 'time.set-period',
      period: { start: '2025-12-01', end: '2025-12-31' },
    })
  })

  it('applies All time chosen while a period change from all time is pending', async () => {
    const shown = stubController(periodProjection())
    showPeriod(shown, undefined)
    const controller = heldFirstApply(shown)
    render(AppShell, { controller })

    await fireEvent.click(screen.getByRole('radio', { name: 'Month' }))
    await fireEvent.click(screen.getByRole('button', { name: 'All time' }))
    const chooser = within(screen.getByRole('dialog', { name: 'Choose period' }))
    await fireEvent.click(chooser.getByRole('button', { name: /All time/ }))
    showPeriod(controller, { from: '2026-10-01', to: '2026-10-31' })
    controller.finish()

    await vi.waitFor(() => expect(controller.apply).toHaveBeenCalledTimes(2))
    expect(vi.mocked(controller.apply).mock.calls.map(([action]) => action)).toEqual([
      { action: 'time.set-period', period: { start: '2026-10-01', end: '2026-10-31' } },
      { action: 'time.clear-period' },
    ])
  })

  it('starts a search typed behind queued view changes from their result', async () => {
    const controller = heldFirstApply(stubController(periodProjection()))
    render(AppShell, { controller })

    await fireEvent.keyDown(window, { key: ' ' })
    await fireEvent.keyDown(window, { key: ' ' })
    await fireEvent.click(screen.getByRole('button', { name: 'Previous period' }))
    const search = screen.getByRole('searchbox', { name: 'Search transactions' })
    search.focus()
    await fireEvent.input(search, { target: { value: 'coffee' } })
    await tick()
    expect(controller.beginSearch).not.toHaveBeenCalled()
    controller.finish()

    await vi.waitFor(() => expect(controller.beginSearch).toHaveBeenCalled())
    const [lastApply] = vi.mocked(controller.apply).mock.invocationCallOrder.slice(-1)
    expect(vi.mocked(controller.apply)).toHaveBeenCalledTimes(3)
    expect(vi.mocked(controller.beginSearch).mock.invocationCallOrder[0]).toBeGreaterThan(
      lastApply!,
    )
  })

  it('leaves the search field after a search with no matches so Esc can clear it', async () => {
    const base = testProjection()
    const controller = stubController(
      testProjection({
        view: { ...base.view, search: 'no such merchant' },
        total_rows: 0,
        window: { offset: 0, limit: 200, count: 0 },
        detail_rows: [],
      }),
    )
    controller.projection!.capabilities!.push(escapeCapability)
    vi.mocked(controller.previewSearch).mockResolvedValue('applied')
    render(AppShell, { controller })
    const search = screen.getByRole('searchbox', { name: 'Search transactions' })
    search.focus()
    await fireEvent.input(search, { target: { value: 'no such merchant' } })
    await vi.waitFor(() => expect(controller.previewSearch).toHaveBeenCalled())

    await fireEvent.keyDown(search, { key: 'Enter' })
    await vi.waitFor(() => expect(controller.commitSearch).toHaveBeenCalled())
    await vi.waitFor(() => expect(document.activeElement).not.toBe(search))
    await fireEvent.keyDown(document.activeElement!, { key: 'Escape' })

    await vi.waitFor(() => expect(controller.apply).toHaveBeenCalledWith({ action: 'view.back' }))
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

  it('loads an off-window bar’s row only after a pending Space toggle', async () => {
    const { controller, release } = heldToggle()
    vi.mocked(controller.insights).mockImplementation(async (body) => {
      const response = testInsights({ query: body.query, revision: '0' })
      response.summary!.composition = {
        spending: [
          { key: 'a', identity: 'merchant:a', label: 'Merchant A', net: '-300', share_tenths: 600 },
          { key: 'c', identity: 'merchant:c', label: 'Merchant C', net: '-200', share_tenths: 400 },
        ],
        income: [],
      }
      return response
    })
    render(AppShell, { controller })
    const list = await screen.findByRole('list', { name: 'Where it went' })

    await fireEvent.keyDown(window, { key: ' ' })
    expect(controller.apply).toHaveBeenCalledWith({
      action: 'selection.toggle',
      target: { identity: 'merchant:a', kind: 'aggregate' },
    })
    await fireEvent.click(within(list).getByRole('button', { name: /^Merchant C,/ }))
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(controller.showAnchor).not.toHaveBeenCalled()

    release()
    await vi.waitFor(() => expect(controller.showAnchor).toHaveBeenCalledWith('merchant:c'))
  })

  it('rechecks the view for newer insights only after a pending Space toggle', async () => {
    const { controller, release } = heldToggle()
    let answer!: () => void
    vi.mocked(controller.insights).mockImplementation(
      (body) =>
        new Promise((resolve) => {
          answer = () => resolve(testInsights({ query: body.query, revision: '1' }))
        }),
    )
    render(AppShell, { controller })
    await vi.waitFor(() => expect(controller.insights).toHaveBeenCalled())

    await fireEvent.keyDown(window, { key: ' ' })
    expect(controller.apply).toHaveBeenCalledTimes(1)
    answer()
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(controller.recheckNewer).not.toHaveBeenCalled()

    release()
    await vi.waitFor(() => expect(controller.recheckNewer).toHaveBeenCalledWith('1'))
  })

  it('still opens an editor queued after a newer-insights recheck fails', async () => {
    const controller = stubController(charted())
    vi.mocked(controller.insights).mockImplementation(async (body) =>
      testInsights({ query: body.query, revision: '1' }),
    )
    vi.mocked(controller.recheckNewer).mockRejectedValue(new Error('recheck failed'))
    render(AppShell, { controller })
    await vi.waitFor(() => expect(controller.recheckNewer).toHaveBeenCalledWith('1'))

    await fireEvent.keyDown(window, { key: 'm' })
    expect(await screen.findByRole('dialog', { name: 'Edit merchant' })).not.toBeNull()
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
    await vi.waitFor(() =>
      expect(controller.apply).toHaveBeenCalledWith({
        action: 'time.set-period',
        period: { start: '2026-10-01', end: '2026-10-31' },
      }),
    )
    expect(within(sheet).getByRole('combobox', { name: /^Currency/ })).not.toBeNull()
    expect(within(sheet).getByRole('button', { name: 'Filters' })).not.toBeNull()

    expect(sheet.contains(document.activeElement)).toBe(true)
    await fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
    await frames.flush()
    expect(screen.queryByRole('dialog', { name: 'View' })).toBeNull()
    expect(document.activeElement).toBe(view)
  })

  it('keeps the currency row in the View sheet when the view currency has no rows left', async () => {
    onTestFinished(stubMedia((query) => query.includes('640px')))
    const usd = { currency: 'USD', scale: 2, count: 3, first: '2024-01-01', latest: '2026-10-07' }
    const controller = stubController(
      testProjection({
        currencies: [usd],
        view: { ...testProjection().view, currency: { currency: 'EUR', scale: 2 } },
      }),
    )
    render(AppShell, { controller })
    await fireEvent.click(within(screen.getByRole('banner')).getByRole('button', { name: 'View' }))
    const sheet = screen.getByRole('dialog', { name: 'View' })
    expect(within(sheet).getByRole('combobox', { name: 'Currency: EUR' })).not.toBeNull()
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
      controller.provider.state.dialog = { view: 'confirmation', startedAt: 0 }
      render(AppShell, { controller })
      expect(screen.getByRole('dialog', { name: 'Confirm provider refresh' })).not.toBeNull()
    } finally {
      restore()
    }
  })

  it('makes the workspace inert and keyless while the refresh dialog is open', async () => {
    const controller = stubController()
    controller.provider.state.phase = 'refreshing'
    controller.provider.state.status = testProviderStatus({ provider_kind: 'monarch' })
    controller.provider.state.dialog = { view: 'progress', startedAt: Date.now() }
    const { container } = render(AppShell, { controller })
    expect(screen.getByRole('dialog', { name: 'Refreshing from Monarch' })).not.toBeNull()
    expect(container.querySelector<HTMLElement>('.app-shell')?.inert).toBe(true)
    await fireEvent.keyDown(window, { key: 'h' })
    expect(controller.editing.submit).not.toHaveBeenCalled()
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
    await vi.waitFor(() => expect(controller.editing.redo).toHaveBeenCalled())
    await fireEvent.keyDown(window, { key: 'w' })
    expect(screen.getByRole('dialog', { name: 'Review pending changes' })).not.toBeNull()
  })

  it.each([
    ['u', 'undo', false],
    ['U', 'redo', true],
  ] as const)('runs %s after a hide queued behind a slow toggle', async (key, method, shiftKey) => {
    const controller = stubController()
    controller.projection!.capabilities!.push({
      id: 'selection.toggle',
      key_display: 'space',
      description: 'selection.toggle',
      category: 'Views',
      available: true,
    })
    let toggled!: () => void
    vi.mocked(controller.apply).mockImplementationOnce(
      () => new Promise<boolean>((resolve) => (toggled = () => resolve(true))),
    )
    render(AppShell, { controller })
    await fireEvent.keyDown(window, { key: ' ' })
    await fireEvent.keyDown(window, { key: 'h' })
    await fireEvent.keyDown(window, { key, shiftKey })
    await vi.waitFor(() => expect(controller.apply).toHaveBeenCalledTimes(1))
    expect(controller.editing[method]).not.toHaveBeenCalled()

    toggled()
    await vi.waitFor(() => expect(controller.editing[method]).toHaveBeenCalledTimes(1))
    const [hidden] = vi.mocked(controller.editing.submit).mock.invocationCallOrder
    expect(hidden).toBeLessThan(vi.mocked(controller.editing[method]).mock.invocationCallOrder[0]!)
  })

  it('shows the last Enter’s transaction when an earlier one’s details arrive later', async () => {
    const { controller, waiting } = lateInformation()
    render(AppShell, { controller })
    await fireEvent.keyDown(window, { key: 'Enter' })
    Object.assign(controller, { cursorIndex: 1, cursorIdentity: 'row-1' })
    await fireEvent.keyDown(window, { key: 'Enter' })
    await new Promise((resolve) => setTimeout(resolve))

    await expectLastInformation(waiting)
  })

  it('shows the last tapped phone row’s transaction when an earlier one’s arrive later', async () => {
    onTestFinished(stubMedia((query) => query.includes('640px')))
    const { controller, waiting } = lateInformation()
    render(AppShell, { controller })
    const merchant = (index: number) =>
      document.querySelector(`[data-index="${index}"] [data-column="merchant"]`)!
    await fireEvent.click(merchant(0))
    await fireEvent.click(merchant(1))
    await new Promise((resolve) => setTimeout(resolve))

    await expectLastInformation(waiting)
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

  it('gives keys to the refresh dialog while it covers duplicate review', async () => {
    const stub = stubController()
    const { provider, aborted, status } = blockedRefresh(stub.projection!)
    const controller = { ...stub, provider }
    render(AppShell, { controller })
    await fireEvent.keyDown(window, { key: 'D', shiftKey: true })
    expect(screen.getByRole('dialog', { name: 'Duplicate transactions' })).not.toBeNull()

    void provider.refresh()
    const refresh = await screen.findByRole('dialog', { name: 'Refreshing from Monarch' })
    await vi.waitFor(() => expect(refresh.contains(document.activeElement)).toBe(true))
    for (const key of ['h', 'x', 'j', ' ', 'Enter']) {
      await fireEvent.keyDown(document.activeElement!, { key })
    }
    expect(controller.duplicates.hideFocused).not.toHaveBeenCalled()
    expect(controller.duplicates.requestDelete).not.toHaveBeenCalled()
    expect(controller.duplicates.move).not.toHaveBeenCalled()
    expect(controller.duplicates.toggleFocused).not.toHaveBeenCalled()

    await fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
    expect(aborted()).toBe(true)
    expect(controller.duplicates.close).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog', { name: 'Duplicate transactions' })).not.toBeNull()

    status.mockResolvedValue(testProviderStatus({ provider_kind: 'monarch' }))
    await provider.poll()
    await vi.waitFor(() => expect(screen.queryByRole('dialog', { name: /^Refreshing/ })).toBeNull())
    await fireEvent.keyDown(window, { key: 'h' })
    expect(controller.duplicates.hideFocused).toHaveBeenCalledOnce()
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
    await vi.waitFor(() =>
      expect(document.querySelector('.kit-flash-banner')?.textContent).toContain(
        'No data to export.',
      ),
    )
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

  it('clears the selection without going back once a queued toggle empties it', async () => {
    const controller = stubController(testProjection({ selection_count: 1 }))
    let deselect!: () => void
    vi.mocked(controller.apply).mockImplementationOnce(
      () =>
        new Promise<boolean>((resolve) => {
          deselect = () => {
            controller.projection!.selection_count = 0
            resolve(true)
          }
        }),
    )
    render(AppShell, { controller })
    const box = document.querySelector<HTMLElement>('[role="gridcell"][data-column="select"]')!
    await fireEvent.click(box)
    await fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    await vi.waitFor(() =>
      expect(controller.apply).toHaveBeenCalledWith({
        action: 'selection.toggle',
        target: { identity: 'row-0', kind: 'transaction' },
      }),
    )

    deselect()
    // The queued Clear runs in the microtasks after the toggle settles.
    await new Promise((resolve) => setTimeout(resolve))
    expect(controller.apply).toHaveBeenCalledTimes(1)
    expect(document.activeElement).toBe(screen.getByRole('grid', { name: 'Financial results' }))
  })

  it('toggles the cursor row of the new view when Space follows a queued view change', async () => {
    const grouped = testProjection({
      view: { ...testProjection().view, mode: 'aggregate' },
      detail_rows: null,
      aggregate_rows: [testAggregateRow()],
    })
    const controller = stubController(grouped)
    controller.projection!.capabilities!.push(
      ...[
        ['view.show-detail', 'd'],
        ['selection.toggle', 'space'],
      ].map(([id, key_display]) => ({
        id: id!,
        key_display: key_display!,
        description: id!,
        category: 'Views',
        available: true,
      })),
    )
    let showDetail!: () => void
    vi.mocked(controller.apply).mockImplementationOnce(
      () =>
        new Promise<boolean>((resolve) => {
          showDetail = () => {
            Object.assign(controller, {
              projection: testProjection({ canonical_query: 'v=1&mode=detail' }),
            })
            resolve(true)
          }
        }),
    )
    render(AppShell, { controller })
    await fireEvent.keyDown(window, { key: 'd' })
    await fireEvent.keyDown(window, { key: ' ' })
    await vi.waitFor(() => expect(controller.apply).toHaveBeenCalledTimes(1))

    showDetail()
    await vi.waitFor(() => expect(controller.apply).toHaveBeenCalledTimes(2))
    expect(controller.apply).toHaveBeenLastCalledWith({
      action: 'selection.toggle',
      target: { identity: 'row-0', kind: 'transaction' },
    })
  })

  it('toggles the row j moved to when Space, j, Space wait behind a slow toggle', async () => {
    const controller = heldFirstApply(lateInformation().controller)
    vi.mocked(controller.moveCursor).mockImplementation(async (delta) => {
      Object.assign(controller, { cursorIndex: controller.cursorIndex + delta })
    })
    render(AppShell, { controller })
    await fireEvent.keyDown(window, { key: ' ' })
    await fireEvent.keyDown(window, { key: 'j' })
    await fireEvent.keyDown(window, { key: ' ' })
    controller.finish()

    await vi.waitFor(() => expect(controller.apply).toHaveBeenCalledTimes(2))
    expect(vi.mocked(controller.apply).mock.calls.map(([action]) => action)).toEqual([
      { action: 'selection.toggle', target: { identity: 'row-0', kind: 'transaction' } },
      { action: 'selection.toggle', target: { identity: 'row-1', kind: 'transaction' } },
    ])
  })

  it.each([
    ['h', 'transaction.toggle-hidden'],
    ['x', 'transaction.delete'],
  ])('acts on the row j moved to when %s waits behind a slow toggle', async (key, action) => {
    const controller = heldFirstApply(lateInformation().controller)
    vi.mocked(controller.moveCursor).mockImplementation(async (delta) => {
      Object.assign(controller, { cursorIndex: controller.cursorIndex + delta })
    })
    render(AppShell, { controller })
    await fireEvent.keyDown(window, { key: ' ' })
    await fireEvent.keyDown(window, { key: 'j' })
    await fireEvent.keyDown(window, { key })
    controller.finish()

    if (key === 'x') {
      await screen.findByRole('alertdialog', { name: 'Confirm deletion' })
      await fireEvent.keyDown(document.activeElement!, { key: 'Enter' })
    }
    await vi.waitFor(() =>
      expect(controller.editing.submit).toHaveBeenCalledWith(
        expect.objectContaining({ action, target: { identity: 'row-1', kind: 'transaction' } }),
      ),
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

/** A view with a year period and a currency, so the period arrows are enabled. */
function periodProjection(): ReturnType<typeof testProjection> {
  const base = testProjection()
  return testProjection({
    currencies: [
      { currency: 'USD', scale: 2, count: 3, first: '2024-01-01', latest: '2026-10-07' },
    ],
    view: { ...base.view, currency: { currency: 'USD', scale: 2 } },
    filters: { ...base.filters, date_range: { from: '2026-01-01', to: '2026-12-31' } },
  })
}

/** Shows `range` as the view's period, as the controller does once a period change returns. */
function showPeriod(controller: ViewController, range: { from: string; to: string } | undefined) {
  const current = controller.projection!
  const filters = { ...current.filters }
  delete filters.date_range
  if (range) filters.date_range = range
  Object.assign(controller, { projection: { ...current, filters } })
}

/** Holds the first transition until `finish`, as a slow request would. */
function heldFirstApply(controller: ViewController): ViewController & { finish: () => void } {
  let release: ((applied: boolean) => void) | undefined
  vi.mocked(controller.apply).mockImplementationOnce(
    () => new Promise<boolean>((resolve) => (release = resolve)),
  )
  return Object.assign(controller, { finish: () => release?.(true) })
}

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

/** A charted merchant view whose Space toggle waits until `release`. */
function heldToggle() {
  const base = charted()
  const controller = stubController({
    ...base,
    view: { ...base.view, mode: 'aggregate' },
    detail_rows: null,
    aggregate_rows: [testAggregateRow({ identity: 'merchant:a', label: 'Merchant A' })],
  })
  controller.projection!.capabilities!.push({
    id: 'selection.toggle',
    key_display: 'space',
    description: 'Toggle selection',
    category: 'Selection',
    available: true,
  })
  let release!: () => void
  vi.mocked(controller.apply).mockReturnValue(
    new Promise<boolean>((resolve) => (release = () => resolve(true))),
  )
  return { controller, release }
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

/** Two transaction rows whose details each wait until the test releases them. */
function lateInformation(): { controller: ViewController; waiting: Map<string, () => void> } {
  const first = testProjection().detail_rows![0]!
  const rows = [0, 1].map((index) => ({
    ...first,
    index,
    identity: `row-${index}`,
    merchant: `Example Merchant ${index}`,
  }))
  const controller = stubController(
    testProjection({
      total_rows: 2,
      window: { offset: 0, limit: 200, count: 2 },
      detail_rows: rows,
    }),
  )
  controller.projection!.capabilities!.push({
    id: 'view.drill',
    key_display: 'enter',
    description: 'view.drill',
    category: 'Views',
    available: true,
  })
  const waiting = new Map<string, () => void>()
  controller.transactionInformation = vi.fn(
    (identity: string) =>
      new Promise<TransactionInformationResponse>((resolve) => {
        const row = rows.find((candidate) => candidate.identity === identity)!
        waiting.set(identity, () => resolve(transactionInformation(row)))
      }),
  )
  return { controller, waiting }
}

/**
 * Answers the second request first when both are out, then the first, and expects the drawer
 * to end on the second row.
 */
async function expectLastInformation(waiting: Map<string, () => void>): Promise<void> {
  waiting.get('row-1')?.()
  waiting.get('row-0')!()
  await vi.waitFor(() => expect(waiting.has('row-1')).toBe(true))
  waiting.get('row-1')!()
  const drawer = await screen.findByRole('dialog', { name: 'Transaction information' })
  await vi.waitFor(() => expect(drawer.textContent).toContain('Example Merchant 1'))
  await new Promise((resolve) => setTimeout(resolve))
  expect(drawer.textContent).not.toContain('Example Merchant 0')
}

function transactionInformation(
  transaction: TransactionInformationResponse['transaction'],
): TransactionInformationResponse {
  const none = { offset: 0, limit: 0, count: 0 }
  return {
    version: '1',
    revision: '1',
    canonical_query: 'v=1',
    amazon_qualified: false,
    item_window: none,
    match_window: none,
    matches: [],
    total_matches: 0,
    transaction,
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
          ['selection.toggle', 'space'],
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
    opening: false,
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
      cancel: vi.fn(),
      continueStale: vi.fn(),
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
      retry: vi.fn(async () => true),
    },
    hydrate: vi.fn(),
    recheck: vi.fn(),
    moveCursor: vi.fn(),
    moveCursorTo: vi.fn(),
    moveHome: vi.fn(),
    showIndex: vi.fn(),
    showAnchor: vi.fn(),
    recheckNewer: vi.fn(),
    reloadDay: vi.fn(),
    insights: vi.fn(async (body) => testInsights({ query: body.query, revision: '0' })),
    apply: vi.fn(),
    beginSearch: vi.fn(),
    previewSearch: vi.fn(),
    commitSearch: vi.fn(),
    restoreSearch: vi.fn(),
    hold: <T>(work: Promise<T>) => work,
    settled: async () => undefined,
    restore: vi.fn(),
    flushHistory: vi.fn(),
    retry: vi.fn(),
    reset: vi.fn(),
  }
}

/** A real provider controller whose refresh runs until it is canceled. */
function blockedRefresh(projection: ViewProjection) {
  let signal: AbortSignal | undefined
  const status = vi.fn(async () =>
    testProviderStatus({ provider_kind: 'monarch', refreshing: true }),
  )
  const request = vi.fn(async (_path: string, _body: string, sent?: AbortSignal) => {
    signal = sent
    return new Promise<Response>((_resolve, reject) =>
      sent?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))),
    )
  })
  const provider = createProviderController({
    transport: { mutations: { request }, status },
    host: {
      current: () => projection,
      accept: vi.fn(),
      reload: vi.fn(async () => undefined),
      serially: (work) => work(),
    },
  })
  provider.sync(projection)
  return { provider, status, aborted: () => signal?.aborted === true }
}
