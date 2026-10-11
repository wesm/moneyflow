import type { Page } from '@playwright/test'

import { activeRow, expect, openMoneyflow, test } from './fixtures'

const grid = (page: Page) => page.getByRole('grid', { name: 'Financial results' })
const workspace = (page: Page) => page.locator('.mf-workspace')

/** The active row's `aria-rowindex`, where row 1 of the result is 2, or -1 while it is not drawn. */
async function activeRowIndex(page: Page): Promise<number> {
  return await grid(page).evaluate((element) => {
    const id = element.getAttribute('aria-activedescendant')
    const row = id ? document.getElementById(id) : null
    return row ? Number(row.getAttribute('aria-rowindex')) : -1
  })
}

async function pressTimes(page: Page, key: string, times: number): Promise<void> {
  for (let step = 0; step < times; step++) await page.keyboard.press(key)
}

test('PgDn moves the cursor a page, End and T reach either end, and PgUp the top', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  await expect(grid(page).getByRole('columnheader', { name: 'Date' })).toBeVisible()
  const total = Number(await grid(page).getAttribute('aria-rowcount')) - 1

  await page.keyboard.press('PageDown')
  await expect.poll(() => activeRowIndex(page)).toBeGreaterThan(2)
  const paged = await activeRowIndex(page)
  await expect(await activeRow(page)).toBeInViewport({ ratio: 1 })
  await page.keyboard.press('PageDown')
  await expect.poll(() => activeRowIndex(page)).toBeGreaterThan(paged)
  await expect(await activeRow(page)).toBeInViewport({ ratio: 1 })

  await page.keyboard.press('Shift+T')
  await expect.poll(() => activeRowIndex(page)).toBe(2)
  await expect.poll(() => workspace(page).evaluate((scroller) => scroller.scrollTop)).toBe(0)

  await page.keyboard.press('End')
  await expect.poll(() => activeRowIndex(page)).toBe(total + 1)
  await page.keyboard.press('Shift+T')
  await page.keyboard.press('Shift+B')
  await expect.poll(() => activeRowIndex(page)).toBe(total + 1)

  await page.keyboard.press('Home')
  await pressTimes(page, 'PageDown', 3)
  await expect.poll(() => activeRowIndex(page)).toBeGreaterThan(paged)
  await pressTimes(page, 'PageUp', 3)
  await expect.poll(() => activeRowIndex(page)).toBe(2)
  await expect.poll(() => workspace(page).evaluate((scroller) => scroller.scrollTop)).toBe(0)
})
