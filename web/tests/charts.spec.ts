import AxeBuilder from '@axe-core/playwright'
import type { Locator, Page } from '@playwright/test'

import { activeRow, expect, openMoneyflow, test } from './fixtures'

const september = 'cur=USD%3A2&from=2026-09-01&to=2026-09-30&v=1'

function workspace(page: Page): Locator {
  return page.locator('.mf-workspace')
}

/** Focuses the grid as the app does, without the browser scrolling it into view. */
async function focusGrid(page: Page): Promise<void> {
  await page
    .getByRole('grid', { name: 'Financial results' })
    .evaluate((element: HTMLElement) => element.focus({ preventScroll: true }))
}

/** Whether the cursor row is drawn wholly inside the workspace, below the sticky header. */
async function expectCursorRowShown(page: Page): Promise<void> {
  const row = await activeRow(page)
  await expect(row).toHaveCount(1)
  const rowBox = (await row.boundingBox())!
  const header = (await page.getByRole('columnheader').first().boundingBox())!
  const port = (await workspace(page).boundingBox())!
  expect(rowBox.y).toBeGreaterThanOrEqual(header.y + header.height - 1)
  expect(rowBox.y + rowBox.height).toBeLessThanOrEqual(port.y + port.height + 1)
}

test('the KPI strip stays while the switch hides the chart row, and the choice persists', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const charts = page.getByRole('region', { name: 'Charts' })
  const income = page.locator('.mf-kpis').getByText('Income', { exact: true })
  await expect(income).toBeVisible()
  await expect(charts).toBeVisible()
  const grid = page.getByRole('grid', { name: 'Financial results' })
  expect((await income.boundingBox())!.y).toBeLessThan((await charts.boundingBox())!.y)
  expect((await charts.boundingBox())!.y).toBeLessThan((await grid.boundingBox())!.y)

  const toggle = page.getByRole('switch', { name: 'Charts' })
  await toggle.uncheck()
  await expect(charts).toHaveCount(0)
  await expect(income).toBeVisible()

  await page.reload()
  await expect(grid).toBeFocused()
  await expect(income).toBeVisible()
  await expect(toggle).not.toBeChecked()
  await expect(charts).toHaveCount(0)
})

test('toggling Charts keeps the table rows aligned with the cursor', async ({ page, server }) => {
  await openMoneyflow(page, server)
  const toggle = page.getByRole('switch', { name: 'Charts' })
  await expect(page.getByRole('region', { name: 'Charts' })).toBeVisible()
  await expect(await activeRow(page)).toHaveAttribute('aria-rowindex', '2')
  expect(await workspace(page).evaluate((element) => element.scrollTop)).toBe(0)

  // The table moves up into the chart row's room; the rows now on screen must be drawn.
  await toggle.uncheck()
  await focusGrid(page)
  const header = (await page.getByRole('columnheader').first().boundingBox())!
  const port = (await workspace(page).boundingBox())!
  // Rows are a pixel taller than the header.
  const rowHeight = (await (await activeRow(page)).boundingBox())!.height
  const shown = Math.floor((port.y + port.height - header.y - header.height) / rowHeight)
  for (let index = 1; index < shown; index += 1) await page.keyboard.press('j')
  await expect(await activeRow(page)).toHaveAttribute('aria-rowindex', String(shown + 1))
  await expectCursorRowShown(page)
  expect(await workspace(page).evaluate((element) => element.scrollTop)).toBe(0)

  // The table moves back down, below the cursor row; the next key reveals its row.
  await toggle.check()
  await focusGrid(page)
  await page.keyboard.press('j')
  await expect(await activeRow(page)).toHaveAttribute('aria-rowindex', String(shown + 2))
  await expectCursorRowShown(page)
})

