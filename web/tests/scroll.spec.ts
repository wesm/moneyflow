import type { Page } from '@playwright/test'

import { activeRow, expect, openMoneyflow, test } from './fixtures'

const grid = (page: Page) => page.getByRole('grid', { name: 'Financial results' })
const workspace = (page: Page) => page.locator('.mf-workspace')

/** The workspace's scroll position, and whether it shows the end of the result. */
async function scrollState(page: Page) {
  return await workspace(page).evaluate((scroller) => ({
    top: scroller.scrollTop,
    atEnd: scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 1,
  }))
}

/** The active row's `aria-rowindex`, or -1 while the grid does not render that row. */
async function activeRowIndex(page: Page): Promise<number> {
  return await grid(page).evaluate((element) => {
    const id = element.getAttribute('aria-activedescendant')
    const row = id ? document.getElementById(id) : null
    return row ? Number(row.getAttribute('aria-rowindex')) : -1
  })
}

/** The `aria-rowindex` of the first row wholly visible below the sticky header. */
async function firstVisibleRowIndex(page: Page): Promise<number> {
  return Math.min(...(await visibleRowIndexes(page)))
}

/** The `aria-rowindex` of every row wholly visible below the sticky header. */
async function visibleRowIndexes(page: Page): Promise<number[]> {
  return await workspace(page).evaluate((scroller) => {
    const headerBottom = scroller
      .querySelector('[role="grid"] [role="row"]:not([data-index])')!
      .getBoundingClientRect().bottom
    const bottom = scroller.getBoundingClientRect().bottom
    return [...scroller.querySelectorAll('[role="row"][data-index]')]
      .filter((row) => {
        const box = row.getBoundingClientRect()
        return box.top >= headerBottom - 0.5 && box.bottom <= bottom + 0.5
      })
      .map((row) => Number(row.getAttribute('aria-rowindex')))
  })
}

/**
 * Turns the mouse wheel over the table until the workspace has scrolled by `deltaY` pixels.
 * Firefox scrolls at most a page for one wheel event, and joins wheel events that queue up, so
 * the wheel turns again after each scroll until the distance is covered. The mouse moves there
 * directly: `hover()` first scrolls its target into view, which in Firefox centers the tall grid.
 */
async function wheel(page: Page, deltaY: number): Promise<void> {
  const box = (await workspace(page).boundingBox())!
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  let top = (await scrollState(page)).top
  const target = top + deltaY
  while (Math.abs(target - top) >= 1) {
    await page.mouse.wheel(0, target - top)
    top = await workspace(page).evaluate(scrollFrom, top)
  }
}

/** Resolves with the scroller's `scrollTop` once it leaves `from`, or fails after five seconds. */
function scrollFrom(scroller: Element, from: number): Promise<number> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      scroller.removeEventListener('scroll', settle)
      reject(new Error(`The wheel left the workspace at scrollTop ${from}`))
    }, 5000)
    function settle(): void {
      if (scroller.scrollTop === from) return
      clearTimeout(timer)
      scroller.removeEventListener('scroll', settle)
      resolve(scroller.scrollTop)
    }
    scroller.addEventListener('scroll', settle)
    settle()
  })
}

/** Turns the mouse wheel until the workspace shows the end of the result. */
async function wheelToEnd(page: Page): Promise<void> {
  const rest = await workspace(page).evaluate(
    (scroller) => scroller.scrollHeight - scroller.clientHeight - scroller.scrollTop,
  )
  await wheel(page, rest)
  expect((await scrollState(page)).atEnd).toBe(true)
}

