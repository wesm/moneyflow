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

export async function openMoneyflow(page: Page, server: E2EServer, query = ''): Promise<void> {
  await page.goto(`${server.url}${query === '' ? '' : `?${query}`}`)
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
}

export async function activeRow(page: Page) {
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const activeID = await grid.getAttribute('aria-activedescendant')
  expect(activeID).toBeTruthy()
  return page.locator(`[id=${JSON.stringify(activeID)}]`)
}