test('toggling Charts while scrolled keeps the rows on screen drawn', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Charts' })).toBeVisible()
  await workspace(page).evaluate((element) => (element.scrollTop = 2000))
  await expect.poll(() => drawnAt(page, 'bottom')).toBe(true)

  const toggle = page.getByRole('switch', { name: 'Charts' })
  for (const shown of [false, true]) {
    await toggle.setChecked(shown)
    await expect(page.getByRole('region', { name: 'Charts' })).toHaveCount(shown ? 1 : 0)
    await expect.poll(() => drawnAt(page, 'top')).toBe(true)
    await expect.poll(() => drawnAt(page, 'bottom')).toBe(true)
  }
})

/** Whether a loaded row is drawn just inside the top or bottom of the workspace's rows. */
async function drawnAt(page: Page, edge: 'top' | 'bottom'): Promise<boolean> {
  const header = (await page.getByRole('columnheader').first().boundingBox())!
  const port = (await workspace(page).boundingBox())!
  const y = edge === 'top' ? header.y + header.height + 10 : port.y + port.height - 10
  return await page.evaluate(
    ({ x, y }) => {
      // Placeholders and spacers carry no row role.
      return Boolean(document.elementFromPoint(x, y)?.closest('[role="row"][aria-rowindex]'))
    },
    { x: port.x + port.width / 2, y },
  )
}

test('a failed insights request offers Retry and leaves the table working', async ({
  page,
  server,
}) => {
  await page.route('**/view/insights', (route) =>
    route.fulfill({
      status: 500,
      contentType: 'application/problem+json',
      body: JSON.stringify({ title: 'Internal Server Error', status: 500, code: 'internal' }),
    }),
  )
  await openMoneyflow(page, server)
  const failure = page.getByText(/Couldn't load charts/)
  await expect(failure).toBeVisible()
  await expect(page.getByRole('region', { name: 'Charts' })).toHaveCount(0)

  const grid = page.getByRole('grid', { name: 'Financial results' })
  await page.keyboard.press('j')
  await expect(await activeRow(page)).toHaveAttribute('aria-rowindex', '3')
  await expect(grid).toBeFocused()

  await page.unroute('**/view/insights')
  await failure.getByRole('button', { name: 'Retry' }).click()
  await expect(page.locator('.mf-kpis').getByText('Income', { exact: true })).toBeVisible()
  await expect(failure).toHaveCount(0)
})

test('the charts stay put while the table scrolls sideways, with nothing showing left of ☐', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 700, height: 844 })
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
  const income = page.locator('.mf-kpis').getByText('Income', { exact: true })
  await expect(income).toBeVisible()
  const before = (await income.boundingBox())!
  const area = page.locator('.mf-chart-area')
  const port = (await workspace(page).boundingBox())!

  for (const scrollLeft of [60, 10_000]) {
    await workspace(page).evaluate((element, left) => (element.scrollLeft = left), scrollLeft)
    await expect
      .poll(() => workspace(page).evaluate((element) => element.scrollLeft))
      .toBeGreaterThan(0)
    expect((await income.boundingBox())!.x).toBe(before.x)
    const areaBox = (await area.boundingBox())!
    expect(areaBox.x).toBeGreaterThanOrEqual(port.x)
    expect(areaBox.x + areaBox.width).toBeLessThanOrEqual(port.x + port.width)
    // The sticky ☐ column reaches the scroller's edge, so scrolled cells cannot show beside it.
    const select = page.locator('[role="columnheader"][data-column="select"]')
    expect((await select.boundingBox())!.x).toBeLessThanOrEqual(port.x)
  }
})

test('an empty month inside a merchant keeps the strip and chart row above the empty table', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page
    .getByRole('radiogroup', { name: 'Period', exact: true })
    .getByRole('radio', { name: 'Month' })
    .click()
  const period = page.getByRole('banner').locator('button[aria-keyshortcuts="t"]')
  await expect(period).toHaveText('Oct 2026')
  await page.getByRole('grid', { name: 'Financial results' }).focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/[?&]drill=merchant/)
  await page.getByRole('button', { name: 'Next period' }).click()
  await expect(period).toHaveText('Nov 2026')

  const area = page.locator('.mf-chart-area')
  await expect(workspace(page).getByText('No transactions', { exact: true })).toBeVisible()
  await expect(area.getByText('Avg per transaction', { exact: true })).toBeVisible()
  await expect(area.locator('.mf-kpi').filter({ hasText: 'Avg per transaction' })).toContainText(
    '—',
  )
  await expect(area.getByRole('region', { name: 'Charts' })).toBeVisible()
})

