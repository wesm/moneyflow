import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ExportPreviewResponse, MoneyflowClient } from '../../lib/api/client'
import { createExportController } from '../../lib/controller/export'
import ExportDialog from './ExportDialog.svelte'

describe('ExportDialog', () => {
  afterEach(cleanup)

  it('shows keyboard-navigable defaults, counts, and state-aware warnings', async () => {
    const controller = await readyController({
      active_operations: 2,
      inactive_operations: 1,
      commit_available: true,
      temporary_profile: true,
    })
    render(ExportDialog, { controller, onclose: vi.fn() })

    expect(screen.getByRole('dialog', { name: 'Export transactions' })).not.toBeNull()
    expect(screen.getByRole('radio', { name: 'Parquet' }).getAttribute('aria-checked')).toBe('true')
    expect(screen.getByRole('radio', { name: 'Full' }).getAttribute('aria-checked')).toBe('true')
    expect(screen.getByText('12 committed transactions')).not.toBeNull()
    expect(screen.getByText(/2 pending operations are excluded/)).not.toBeNull()
    expect(screen.getByText(/Commit them first/)).not.toBeNull()
    expect(screen.getByText(/temporary profile/)).not.toBeNull()

    const full = screen.getByRole('radio', { name: 'Full' })
    full.focus()
    await fireEvent.keyDown(full, { key: 'ArrowRight' })
    expect(screen.getByRole('radio', { name: 'Filtered' }).getAttribute('aria-checked')).toBe(
      'true',
    )
    expect(screen.getByText('3 committed transactions')).not.toBeNull()
  })

  it('keeps the dialog open after an export error and cancels with Escape', async () => {
    const client = exportClient()
    vi.mocked(client.downloadExport).mockResolvedValue(new Response('failure', { status: 500 }))
    const controller = createExportController({ client })
    await controller.open('v=1')
    const onclose = vi.fn()
    render(ExportDialog, { controller, onclose })

    await fireEvent.click(screen.getByRole('button', { name: 'Export' }))
    await vi.waitFor(() =>
      expect(screen.getByText('The export could not be completed. Try again.')).not.toBeNull(),
    )
    expect(screen.getByRole('dialog', { name: 'Export transactions' })).not.toBeNull()
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onclose).toHaveBeenCalledTimes(1)
  })

  it('explains exclusions without impossible commit guidance during a write batch', async () => {
    const controller = await readyController({ active_operations: 4, commit_available: false })
    render(ExportDialog, { controller, onclose: vi.fn() })

    expect(screen.getByText(/4 pending operations are excluded/)).not.toBeNull()
    expect(screen.queryByText(/Commit them first/)).toBeNull()
  })

  it('cancels an in-flight transfer back to the chooser without closing it', async () => {
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
    const onclose = vi.fn()
    render(ExportDialog, { controller, onclose })

    await fireEvent.click(screen.getByRole('button', { name: /^Export$/ }))
    expect(screen.getAllByRole('button', { name: /^Cancel export$/ })).toHaveLength(2)
    await fireEvent.keyDown(window, { key: 'Escape' })

    expect(onclose).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog', { name: 'Export transactions' })).not.toBeNull()
    expect(screen.getByText('Export cancelled.')).not.toBeNull()
  })
})

async function readyController(overrides: Partial<ExportPreviewResponse> = {}) {
  const controller = createExportController({ client: exportClient(overrides) })
  await controller.open('v=1')
  return controller
}

function exportClient(
  overrides: Partial<ExportPreviewResponse> = {},
): Pick<MoneyflowClient, 'previewExport' | 'downloadExport'> {
  return {
    previewExport: vi.fn(async () => ({
      version: '2',
      revision: '4',
      full_count: 12,
      filtered_count: 3,
      active_operations: 0,
      inactive_operations: 0,
      commit_available: true,
      temporary_profile: false,
      canonical_query: 'v=1',
      ...overrides,
    })),
    downloadExport: vi.fn(async () => new Response('export')),
  }
}