test('the mouse wheel reaches the last row of a long result and j moves from the top row', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  await expect(grid(page).getByRole('columnheader', { name: 'Date' })).toBeVisible()
  const total = Number(await grid(page).getAttribute('aria-rowcount')) - 1
  expect(total).toBeGreaterThan(200)

  await wheelToEnd(page)

  const last = grid(page).locator(`[role="row"][aria-rowindex="${total + 1}"]`)
  await expect(last).toBeInViewport({ ratio: 1 })
  await expect(last).toHaveId(/^moneyflow-row-/)
  // The header sticks to the top of the workspace, and only the workspace scrolls.
  const header = grid(page).getByRole('row').first()
  const [headerBox, workspaceBox] = await Promise.all([
    header.boundingBox(),
    workspace(page).boundingBox(),
  ])
  expect(Math.abs(headerBox!.y - workspaceBox!.y)).toBeLessThanOrEqual(1)
  expect(await grid(page).evaluate((element) => element.scrollTop)).toBe(0)

  const first = await firstVisibleRowIndex(page)
  await expect.poll(() => activeRowIndex(page)).toBe(first)
  await expect(grid(page)).toBeFocused()
  const before = (await scrollState(page)).top
  await page.keyboard.press('j')
  await expect.poll(() => activeRowIndex(page)).toBe(first + 1)
  expect((await scrollState(page)).top).toBe(before)

  // Moving above the first visible row scrolls the workspace until the row clears the header.
  await page.keyboard.press('k')
  await page.keyboard.press('k')
  await expect.poll(() => activeRowIndex(page)).toBe(first - 1)
  await expect.poll(() => firstVisibleRowIndex(page)).toBe(first - 1)

  await page.keyboard.press('Home')
  await expect.poll(() => activeRowIndex(page)).toBe(2)
  await expect.poll(async () => (await scrollState(page)).top).toBe(0)
})

test('@smoke the cursor follows a wheel scroll inside the loaded rows, so j moves on from the screen', async ({
  page,
  server,
}) => {
  // A short window, so the merchant table scrolls by more than a screen.
  await page.setViewportSize({ width: 1440, height: 600 })
  await openMoneyflow(page, server)
  await expect(grid(page).getByRole('columnheader', { name: 'Merchant' })).toBeVisible()
  const total = Number(await grid(page).getAttribute('aria-rowcount')) - 1
  expect(total).toBeLessThanOrEqual(200)

  await wheelToEnd(page)
  const first = await firstVisibleRowIndex(page)
  expect(first).toBeGreaterThan(2)
  await expect.poll(() => activeRowIndex(page)).toBe(first)
  const before = (await scrollState(page)).top
  await page.keyboard.press('j')
  await expect.poll(() => activeRowIndex(page)).toBe(first + 1)
  expect((await scrollState(page)).top).toBe(before)

  // Scrolling up a screen leaves the cursor below it, so the cursor moves to the last row shown.
  const height = await workspace(page).evaluate((scroller) => scroller.clientHeight)
  await wheel(page, -height)
  await expect.poll(async () => (await scrollState(page)).top).toBe(before - height)
  const last = Math.max(...(await visibleRowIndexes(page)))
  expect(last).toBeGreaterThan(1)
  expect(last).toBeLessThan(first + 1)
  await expect.poll(() => activeRowIndex(page)).toBe(last)
  await expect(grid(page)).toBeFocused()
})

test('the cursor follows the scroll below a write banner that appeared later', async ({
  page,
  server,
}) => {
  // Hold the write status until the charts above the table have been measured.
  let release = () => {}
  const held = new Promise<void>((resolve) => (release = resolve))
  await page.route('**/provider/write-status', async (route) => {
    await held
    await route.fulfill({
      json: {
        version: '1',
        revision: '1',
        generation: '1',
        total: 4,
        completed: 1,
        failed: 0,
        remaining: 3,
        overrides: 0,
        actions: ['pause'],
        phase: 'writing',
      },
    })
  })
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  await expect(grid(page).getByRole('columnheader', { name: 'Date' })).toBeVisible()
  await expect(page.locator('.mf-chart-area')).toBeVisible()
  release()
  await expect(page.getByText(/Editing is locked until the write finishes/)).toBeVisible()

  await wheelToEnd(page)
  const first = await firstVisibleRowIndex(page)
  await expect.poll(() => activeRowIndex(page)).toBe(first)
  const before = (await scrollState(page)).top
  await page.keyboard.press('j')
  await expect.poll(() => activeRowIndex(page)).toBe(first + 1)
  expect((await scrollState(page)).top).toBe(before)
})