test('a comparison arriving where the strip wraps shifts neither the chart row nor the table', async ({
  page,
  server,
}) => {
  let release = () => {}
  const held = new Promise<void>((resolve) => (release = resolve))
  await page.route('**/view/insights', async (route) => {
    await held
    await route.continue()
  })
  await page.setViewportSize({ width: 700, height: 844 })
  await openMoneyflow(page, server, september)
  const row = page.getByRole('region', { name: 'Charts' })
  const grid = page.getByRole('grid', { name: 'Financial results' })
  await expect(row).toHaveAttribute('aria-busy', 'true')
  const before = [(await row.boundingBox())!.y, (await grid.boundingBox())!.y]

  release()
  const caption = page.getByText('vs Aug', { exact: true })
  await expect(caption).toBeVisible()
  await expect(row).toHaveAttribute('aria-busy', 'false')
  expect([(await row.boundingBox())!.y, (await grid.boundingBox())!.y]).toEqual(before)
  // The caption sits right under the last row of tiles, not on a line of its own further down.
  const tiles = (await page.locator('.mf-kpis__tiles').boundingBox())!
  const captionBox = (await caption.boundingBox())!
  expect(captionBox.y - (tiles.y + tiles.height)).toBeLessThanOrEqual(8)
  expect(captionBox.x).toBe(tiles.x)
})

for (const theme of ['light', 'dark'] as const) {
  test(`a month scope shows ${theme} KPI changes against the prior month and passes axe`, async ({
    page,
    server,
  }) => {
    await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), theme)
    await openMoneyflow(page, server, september)
    await expect(page.locator('.mf-kpis__caption')).toHaveText('vs Aug')
    await expect(page.locator('.mf-kpi__note').filter({ hasText: /^[▲▼] \d/ })).not.toHaveCount(0)
    await expect(page.getByRole('region', { name: 'Charts' })).toBeVisible()
    const result = await new AxeBuilder({ page }).include('.mf-chart-area').analyze()
    expect(result.violations).toEqual([])
  })
}

function cashflow(page: Page): Locator {
  return page.getByRole('group', { name: 'Cash flow' })
}

function column(page: Page, label: string): Locator {
  return cashflow(page).getByRole('button', { name: new RegExp(`^${label}, in [+-]`) })
}

/**
 * Shift+Tab from the table through a grouped view's right panel: its bar stop, then its
 * Spending/Income switch. The next Shift+Tab reaches the cash-flow chart.
 */
async function backThroughComposition(page: Page): Promise<void> {
  await page.keyboard.press('Shift+Tab')
  await expect(whereItWent(page).locator('button[tabindex="0"]')).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(page.getByRole('radio', { name: 'Spending' })).toBeFocused()
}

function periodControl(page: Page): Locator {
  return page.getByRole('banner').locator('button[aria-keyshortcuts="t"]')
}

