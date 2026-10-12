import { cleanup, fireEvent, render } from '@testing-library/svelte'
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

type Insights = (body: InsightsBody, signal: AbortSignal) => Promise<InsightsResponse>

function renderBody(insights: Insights) {
  const base = testProjection()
  const projection = testProjection({
    view: { ...base.view, currency: { currency: 'USD', scale: 2 } },
  })
  const controller = {
    projection,
    loading: false,
    cursorIndex: 0,
    cursorIdentity: undefined,
    neighbors: [],
    editing: testEditingController(),
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
  const charts = new ChartState({ ...host, compact: () => false })
  const rendered = render(WorkspaceBody, { projection, actions, onapply: vi.fn(), charts })
  return { ...rendered, controller }
}

describe('WorkspaceBody', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('aborts the insights request in flight when it unmounts', () => {
    const signals: AbortSignal[] = []
    const insights = vi.fn<Insights>(
      (_body, signal) =>
        new Promise<InsightsResponse>(() => {
          signals.push(signal)
        }),
    )

    const { unmount } = renderBody(insights)
    expect(insights).toHaveBeenCalledTimes(1)
    expect(signals[0]?.aborted).toBe(false)

    unmount()
    expect(signals[0]?.aborted).toBe(true)
  })

  it('checks old insights again when the window comes back to the foreground', async () => {
    const now = vi.spyOn(Date, 'now').mockReturnValue(1_000_000)
    const insights = vi.fn<Insights>(async (body) =>
      testInsights({ query: body.query, revision: '0' }),
    )
    renderBody(insights)
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
    const insights = vi.fn<Insights>(async (body) =>
      testInsights({ query: body.query, revision: '0', today }),
    )
    const { controller } = renderBody(insights)
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
