import { describe, expect, it, vi } from 'vitest'

import type { AmazonImportStatus } from './amazon-import.svelte'
import { createAmazonImportController } from './amazon-import.svelte'

describe('Amazon import controller', () => {
  it('runs settings, upload, and atomic execute through versioned attempt state', async () => {
    const transport = transportStub()
    const controller = createAmazonImportController({ transport })

    expect(await controller.start('usd', 2)).toBe(true)
    expect(transport.start).toHaveBeenCalledWith({ version: '1', currency: 'USD', scale: 2 })
    expect(controller.state.phase).toBe('source')

    const file = new File(['header\nvalue'], 'Retail.OrderHistory.1.csv', { type: 'text/csv' })
    expect(await controller.upload([file])).toBe(true)
    expect(transport.upload).toHaveBeenCalledWith('attempt-example', '1', [file])

    expect(await controller.execute()).toBe(true)
    expect(transport.execute).toHaveBeenCalledWith('attempt-example', '2')
    expect(controller.state.phase).toBe('complete')
    expect(controller.state.announcement).toContain('1 Amazon transaction imported')
  })

  it('keeps actionable coordinates in the initiating session', async () => {
    const transport = transportStub({
      execute: status({
        state: 'failed',
        state_version: '3',
        failure_code: 'amazon_import_invalid',
        coordinate: {
          relative_filename: 'Retail.OrderHistory.csv',
          record: 7,
          column: 'Total Owed',
          reason: 'amount_invalid',
        },
      }),
    })
    const controller = createAmazonImportController({ transport })
    await controller.start('USD', 2)
    await controller.upload([new File(['bad'], 'Retail.OrderHistory.csv')])

    expect(await controller.execute()).toBe(false)
    expect(controller.state.problem).toContain('record 7')
    expect(controller.state.problem).toContain('Total Owed')
  })
})

function transportStub(overrides: { execute?: AmazonImportStatus } = {}) {
  return {
    start: vi.fn(async () => status()),
    upload: vi.fn(async () => status({ state_version: '2' })),
    execute: vi.fn(
      async () =>
        overrides.execute ??
        status({
          state: 'complete',
          state_version: '3',
          result: {
            revision: '1',
            inserted: 1,
            updated: 0,
            restored: 0,
            retired: 0,
            unchanged: 0,
            no_op: false,
          },
        }),
    ),
    status: vi.fn(async () => status({ state: 'parsing', state_version: '3' })),
    cancel: vi.fn(async () => status({ state: 'canceled', state_version: '3' })),
  }
}

function status(overrides: Partial<AmazonImportStatus> = {}): AmazonImportStatus {
  return {
    version: '1',
    attempt_id: 'attempt-example',
    profile_id: 'profile-example',
    state: 'source_required',
    state_version: '1',
    progress: { phase: '', completed: 0, total: 0 },
    result: {
      revision: '0',
      inserted: 0,
      updated: 0,
      restored: 0,
      retired: 0,
      unchanged: 0,
      no_op: false,
    },
    ...overrides,
  }
}
