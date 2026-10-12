import { describe, expect, it } from 'vitest'

import { StageQueue } from './stage-queue.svelte'

const draft = { text: 'Local Merchant', scope: 'transactions' } as const

describe('StageQueue', () => {
  it('stages nothing when nothing was requested', () => {
    expect(new StageQueue().settle(draft, true, true)).toBe(false)
  })

  it('waits while loading, then stages the unchanged draft once', () => {
    const queue = new StageQueue()
    queue.request(draft)
    expect(queue.settle(draft, true, false)).toBe(false)
    expect(queue.settle(draft, true, true)).toBe(true)
    expect(queue.settle(draft, true, true)).toBe(false)
  })

  it('drops the request once the text or the scope changes', () => {
    for (const edited of [
      { ...draft, text: 'Local Merchants' },
      { ...draft, scope: 'entity' as const },
    ]) {
      const queue = new StageQueue()
      queue.request(draft)
      expect(queue.settle(edited, true, false)).toBe(false)
      expect(queue.settle(draft, true, true)).toBe(false)
    }
  })

  it('drops the request once the draft stops being stageable', () => {
    const queue = new StageQueue()
    queue.request(draft)
    expect(queue.settle(draft, false, false)).toBe(false)
    expect(queue.settle(draft, true, true)).toBe(false)
  })

  it('keeps only the latest request', () => {
    const queue = new StageQueue()
    queue.request(draft)
    queue.request({ ...draft, scope: 'entity' })
    expect(queue.settle(draft, true, true)).toBe(false)
  })
})
