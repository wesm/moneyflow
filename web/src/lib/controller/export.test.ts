import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ExportBody, ExportPreviewResponse, MoneyflowClient } from '../api/client'
import { createExportController } from './export'

describe('ExportController', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('previews once with Parquet and Full defaults and switches counts locally', async () => {
    const client = exportClient()
    const controller = createExportController({ client })

    await expect(controller.open('group=merchant&v=1')).resolves.toBe(true)
    expect(client.previewExport).toHaveBeenCalledWith(
      { version: '2', query: 'group=merchant&v=1' },
      expect.any(AbortSignal),
    )
    expect(controller.state).toMatchObject({
      phase: 'ready',
      format: 'parquet',
      scope: 'full',
      count: 12,
    })

    controller.setScope('filtered')
    expect(controller.state.count).toBe(3)
    expect(client.previewExport).toHaveBeenCalledTimes(1)
  })

  it('bypasses the chooser when committed data is empty', async () => {
    const client = exportClient({ full_count: 0, filtered_count: 0 })
    const controller = createExportController({ client })

    await expect(controller.open('v=1')).resolves.toBe(false)
    expect(controller.state.phase).toBe('idle')
    expect(controller.state.announcement).toBe('No data to export.')
    expect(client.downloadExport).not.toHaveBeenCalled()
  })

  it('buffers a protected response and completes a temporary browser download', async () => {
    const client = exportClient()
    vi.mocked(client.downloadExport).mockResolvedValue(
      new Response(new Uint8Array([80, 65, 82, 49]), {
        headers: {
          'Content-Disposition': 'attachment; filename="moneyflow-full-export.parquet"',
          'Content-Type': 'application/vnd.apache.parquet',
        },
      }),
    )
    const createObjectURL = vi.fn(() => 'blob:moneyflow-export')
    const revokeObjectURL = vi.fn()
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    const remove = vi.spyOn(HTMLAnchorElement.prototype, 'remove')
    const controller = createExportController({
      client,
      objectURLs: { createObjectURL, revokeObjectURL },
    })
    await controller.open('v=1')

    await expect(controller.export()).resolves.toBe(true)

    expect(client.downloadExport).toHaveBeenCalledWith(
      {
        version: '2',
        format: 'parquet',
        scope: 'full',
        query: 'v=1',
      } satisfies ExportBody,
      expect.any(AbortSignal),
    )
    expect(createObjectURL).toHaveBeenCalledWith(expect.any(Blob))
    expect(click).toHaveBeenCalledTimes(1)
    expect(remove).toHaveBeenCalledTimes(1)
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:moneyflow-export')
    expect(controller.state).toMatchObject({
      phase: 'complete',
      filename: 'moneyflow-full-export.parquet',
      announcement: 'Exported 12 transactions.',
    })
  })

  it('uses a fixed fallback filename and never echoes response data in errors', async () => {
    const client = exportClient()
    vi.mocked(client.downloadExport)
      .mockResolvedValueOnce(new Response('CSV', { status: 200 }))
      .mockResolvedValueOnce(
        Response.json(
          { code: 'export_failed', detail: 'Example Merchant private value' },
          { status: 500, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      )
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    const controller = createExportController({
      client,
      objectURLs: { createObjectURL: () => 'blob:fallback', revokeObjectURL: () => undefined },
    })
    await controller.open('v=1')
    controller.setFormat('csv')

    await controller.export()
    expect(controller.state.filename).toBe('moneyflow-export.csv')
    await expect(controller.export()).resolves.toBe(false)
    expect(controller.state.announcement).toBe('The export could not be completed. Try again.')
    expect(controller.state.announcement).not.toContain('Example Merchant')
  })

  it('cancels an in-flight request without reporting a completed download', async () => {
    const client = exportClient()
    vi.mocked(client.downloadExport).mockImplementation(
      async (_body, signal) =>
        await new Promise<Response>((_resolve, reject) => {
          signal?.addEventListener('abort', () =>
            reject(new DOMException('The operation was aborted.', 'AbortError')),
          )
        }),
    )
    const controller = createExportController({ client })
    await controller.open('v=1')

    const exporting = controller.export()
    controller.cancel()

    await expect(exporting).resolves.toBe(false)
    expect(controller.state.phase).toBe('ready')
    expect(controller.state.announcement).toBe('Export cancelled.')
    expect(controller.state.filename).toBeUndefined()
  })
})

function exportClient(
  overrides: Partial<ExportPreviewResponse> = {},
): Pick<MoneyflowClient, 'previewExport' | 'downloadExport'> {
  return {
    previewExport: vi.fn(async () => preview(overrides)),
    downloadExport: vi.fn(async () => new Response('export')),
  }
}

function preview(overrides: Partial<ExportPreviewResponse> = {}): ExportPreviewResponse {
  return {
    version: '2',
    revision: '4',
    full_count: 12,
    filtered_count: 3,
    active_operations: 2,
    inactive_operations: 1,
    commit_available: true,
    temporary_profile: false,
    canonical_query: 'v=1',
    ...overrides,
  }
}
