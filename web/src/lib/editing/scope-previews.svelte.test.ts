import { describe, expect, it, vi } from 'vitest'

import type { MerchantPreview } from '../api/client'
import { ScopePreviews } from './scope-previews.svelte'

const preview = { affected_transactions: 3 } as MerchantPreview

describe('ScopePreviews', () => {
  it('stores each scope preview it loads', async () => {
    const previews = new ScopePreviews(async () => preview)
    previews.load('entity')
    await vi.waitFor(() => expect(previews.byScope.entity).toEqual(preview))
    expect(previews.byScope.transactions).toBeUndefined()
    expect(previews.error).toBe('')
  })

  it('reports a failed preview with how to retry', async () => {
    const previews = new ScopePreviews(async () => {
      throw new Error('network down')
    })
    previews.load('transactions')
    await vi.waitFor(() =>
      expect(previews.error).toBe(
        'Affected transactions could not be loaded. Change scope or reopen this editor to retry.',
      ),
    )
  })

  it('ignores the result or failure of an aborted request', async () => {
    let settle!: (fail: boolean) => void
    const previews = new ScopePreviews(
      () =>
        new Promise<MerchantPreview>((resolve, reject) => {
          settle = (fail) => (fail ? reject(new Error('aborted')) : resolve(preview))
        }),
    )
    previews.load('entity')()
    settle(false)
    previews.load('transactions')()
    settle(true)
    await Promise.resolve()
    await Promise.resolve()
    expect(previews.byScope).toEqual({})
    expect(previews.error).toBe('')
  })

  it('clears an earlier failure when it loads again', async () => {
    let fail = true
    const previews = new ScopePreviews(async () => {
      if (fail) throw new Error('network down')
      return preview
    })
    previews.load('transactions')
    await vi.waitFor(() => expect(previews.error).not.toBe(''))
    fail = false
    previews.load('transactions')
    await vi.waitFor(() => expect(previews.byScope.transactions).toEqual(preview))
    expect(previews.error).toBe('')
  })
})