test('a sort change keeps Trend, vs prior, and the charts without asking again', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server, september)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const megastore = grid.getByRole('row').filter({ hasText: 'Online Megastore' })
  const vsPrior = megastore.locator('[data-column="vs_prior"]')
  await expect(vsPrior).toHaveText(/^▲ 15\.1%/)
  await expect(page.getByRole('region', { name: 'Charts' })).toHaveAttribute('aria-busy', 'false')
  const insights: string[] = []
  page.on('request', (request) => {
    if (request.url().endsWith('/view/insights')) insights.push(request.url())
  })
  // Records any frame-to-be where the charts go busy or a Trend or vs prior cell loads again.
  await page.evaluate(() => {
    const seen: string[] = []
    Object.assign(window, { sortChurn: seen })
    new MutationObserver((records) => {
      for (const record of records) {
        const target = record.target as Element
        if (record.attributeName === 'aria-busy' && target.getAttribute('aria-busy') === 'true') {
          seen.push('busy')
        }
        for (const node of record.addedNodes) {
          if (
            node instanceof Element &&
            node
              .closest('[data-column="trend"], [data-column="vs_prior"]')
              ?.querySelector('.mf-skeleton')
          ) {
            seen.push('skeleton')
          }
        }
      }
    }).observe(document.body, { attributes: true, childList: true, subtree: true })
  })

  await focusGrid(page)
  await page.keyboard.press('s')
  await expect(page.getByRole('columnheader', { name: 'Merchant' })).toHaveAttribute(
    'aria-sort',
    'descending',
  )
  await expect(vsPrior).toHaveText(/^▲ 15\.1%/)
  await expect(page.getByRole('columnheader', { name: 'vs Aug' })).toBeVisible()
  expect(
    await page.evaluate(() => (window as unknown as { sortChurn: string[] }).sortChurn),
  ).toEqual([])
  expect(insights).toEqual([])
})

test('a cash-flow column click scopes the view to its month', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await expect(periodControl(page)).toHaveText('All time')
  await expect(column(page, 'Sep 2026')).toHaveAccessibleName(
    /^Sep 2026, in \+[\d,]+\.\d\d, out -[\d,]+\.\d\d, net [+-][\d,]+\.\d\d$/,
  )
  await column(page, 'Sep 2026').click()
  await expect(periodControl(page)).toHaveText('Sep 2026')
  await expect(page).toHaveURL(/[?&]from=2026-09-01&to=2026-09-30/)
  // The scope band now covers September, and the table keeps the focus.
  await expect(column(page, 'Sep 2026')).toHaveClass(/mf-cashflow__column--scope/)
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
})

test('a drag across cash-flow columns sets a custom range', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await column(page, 'Jul 2026').hover()
  await page.mouse.down()
  await column(page, 'Aug 2026').hover()
  await column(page, 'Sep 2026').hover()
  await page.mouse.up()
  await expect(periodControl(page)).toHaveText('Jul 1 – Sep 30, 2026')
  await expect(page).toHaveURL(/[?&]from=2026-07-01&to=2026-09-30/)
})

test('the cash-flow chart is one Tab stop that arrows, Enter, and Esc drive', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  await expect(cashflow(page).locator('button[tabindex="0"]')).toHaveCount(1)
  // The chart comes before the right panel and the table; its stop starts on the last month.
  await backThroughComposition(page)
  await page.keyboard.press('Shift+Tab')
  await expect(column(page, 'Oct 2026')).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await expect(column(page, 'Sep 2026')).toBeFocused()
  await expect(page.getByRole('tooltip')).toContainText('Sep 2026')
  await page.keyboard.press('Enter')
  await expect(periodControl(page)).toHaveText('Sep 2026')
  await expect(grid).toBeFocused()

  // Shift+Enter extends the scope to the focused column, as a shift-click does.
  await backThroughComposition(page)
  await page.keyboard.press('Shift+Tab')
  await expect(column(page, 'Sep 2026')).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await page.keyboard.press('ArrowLeft')
  await expect(column(page, 'Jul 2026')).toBeFocused()
  await page.keyboard.press('Shift+Enter')
  await expect(periodControl(page)).toHaveText('Jul 1 – Sep 30, 2026')
  await expect(grid).toBeFocused()

  // Inside a drill, Esc leaves the chart for the table without also going back a level.
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/[?&]drill=merchant/)
  // The drill lists transactions, so the panel is the heatmap, one Tab stop.
  await page.keyboard.press('Shift+Tab')
  await expect(heatmap(page).locator('button[tabindex="0"]')).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(column(page, 'Sep 2026')).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(grid).toBeFocused()
  await expect(page).toHaveURL(/[?&]drill=merchant/)
  await expect(periodControl(page)).toHaveText('Jul 1 – Sep 30, 2026')
})

