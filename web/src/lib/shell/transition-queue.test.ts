import { describe, expect, it, vi } from 'vitest'

import type { ViewProjection } from '../api/client'
import { testProjection } from '../../test/projection'
import { TransitionQueue } from './transition-queue'

function controller() {
  return {
    projection: testProjection(),
    apply: vi.fn(async () => true),
    settled: vi.fn(async () => undefined),
  }
}

describe('TransitionQueue', () => {
  it('runs a change queued after a failed one', async () => {
    const host = controller()
    const queue = new TransitionQueue(() => host)
    const failed = queue.run(async () => {
      throw new Error('request failed')
    })
    const next = queue.apply({ action: 'time.previous-period' })

    await expect(failed).rejects.toThrow('request failed')
    await next
    expect(host.apply).toHaveBeenCalledWith({ action: 'time.previous-period' })
  })

  it('settles only after the queued changes and the controller finish', async () => {
    const host = controller()
    const queue = new TransitionQueue(() => host)
    let releaseWork!: () => void
    let releaseController!: () => void
    void queue.run(() => new Promise<void>((resolve) => (releaseWork = resolve)))
    host.settled.mockImplementation(
      () => new Promise<undefined>((resolve) => (releaseController = () => resolve(undefined))),
    )
    let settled = false
    const done = queue.settled().then(() => (settled = true))

    await Promise.resolve()
    expect(host.settled).not.toHaveBeenCalled()
    releaseWork()
    await vi.waitFor(() => expect(host.settled).toHaveBeenCalled())
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(settled).toBe(false)
    releaseController()
    await done

    expect(settled).toBe(true)
  })

  it('decides a change from the view the controller shows once it settles', async () => {
    const host = controller()
    const queue = new TransitionQueue(() => host)
    let release!: () => void
    host.settled.mockImplementation(
      () => new Promise<undefined>((resolve) => (release = () => resolve(undefined))),
    )
    const clearPeriod = (current: ViewProjection) =>
      current.filters.date_range ? { action: 'time.clear-period' as const } : undefined

    const applied = queue.apply(clearPeriod)
    await vi.waitFor(() => expect(host.settled).toHaveBeenCalled())
    const range = { from: '2025-01-01', to: '2025-12-31' }
    host.projection = testProjection({ filters: { ...host.projection.filters, date_range: range } })
    release()
    await applied

    expect(host.apply).toHaveBeenCalledWith({ action: 'time.clear-period' })
  })

  it('drops a change still being decided when it stops', async () => {
    const host = controller()
    const queue = new TransitionQueue(() => host)
    const deciding = Promise.withResolvers<void>()
    const settling = Promise.withResolvers<undefined>()
    host.settled.mockImplementation(() => {
      deciding.resolve()
      return settling.promise
    })

    const applied = queue.apply(() => ({ action: 'time.clear-period' }))
    await deciding.promise
    queue.stop()
    settling.resolve(undefined)
    await applied

    expect(host.apply).not.toHaveBeenCalled()
  })
})
