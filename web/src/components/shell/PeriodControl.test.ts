import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import PeriodControl from './PeriodControl.svelte'
import type { ViewProjection } from '../../lib/api/client'
import { TransitionQueue } from '../../lib/shell/transition-queue'
import { testProjection } from '../../test/projection'

const usd = { currency: 'USD', scale: 2, count: 3, first: '2024-01-01', latest: '2026-10-07' }

function allTime(overrides: Partial<ViewProjection> = {}): ViewProjection {
  const base = testProjection()
  return testProjection({
    currencies: [usd],
    view: { ...base.view, currency: { currency: 'USD', scale: 2 } },
    ...overrides,
  })
}

function control(projection: ViewProjection) {
  const host = { projection, apply: vi.fn(async () => true), settled: async () => undefined }
  const queue = new TransitionQueue(() => host)
  const onclose = vi.fn()
  render(PeriodControl, {
    projection,
    chooserOpen: true,
    onchoose: vi.fn(),
    onclose,
    onapply: queue.apply,
  })
  const chooser = within(screen.getByRole('dialog', { name: 'Choose period' }))
  return {
    sent: async () => {
      await queue.settled()
      return host.apply.mock.calls
    },
    onclose,
    allTimeButton: chooser.getByRole('button', { name: /All time/ }),
  }
}

describe('PeriodControl', () => {
  afterEach(cleanup)

  it('clears a time drill opened from all time when All time is chosen', async () => {
    const { sent, onclose, allTimeButton } = control(
      allTime({
        canonical_query: 'v=1&g=time&drill=time%3AUSD%3A2%3Amonth%3A2026-09',
        view: {
          ...testProjection().view,
          currency: { currency: 'USD', scale: 2 },
          drill_depth: 1,
        },
        breadcrumbs: [{ dimension: 'time', label: 'Sep 2026' }],
      }),
    )
    await fireEvent.click(allTimeButton)
    expect(onclose).toHaveBeenCalled()
    expect(await sent()).toEqual([[{ action: 'time.clear-period' }]])
  })

  it('sends nothing when All time is chosen in a view that already covers all time', async () => {
    const { sent, onclose, allTimeButton } = control(allTime())
    await fireEvent.click(allTimeButton)
    expect(onclose).toHaveBeenCalled()
    expect(await sent()).toEqual([])
  })
})
