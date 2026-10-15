import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import WorkspaceBody from './WorkspaceBody.svelte'
import type { InsightsBody, InsightsResponse } from '../../lib/api/client'
import type { ViewController } from '../../lib/controller/view-controller.svelte'
import { ChartState } from '../../lib/shell/chart-state.svelte'
import type { LocalActionContext } from '../../lib/shell/local-actions'
import { OverlayState } from '../../lib/shell/overlay-state.svelte'
import { testEditingController } from '../../test/editing'
import { testInsights } from '../../test/insights'
import { testProjection } from '../../test/projection'
import { stubProvider } from '../../test/provider'

function bodyActions(
  projection: ReturnType<typeof testProjection>,
  providerPhase?: string,
  insights: ViewController['insights'] = vi.fn(
    () => new Promise<InsightsResponse>(() => undefined),
  ),
): { actions: LocalActionContext; charts: ChartState } {
  const controller = {
    projection,
    loading: false,
    cursorIndex: 0,
    cursorIdentity: undefined,
    neighbors: [],
    editing: testEditingController(),
    providerWrite: { state: { status: providerPhase ? { phase: providerPhase } : undefined } },
    provider: stubProvider(true),
    insights,
    recheckNewer: vi.fn(async () => undefined),
    reloadDay: vi.fn(async () => undefined),
  } as unknown as ViewController
  const host = { pushScope: () => undefined }
  const actions: LocalActionContext = {
    controller,
    overlays: new OverlayState({ ...host, focusGrid: () => undefined, grid: () => undefined }),
    queue: async (work) => work(),
    focusGrid: () => undefined,
    importAmazon: () => undefined,
    focusSearch: () => undefined,
    openFilters: () => undefined,
  }
  return { actions, charts: new ChartState({ ...host, compact: () => false }) }
}

function usdProjection(): ReturnType<typeof testProjection> {
  const base = testProjection()
  return testProjection({ view: { ...base.view, currency: { currency: 'USD', scale: 2 } } })
}

function stubPhoneWidth(): () => void {
  const original = window.matchMedia
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: (query: string) => ({
      matches: true,
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

describe('WorkspaceBody', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('aborts the insights request in flight when it unmounts', () => {
    const signals: AbortSignal[] = []
    const insights = vi.fn(
      (_body: InsightsBody, signal: AbortSignal) =>
        new Promise<InsightsResponse>(() => {
          signals.push(signal)
        }),
    )
    const projection = usdProjection()
    const { actions, charts } = bodyActions(projection, undefined, insights)

    const { unmount } = render(WorkspaceBody, { projection, actions, onapply: vi.fn(), charts })
    expect(insights).toHaveBeenCalledTimes(1)
    expect(signals[0]?.aborted).toBe(false)

    unmount()
    expect(signals[0]?.aborted).toBe(true)
  })

  it('shows the edit lock banner while a provider write runs and opens write status', async () => {
    const projection = testProjection({ profile_kind: 'ynab' })
    const { actions, charts } = bodyActions(projection, 'writing')

    render(WorkspaceBody, { projection, actions, onapply: vi.fn(), charts })

    expect(
      screen.getByText('Writing to YNAB · Editing is locked until the write finishes'),
    ).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: /^Status\s*w$/ }))
    expect(actions.overlays.current).toBe('write')
  })

  it('words the banner by the write phase', () => {
    const projection = testProjection({ profile_kind: 'ynab' })
    const { actions, charts } = bodyActions(projection, 'paused')

    render(WorkspaceBody, { projection, actions, onapply: vi.fn(), charts })

    expect(
      screen.getByText('YNAB write paused · Editing is locked until it resumes or is reconciled'),
    ).toBeTruthy()
  })

  it('shows no banner when no provider write is active', () => {
    const projection = testProjection()
    const { actions, charts } = bodyActions(projection)

    render(WorkspaceBody, { projection, actions, onapply: vi.fn(), charts })

    expect(screen.queryByText(/Editing is locked/)).toBeNull()
  })

  it('shows the banner at phone width, where the KPI strip lives in the chart drawer', () => {
    const restore = stubPhoneWidth()
    try {
      const projection = usdProjection()
      const { actions, charts } = bodyActions(projection, 'writing')

      render(WorkspaceBody, { projection, actions, onapply: vi.fn(), charts })

      expect(screen.getByText(/Editing is locked until the write finishes/)).toBeTruthy()
      expect(document.querySelector('.mf-workspace-body > .mf-chart-area')).toBeNull()
    } finally {
      restore()
    }
  })

  it('checks old insights again when the window comes back to the foreground', async () => {
    const now = vi.spyOn(Date, 'now').mockReturnValue(1_000_000)
    const insights = vi.fn<ViewController['insights']>(async (body) =>
      testInsights({ query: body.query, revision: '0' }),
    )
    const projection = usdProjection()
    const { actions, charts } = bodyActions(projection, undefined, insights)
    render(WorkspaceBody, { projection, actions, onapply: vi.fn(), charts })
    await vi.waitFor(() => expect(insights).toHaveBeenCalledTimes(1))
    await new Promise((resolve) => setTimeout(resolve, 0))

    now.mockReturnValue(1_000_000 + 10 * 60_000)
    await fireEvent.focus(window)
    expect(insights).toHaveBeenCalledTimes(2)
    expect(insights.mock.lastCall?.[0]).toEqual({ query: 'v=1', summary: true })
  })

  it('reloads the table when the insights come back from a new day', async () => {
    const now = vi.spyOn(Date, 'now').mockReturnValue(1_000_000)
    let today = '2026-10-07'
    const insights = vi.fn<ViewController['insights']>(async (body) =>
      testInsights({ query: body.query, revision: '0', today }),
    )
    const projection = usdProjection()
    const { actions, charts } = bodyActions(projection, undefined, insights)
    const { controller } = actions
    render(WorkspaceBody, { projection, actions, onapply: vi.fn(), charts })
    await vi.waitFor(() => expect(insights).toHaveBeenCalledTimes(1))
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(controller.reloadDay).not.toHaveBeenCalled()

    // The tab comes back after midnight; the profile revision has not changed.
    today = '2026-10-08'
    now.mockReturnValue(1_000_000 + 10 * 60_000)
    await fireEvent.focus(window)
    await vi.waitFor(() => expect(controller.reloadDay).toHaveBeenCalledTimes(1))
  })
})
