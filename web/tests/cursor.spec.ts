import type { Page } from '@playwright/test'

import { activeRow, expect, openMoneyflow, test } from './fixtures'

const grid = (page: Page) => page.getByRole('grid', { name: 'Financial results' })
const workspace = (page: Page) => page.locator('.mf-workspace')
const cashflowColumn = (page: Page, label: string) =>
  page
    .getByRole('group', { name: 'Cash flow' })
    .getByRole('button', { name: new RegExp(`^${label}, in [+-]`) })
const periodControl = (page: Page) =>
  page.getByRole('banner').locator('button[aria-haspopup="dialog"][aria-keyshortcuts="t"]')

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

test('a period change after moving down puts the cursor on row 1', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  await expect(grid(page).getByRole('columnheader', { name: 'Date' })).toBeVisible()

  await pressTimes(page, 'j', 30)
  await expect.poll(() => activeRowIndex(page)).toBe(32)
  await cashflowColumn(page, 'Sep 2026').click()
  await expect(periodControl(page)).toHaveText('Sep 2026')
  await expect.poll(() => activeRowIndex(page)).toBe(2)
  await expect(grid(page)).toBeFocused()

  await pressTimes(page, 'j', 10)
  await expect.poll(() => activeRowIndex(page)).toBe(12)
  await page.keyboard.press('ArrowLeft')
  await expect(periodControl(page)).toHaveText('Aug 2026')
  await expect.poll(() => activeRowIndex(page)).toBe(2)
  await expect(await activeRow(page)).toBeInViewport({ ratio: 1 })
})

test('a sort change keeps the cursor on its row', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await pressTimes(page, 'j', 10)
  await expect.poll(() => activeRowIndex(page)).toBe(12)
  const before = await grid(page).getAttribute('aria-activedescendant')

  for (const key of ['s', 'v']) {
    const url = page.url()
    await page.keyboard.press(key)
    await expect(page).not.toHaveURL(url)
    await expect(grid(page)).toHaveAttribute('aria-activedescendant', before!)
    await expect(await activeRow(page)).toBeInViewport({ ratio: 1 })
  }
})

test('drilling in and searching start on row 1, and Back returns to the row', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await pressTimes(page, 'j', 5)
  await expect.poll(() => activeRowIndex(page)).toBe(7)
  const before = await grid(page).getAttribute('aria-activedescendant')

  await page.keyboard.press('Enter')
  await expect(grid(page).getByRole('columnheader', { name: 'Date' })).toBeVisible()
  await expect.poll(() => activeRowIndex(page)).toBe(2)
  await page.keyboard.press('Escape')
  await expect(grid(page)).toHaveAttribute('aria-activedescendant', before!)

  await page.keyboard.press('/')
  await page.keyboard.type('a')
  await expect(page).toHaveURL(/[?&]q=a/)
  await expect.poll(() => activeRowIndex(page)).toBe(2)
})
