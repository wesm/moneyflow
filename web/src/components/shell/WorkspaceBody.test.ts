import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import WorkspaceBody from './WorkspaceBody.svelte'
import type { InsightsBody, InsightsResponse } from '../../lib/api/client'
import type { ViewController } from '../../lib/controller/view-controller.svelte'
import { ChartState } from '../../lib/shell/chart-state.svelte'
import type { LocalActionContext } from '../../lib/shell/local-actions'
import { OverlayState } from '../../lib/shell/overlay-state.svelte'
import { testEditingController } from '../../test/editing'
import { testProjection } from '../../test/projection'

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
    insights,
    recheckNewer: vi.fn(async () => undefined),
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
      expect(document.querySelector('.mf-workspace > .mf-chart-area')).toBeNull()
    } finally {
      restore()
    }
  })
})
