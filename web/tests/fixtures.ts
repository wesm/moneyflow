import { test as base, expect, type Page } from '@playwright/test'

import { startE2EServer, type E2EServer } from '../scripts/e2e-server'

interface MoneyflowFixtures {
  server: E2EServer
}

export const test = base.extend<MoneyflowFixtures>({
  server: [
    // Playwright requires an object pattern even though this worker fixture has no dependencies.
    // eslint-disable-next-line no-empty-pattern
    async ({}, use) => {
      const server = await startE2EServer('/')
      await use(server)
      await server.stop()
    },
    { scope: 'worker' },
  ],
})

export { expect }

/**
 * Opens Moneyflow and returns once the first load has settled. A link without a currency first
 * shows every currency, then switches to the busiest one, so the URL gains `cur=` only after the
 * grid already has focus. Every profile these tests open has transactions, so it has a currency.
 */
export async function openMoneyflow(page: Page, server: E2EServer, query = ''): Promise<void> {
  await page.goto(`${server.url}${query === '' ? '' : `?${query}`}`)
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  await expect(page).toHaveURL(/[?&]cur=/)
}

export async function activeRow(page: Page) {
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const activeID = await grid.getAttribute('aria-activedescendant')
  expect(activeID).toBeTruthy()
  return page.locator(`[id=${JSON.stringify(activeID)}]`)
}

export async function activeAggregateCount(page: Page, rowIndex: number): Promise<number> {
  const row = await activeRow(page)
  await expect(row).toHaveAttribute('aria-rowindex', String(rowIndex))
  const text = await row.getByRole('gridcell').nth(1).innerText()
  const count = Number(text.replace(/\D/g, ''))
  expect(Number.isSafeInteger(count) && count > 0, `Count cell "${text}"`).toBe(true)
  return count
}
