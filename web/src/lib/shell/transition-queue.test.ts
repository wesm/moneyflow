import { describe, expect, it, vi } from 'vitest'

import { TransitionQueue } from './transition-queue'

function controller() {
  return { apply: vi.fn(async () => true), settled: vi.fn(async () => undefined) }
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
})
