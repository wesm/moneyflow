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
    let release!: () => void
    void queue.run(() => new Promise<void>((resolve) => (release = resolve)))
    const order: string[] = []
    host.settled.mockImplementation(async () => {
      order.push('controller settled')
    })

    const settled = queue.settled().then(() => order.push('queue settled'))
    await Promise.resolve()
    expect(order).toEqual([])
    release()
    await settled

    expect(order).toEqual(['controller settled', 'queue settled'])
  })
})
