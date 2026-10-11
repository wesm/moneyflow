import { cleanup, render } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import WorkspaceBody from './WorkspaceBody.svelte'
import type { InsightsBody, InsightsResponse } from '../../lib/api/client'
import type { ViewController } from '../../lib/controller/view-controller.svelte'
import { ChartState } from '../../lib/shell/chart-state.svelte'
import type { LocalActionContext } from '../../lib/shell/local-actions'
import { OverlayState } from '../../lib/shell/overlay-state.svelte'
import { testEditingController } from '../../test/editing'
import { testProjection } from '../../test/projection'

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
    } as unknown as ViewController
    const host = { pushScope: () => undefined }
    const actions: LocalActionContext = {
      controller,
      overlays: new OverlayState({ ...host, focusGrid: () => undefined, grid: () => undefined }),
      afterTransitions: async () => undefined,
      queue: (work) => work(),
      focusGrid: () => undefined,
      importAmazon: () => undefined,
      focusSearch: () => undefined,
      openFilters: () => undefined,
    }
    const charts = new ChartState({ ...host, compact: () => false })

    const { unmount } = render(WorkspaceBody, { projection, actions, onapply: vi.fn(), charts })
    expect(insights).toHaveBeenCalledTimes(1)
    expect(signals[0]?.aborted).toBe(false)

    unmount()
    expect(signals[0]?.aborted).toBe(true)
  })
})