test('cash-flow columns stay even in the phone drawer, labeled or not', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await openMoneyflow(page, server)
  await page.getByRole('switch', { name: 'Charts' }).click()
  await expect(page.getByRole('dialog', { name: 'Moneyflow visualizations' })).toBeVisible()
  const columns = cashflow(page).getByRole('button')
  await expect(columns).not.toHaveCount(0)
  const widths = await columns.evaluateAll((buttons) =>
    buttons.map((button) => ({
      button: button.getBoundingClientRect().width,
      bars: button.querySelector('svg')!.getBoundingClientRect().width,
      label: button.querySelector('.mf-cashflow__tick')!.textContent,
    })),
  )
  expect(widths.some((width) => width.label !== '')).toBe(true)
  const first = widths[0]!.button
  for (const width of widths) {
    expect(Math.abs(width.button - first)).toBeLessThan(1)
    expect(Math.abs(width.bars - width.button)).toBeLessThan(0.5)
  }
  // The partial current month is hollow, down to its Net dot.
  await expect(column(page, 'Oct 2026')).toHaveClass(/mf-cashflow__column--partial/)
  await expect(cashflow(page).locator('.mf-cashflow__dot').last()).toHaveClass(
    /mf-cashflow__dot--partial/,
  )
})

function whereItWent(page: Page): Locator {
  return page.getByRole('list', { name: 'Where it went' })
}

function bar(page: Page, label: string): Locator {
  return whereItWent(page).getByRole('button', { name: new RegExp(`^${label}, \\d+\\.\\d%, -`) })
}

test('a composition bar click moves the cursor, and a double-click drills', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server, september)
  await expect(bar(page, 'Online Megastore')).toHaveAccessibleName(
    /^Online Megastore, \d+\.\d%, -[\d,]+\.\d\d$/,
  )
  await expect(whereItWent(page).getByText(/^\d+ others$/)).toBeVisible()
  await bar(page, 'Online Megastore').click()
  await expect(await activeRow(page)).toContainText('Online Megastore')
  await expect(bar(page, 'Online Megastore')).toHaveClass(/mf-bar--cursor/)

  // The switch shows Income from the same insights, without asking the server again.
  let requests = 0
  await page.route('**/view/insights', async (route) => {
    requests += 1
    await route.continue()
  })
  await page
    .getByRole('radiogroup', { name: 'Composition' })
    .getByRole('radio', { name: 'Income' })
    .click()
  await expect(
    whereItWent(page).getByRole('button', { name: /^Northwind Payroll, / }),
  ).toBeVisible()
  expect(requests).toBe(0)
  await page
    .getByRole('radiogroup', { name: 'Composition' })
    .getByRole('radio', { name: 'Spending' })
    .click()

  await bar(page, 'Online Megastore').dblclick()
  await expect(page).toHaveURL(/[?&]drill=merchant/)
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toContainText(
    'Online Megastore',
  )
})

test('the Time grouping shows the cursor day’s groups as the cursor moves', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server, september)
  await page.keyboard.press('5')
  const groups = page.getByRole('list', { name: /by group$/ })
  await expect(groups).toHaveAttribute('aria-label', 'Sep 1, 2026 by group')
  let requests = 0
  await page.route('**/view/insights', async (route) => {
    requests += 1
    await route.continue()
  })
  await page.keyboard.press('j')
  await expect(groups).toHaveAttribute('aria-label', 'Sep 2, 2026 by group')
  await expect(groups.getByRole('listitem')).not.toHaveCount(0)
  expect(requests).toBe(0)
})

function heatmap(page: Page): Locator {
  return page.getByRole('group', { name: 'Daily spending' })
}

