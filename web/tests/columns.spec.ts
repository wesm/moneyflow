import type { Page } from '@playwright/test'

import { expect, openMoneyflow, test } from './fixtures'

const year = 'cur=USD%3A2&from=2026-01-01&to=2026-12-31&v=1'

/** The labels a column cuts off in the rows on screen. */
async function truncated(page: Page, key: string): Promise<string[]> {
  return page
    .locator(`.mf-table__row:not(.mf-table__sizer) > [data-column="${key}"]`)
    .evaluateAll((cells) =>
      cells.flatMap((cell) => {
        const text = cell.querySelector('.mf-table__text') ?? cell
        return text.scrollWidth > text.clientWidth ? [text.textContent?.trim() ?? ''] : []
      }),
    )
}

/** Drills into the grouped row named `label`, as Enter on it does. */
async function drill(page: Page, groupKey: string, label: string): Promise<void> {
  await page.keyboard.press(groupKey)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const row = grid.getByRole('row').filter({ hasText: label }).first()
  await row.locator('[data-column="name"]').click()
  await expect(row).toHaveClass(/mf-table__row--active/)
  await page.keyboard.press('Enter')
  await expect(grid.getByRole('columnheader', { name: 'Date' })).toBeVisible()
}

for (const { key, dimension, label } of [
  { key: '1', dimension: 'merchant', label: 'Taco Truck' },
  { key: '2', dimension: 'category', label: 'Groceries' },
  { key: '4', dimension: 'account', label: 'Rewards Card' },
]) {
  test(`a drilled ${dimension} shows its short name in full`, async ({ page, server }) => {
    await openMoneyflow(page, server, year)
    await drill(page, key, label)
    const cells = page.locator(
      `.mf-table__row:not(.mf-table__sizer) > [data-column="${dimension}"]`,
    )
    await expect(cells.first()).toContainText(label)
    expect(await truncated(page, dimension)).toEqual([])
  })
}

const header = (page: Page, key: string) => page.locator(`.mf-table__header [data-column="${key}"]`)

/** The widths of the header cells, which are the column widths. */
async function widths(page: Page, keys: readonly string[]): Promise<number[]> {
  const result: number[] = []
  for (const key of keys) result.push((await header(page, key).boundingBox())!.width)
  return result
}

/** The widest drawn label of a column in the rows on screen, the header's included, in px. */
async function widestOnScreen(page: Page, key: string): Promise<number> {
  return page
    .locator(
      `.mf-table__header [data-column="${key}"] button, ` +
        `.mf-table__row:not(.mf-table__sizer) > [data-column="${key}"]`,
    )
    .evaluateAll((elements) => {
      const range = document.createRange()
      let widest = 0
      for (const element of elements) {
        const text = element.querySelector('.mf-table__text') ?? element
        range.selectNodeContents(text)
        widest = Math.max(widest, range.getBoundingClientRect().width)
      }
      return widest
    })
}

/**
 * Scrolls through every row of the view and back to the top, and returns the widest drawn label
 * of each column, and whether any row cut one off.
 */
async function scan(page: Page, keys: readonly string[]) {
  const widest = new Map<string, number>(keys.map((key) => [key, 0]))
  const cut = new Map<string, string[]>(keys.map((key) => [key, []]))
  const scroller = page.locator('.mf-workspace')
  const height = await scroller.evaluate((element) => element.scrollHeight)
  for (let top = 0; top < height; top += 400) {
    await scroller.evaluate((element, to) => (element.scrollTop = to), top)
    await expect(page.locator('.mf-table__row--placeholder')).toHaveCount(0)
    for (const key of keys) {
      widest.set(key, Math.max(widest.get(key)!, await widestOnScreen(page, key)))
      cut.get(key)!.push(...(await truncated(page, key)))
    }
  }
  await scroller.evaluate((element) => (element.scrollTop = 0))
  return { widest, cut }
}

const detailText = ['merchant', 'category', 'account'] as const
const september = 'cur=USD%3A2&from=2026-09-01&to=2026-09-30&v=1'

for (const width of [1440, 2000]) {
  test(`at ${width} px, transaction labels show in full and Merchant takes the room`, async ({
    page,
    server,
  }) => {
    await page.setViewportSize({ width, height: 900 })
    await openMoneyflow(page, server, september)
    await page.keyboard.press('d')
    await expect(header(page, 'merchant')).toBeVisible()
    const { widest, cut } = await scan(page, detailText)
    for (const key of detailText) expect(cut.get(key), key).toEqual([])

    const [merchant, category, account] = await widths(page, detailText)
    // Category and Account keep their widest label, the cell's padding, and the pencil's room.
    expect(category!).toBeLessThanOrEqual(widest.get('category')! + 64)
    expect(account!).toBeLessThanOrEqual(widest.get('account')! + 64)
    expect(merchant!).toBeGreaterThan(widest.get('merchant')! + 64)
  })
}