test('j moves down across the edge of the 200 loaded rows', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  await expect(grid(page).getByRole('columnheader', { name: 'Date' })).toBeVisible()
  // Show rows near the end of the first 200, then put the cursor on row 199.
  await workspace(page).evaluate((scroller) => (scroller.scrollTop = 185 * 30))
  const edge = grid(page).locator('[role="row"][aria-rowindex="201"]')
  await edge.locator('[data-column="date"]').click()
  await expect.poll(() => activeRowIndex(page)).toBe(201)

  for (let step = 1; step <= 3; step++) {
    await page.keyboard.press('j')
    await expect.poll(() => activeRowIndex(page)).toBe(201 + step)
  }
})

test('a header sort brings the cursor row it keeps into view', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  await expect(grid(page).getByRole('columnheader', { name: 'Date' })).toBeVisible()
  // Past row 100, beyond the charts above the table. The cursor follows to the first row shown.
  await workspace(page).evaluate((scroller) => (scroller.scrollTop = 120 * 30))
  await expect.poll(() => firstVisibleRowIndex(page)).toBeGreaterThan(100)
  const first = await firstVisibleRowIndex(page)
  await expect.poll(() => activeRowIndex(page)).toBe(first)

  const url = page.url()
  await grid(page).getByRole('button', { name: 'Amount' }).click()
  await expect(page).not.toHaveURL(url)
  await expect.poll(() => activeRowIndex(page)).toBeGreaterThan(0)
  await expect(await activeRow(page)).toBeInViewport({ ratio: 1 })
})

/** How far `row`'s bottom edge sits below the selection bar's top edge; at most 0 when clear. */
async function overlap(page: Page, rowIndex: number): Promise<number> {
  return await page.evaluate((index) => {
    const row = document.querySelector(`[role="row"][aria-rowindex="${index}"]`)!
    const bar = document.querySelector('[aria-label="Selection"]')!
    return row.getBoundingClientRect().bottom - bar.getBoundingClientRect().top
  }, rowIndex)
}

test('the selection bar never covers the last row or the cursor row', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  await expect(grid(page).getByRole('columnheader', { name: 'Date' })).toBeVisible()
  await page.keyboard.press('Space')
  await expect(page.getByRole('region', { name: 'Selection' })).toBeVisible()

  // j past the bottom of the screen keeps the cursor row whole above the bar.
  for (let step = 0; step < 40; step++) await page.keyboard.press('j')
  await expect.poll(() => activeRowIndex(page)).toBe(42)
  expect(await overlap(page, 42)).toBeLessThanOrEqual(0.5)
  expect(await overlap(page, 42)).toBeGreaterThan(-30)

  const total = Number(await grid(page).getAttribute('aria-rowcount')) - 1
  await wheelToEnd(page)
  await expect(grid(page).locator(`[role="row"][aria-rowindex="${total + 1}"]`)).toBeVisible()
  expect(await overlap(page, total + 1)).toBeLessThanOrEqual(0.5)
})

test('the cursor row at the bottom stays whole when the selection bar opens', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  await expect(grid(page).getByRole('columnheader', { name: 'Date' })).toBeVisible()
  for (let step = 0; step < 30; step++) await page.keyboard.press('j')
  await expect.poll(() => activeRowIndex(page)).toBe(32)
  await page.keyboard.press('Space')
  await expect(page.getByRole('region', { name: 'Selection' })).toBeVisible()
  await expect.poll(() => overlap(page, 32)).toBeLessThanOrEqual(0.5)
})