test('a transaction view shows the month’s heatmap, and a day click scopes to it', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server, september)
  await page.keyboard.press('d')
  // Every September day up to the demo's today is a button, outlined on the cursor's date.
  await expect(heatmap(page).getByRole('button')).toHaveCount(30)
  const date = await (await activeRow(page)).locator('[data-column="date"]').innerText()
  const cursorDay = heatmap(page).locator('.mf-day--cursor')
  await expect(cursorDay).toHaveCount(1)
  await expect(cursorDay).toHaveAccessibleName(
    new RegExp(`^Sep ${Number(date.slice(8))}, 2026, out -[\\d,]+\\.\\d\\d`),
  )

  await heatmap(page)
    .getByRole('button', { name: /^Sep 15, 2026, / })
    .click()
  await expect(periodControl(page)).toHaveText('Sep 15, 2026')
  await expect(page).toHaveURL(/[?&]from=2026-09-15(&|$)/)
  await expect(page).toHaveURL(/[?&]to=2026-09-15(&|$)/)
  // A Day scope keeps the month's grid, with only that day at full strength.
  await expect(heatmap(page).getByRole('button')).toHaveCount(30)
  await expect(heatmap(page).locator('.mf-day:not(.mf-day--outside)')).toHaveCount(1)
})

test('a year of transactions shows the 53-week strip with future days blank', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server, 'cur=USD%3A2&from=2026-01-01&to=2026-12-31&v=1')
  await page.keyboard.press('d')
  await expect(heatmap(page)).toHaveClass(/mf-calendar--weeks/)
  // January 1 through the demo's today, October 7; the rest of the year is blank.
  await expect(heatmap(page).getByRole('button')).toHaveCount(280)
  await expect(heatmap(page).locator('.mf-day--future')).toHaveCount(85)
  const columns = await heatmap(page)
    .getByRole('button')
    .evaluateAll(
      (days) => new Set(days.map((day) => Math.round(day.getBoundingClientRect().x))).size,
    )
  expect(columns).toBe(41)
})

test('the strip ending at Latest keeps its month names inside the panel, apart', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  const strip = heatmap(page)
  await expect(strip).toHaveClass(/mf-calendar--weeks/)
  for (const width of [1440, 1280, 1024, 900]) {
    await page.setViewportSize({ width, height: 900 })
    await expect
      .poll(
        () =>
          strip.evaluate((node) => {
            const edge = node.getBoundingClientRect().right
            const shown = [...node.querySelectorAll('.mf-calendar__ticks > span')]
              .filter((tick) => getComputedStyle(tick).visibility === 'visible')
              .map((tick) => tick.getBoundingClientRect())
            const overhang = Math.max(...shown.map((box) => box.right)) - edge
            const overlaps = shown.slice(1).filter((box, index) => box.left < shown[index]!.right)
            return { overhang: overhang > 0.5, overlaps: overlaps.length }
          }),
        { message: `${width} px` },
      )
      .toEqual({ overhang: false, overlaps: 0 })
  }
  await page.setViewportSize({ width: 1440, height: 900 })
  await expect(strip.locator('.mf-calendar__ticks > span').last()).toBeVisible()
  await expect(strip.locator('.mf-calendar__ticks > span').last()).toHaveText('Oct')
})

for (const theme of ['light', 'dark'] as const) {
  test(`the ${theme} heatmap passes axe`, async ({ page, server }) => {
    await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), theme)
    await openMoneyflow(page, server, september)
    await page.keyboard.press('d')
    await expect(heatmap(page).getByRole('button')).toHaveCount(30)
    const month = await new AxeBuilder({ page }).include('.mf-chart-area').analyze()
    expect(month.violations).toEqual([])
    // A Day scope fades the other days; their numbers must still read.
    await heatmap(page)
      .getByRole('button', { name: /^Sep 15, 2026, / })
      .click()
    await expect(heatmap(page).locator('.mf-day--scope')).toHaveCount(1)
    const day = await new AxeBuilder({ page }).include('.mf-chart-area').analyze()
    expect(day.violations).toEqual([])
  })
}