test('grouped labels show in full, and the name column takes the room', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await openMoneyflow(page, server, september)
  await page.keyboard.press('1')
  await expect(header(page, 'context')).toHaveText('Top category')
  const keys = ['name', 'context']
  const { widest, cut } = await scan(page, keys)
  for (const key of keys) expect(cut.get(key), key).toEqual([])
  const [name, context] = await widths(page, keys)
  // Top category keeps its widest label and the cell's padding.
  expect(context!).toBeLessThanOrEqual(widest.get('context')! + 32)
  expect(name!).toBeGreaterThan(widest.get('name')! + 32)
})

/**
 * Gives the transactions in `body`, at any depth, one merchant and one account, so only their
 * categories decide which rows size the table. The sixth row of every window, and every sixth
 * row after it, gets `wide`; the others get one of five longer labels of narrow letters.
 */
function recategorize(body: unknown, wide: string): void {
  if (Array.isArray(body)) {
    for (const item of body) recategorize(item, wide)
    return
  }
  if (typeof body !== 'object' || body === null) return
  for (const [key, value] of Object.entries(body)) {
    if (key !== 'detail_rows' || !Array.isArray(value)) {
      recategorize(value, wide)
      continue
    }
    value.forEach((row: { merchant: string; account: string; category: string }, index) => {
      row.merchant = 'Example Merchant'
      row.account = 'Example Account'
      row.category = index % 6 === 5 ? wide : `little lilies ${(index % 6) + 1}`
    })
  }
}

test('a short label of wide letters shows in full beside longer narrow ones', async ({
  page,
  server,
}) => {
  // Fewer characters than "little lilies 1", but drawn wider than it.
  const wide = 'WWWWWWWWWWWWWW'
  await page.route(`${server.profileAPI}view**`, async (route) => {
    const response = await route.fetch()
    const body: unknown = await response.json()
    recategorize(body, wide)
    await route.fulfill({ response, json: body })
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await openMoneyflow(page, server, september)
  await page.keyboard.press('d')
  await expect(header(page, 'merchant')).toBeVisible()
  const cells = page.locator('.mf-table__row:not(.mf-table__sizer) > [data-column="category"]')
  await expect(cells.filter({ hasText: wide }).first()).toBeVisible()
  expect((await scan(page, ['category'])).cut.get('category')).toEqual([])
})

/** The width in px that the table's columns may fill. */
async function room(page: Page): Promise<number> {
  return page.locator('.mf-workspace').evaluate((element) => {
    const style = getComputedStyle(element)
    return element.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight)
  })
}

test('in a narrow window, the widest text column narrows before a shorter one cuts', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 1024, height: 768 })
  await openMoneyflow(page, server, year)
  await page.keyboard.press('d')
  await expect(header(page, 'merchant')).toBeVisible()
  expect((await scan(page, detailText)).cut).toEqual(new Map(detailText.map((key) => [key, []])))

  // Just more room than every transaction column's minimum, 47 rem, so none drops. The sidebar
  // can change form as the window narrows, so the window steps until the room is right.
  const target = 47.5 * 16
  let width = 1024
  for (let step = 0; step < 3 && (await room(page)) !== target; step++) {
    width -= (await room(page)) - target
    await page.setViewportSize({ width, height: 768 })
    await expect(header(page, 'merchant')).toBeVisible()
  }
  expect(await room(page)).toBe(target)
  const { cut } = await scan(page, detailText)
  const [merchant, category, account] = await widths(page, detailText)
  expect(cut.get('merchant')!.length).toBeGreaterThan(0)
  expect(cut.get('category')).toEqual([])
  expect(cut.get('account')).toEqual([])
  expect(merchant!).toBeGreaterThanOrEqual(Math.max(category!, account!))
})

test('column widths hold still while the table scrolls', async ({ page, server }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await openMoneyflow(page, server, year)
  await page.keyboard.press('d')
  await expect(header(page, 'merchant')).toBeVisible()
  const keys = ['date', ...detailText, 'amount', 'flags']
  const before = await widths(page, keys)
  const scroller = page.locator('.mf-workspace')
  const height = await scroller.evaluate((element) => element.scrollHeight)
  for (let top = 0; top < height; top += 600) {
    await scroller.evaluate((element, to) => (element.scrollTop = to), top)
    await expect(page.locator('.mf-table__row--placeholder')).toHaveCount(0)
    expect(await widths(page, keys)).toEqual(before)
  }
})
