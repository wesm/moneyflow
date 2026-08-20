import { readFile } from 'node:fs/promises'

import type { Download, Page, Response } from '@playwright/test'

import { expect, openMoneyflow, test } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

test('@smoke E downloads a protected Parquet blob with complete headers', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const exportRequestHeaders: Array<Record<string, string>> = []
  page.on('request', (request) => {
    if (request.method() === 'POST' && request.url() === `${server.profileAPI}export`) {
      exportRequestHeaders.push(request.headers())
    }
  })

  await page.keyboard.press('Shift+E')
  const dialog = page.getByRole('dialog', { name: 'Export transactions' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('radio', { name: 'Parquet' })).toHaveAttribute(
    'aria-checked',
    'true',
  )
  await expect(dialog.getByRole('radio', { name: 'Full' })).toHaveAttribute('aria-checked', 'true')

  const result = await downloadFrom(
    dialog.getByRole('button', { name: 'Export', exact: true }),
    page,
    server,
  )
  expect(result.response.headers()['content-disposition']).toContain('attachment; filename=')
  expect(result.response.headers()['content-length']).toBe(String(result.bytes.length))
  expect(result.response.headers()['cache-control']).toContain('no-store')
  expect(result.response.headers()['x-content-type-options']).toBe('nosniff')
  expect(result.download.suggestedFilename()).toMatch(
    /^\d{4}-\d{2}-\d{2}_\d{6}_\d{6}-full-export\.parquet$/,
  )
  expect(result.bytes.subarray(0, 4).toString('ascii')).toBe('PAR1')
  expect(result.bytes.subarray(-4).toString('ascii')).toBe('PAR1')
  expect(exportRequestHeaders).toHaveLength(1)
  expect(exportRequestHeaders[0]?.['x-moneyflow-mutation-token']).toBeTruthy()
})

test('Chromium exports all formats and applies the canonical filtered query', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  for (const format of ['CSV', 'SQLite', 'Parquet'] as const) {
    await page.keyboard.press('Shift+E')
    const dialog = page.getByRole('dialog', { name: 'Export transactions' })
    await dialog.getByRole('radio', { name: format }).click()
    const result = await downloadFrom(
      dialog.getByRole('button', { name: 'Export', exact: true }),
      page,
      server,
    )
    if (format === 'CSV') {
      const text = result.bytes.toString('utf8')
      expect(text).toContain('# scope: full')
      expect(text).toContain('amount,amount_minor,currency,scale')
      expect(text).toContain(',-1200.00,-120000,USD,2,')
    } else if (format === 'SQLite') {
      expect(result.bytes.subarray(0, 16).toString('utf8')).toBe('SQLite format 3\u0000')
    } else {
      expect(result.bytes.subarray(0, 4).toString('ascii')).toBe('PAR1')
    }
    await dialog.getByRole('button', { name: 'Close' }).click()
    await expect(dialog).toBeHidden()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  }

  await page.keyboard.press('/')
  const search = page.getByRole('searchbox', { name: 'Search transactions' })
  await search.fill('grocer')
  await search.press('Enter')
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  await page.keyboard.press('Shift+E')
  const filtered = page.getByRole('dialog', { name: 'Export transactions' })
  await filtered.getByRole('radio', { name: 'CSV' }).click()
  await filtered.getByRole('radio', { name: 'Filtered' }).click()
  const filteredResult = await downloadFrom(
    filtered.getByRole('button', { name: 'Export', exact: true }),
    page,
    server,
  )
  const filteredText = filteredResult.bytes.toString('utf8')
  expect(filteredText).toContain('# scope: filtered')
  expect(filteredText).toContain('# canonical_query:')
  expect(filteredText).toContain('q=grocer')
  expect(filteredText).toContain('Example Grocer')
  expect(filteredText).not.toContain('Example Housing')
})

test('pending warnings, empty bypass, and failures remain keyboard safe', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    await page.keyboard.press('h')
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await page.keyboard.press('Shift+E')
    const dialog = page.getByRole('dialog', { name: 'Export transactions' })
    await expect(dialog).toContainText('1 pending operation is excluded')
    await dialog.getByRole('button', { name: 'Close' }).click()

    await page.route(`${server.profileAPI}export/preview`, async (route) => {
      await route.fulfill({
        json: {
          version: '2',
          revision: '1',
          full_count: 0,
          filtered_count: 0,
          active_operations: 0,
          inactive_operations: 0,
          commit_available: true,
          temporary_profile: true,
          canonical_query: 'v=1',
        },
      })
    })
    await page.keyboard.press('Shift+E')
    await expect(page.getByRole('dialog', { name: 'Export transactions' })).toBeHidden()
    await expect(page.getByText('No data to export.')).toBeAttached()
    await page.unroute(`${server.profileAPI}export/preview`)

    await page.keyboard.press('Shift+E')
    let releaseCancelledRequest = (): void => undefined
    await page.route(`${server.profileAPI}export`, async (route) => {
      await new Promise<void>((resolve) => {
        releaseCancelledRequest = resolve
      })
      await route.abort().catch(() => undefined)
    })
    const cancelledDialog = page.getByRole('dialog', { name: 'Export transactions' })
    await cancelledDialog.getByRole('button', { name: 'Export', exact: true }).click()
    await expect(cancelledDialog.getByRole('button', { name: 'Exporting…' })).toBeDisabled()
    await page.keyboard.press('Escape')
    await expect(cancelledDialog).toContainText('Export cancelled.')
    await expect(cancelledDialog).toBeVisible()
    releaseCancelledRequest()
    await page.unroute(`${server.profileAPI}export`)

    await page.route(`${server.profileAPI}export`, async (route) => {
      await route.fulfill({
        status: 500,
        contentType: 'application/problem+json',
        body: JSON.stringify({
          type: 'about:blank',
          title: 'Export failed',
          status: 500,
          code: 'export_failed',
          detail: 'private provider data must not be echoed',
        }),
      })
    })
    const retryDialog = page.getByRole('dialog', { name: 'Export transactions' })
    await retryDialog.getByRole('button', { name: 'Export', exact: true }).click()
    await expect(retryDialog).toContainText('The export could not be completed. Try again.')
    await expect(retryDialog).not.toContainText('private provider data')
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  } finally {
    await server.stop()
  }
})

async function downloadFrom(
  button: ReturnType<Page['getByRole']>,
  page: Page,
  server: { profileAPI: string },
): Promise<{ bytes: Buffer; download: Download; response: Response }> {
  const responsePromise = page.waitForResponse(
    (response) =>
      response.request().method() === 'POST' && response.url() === `${server.profileAPI}export`,
  )
  const downloadPromise = page.waitForEvent('download')
  await button.click()
  const [response, download] = await Promise.all([responsePromise, downloadPromise])
  const path = await download.path()
  expect(path).toBeTruthy()
  return { bytes: await readFile(path!), download, response }
}
