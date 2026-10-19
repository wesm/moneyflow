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
  // TEMPORARY CI diagnostic: report the geometry the reveal used.
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const diagnostic = await workspace(page).evaluate(
    (element, gridHandle) => {
      const grid = gridHandle as HTMLElement
      const box = (node: Element) => {
        const rect = node.getBoundingClientRect()
        return [rect.top, rect.bottom, rect.height]
      }
      const active = document.getElementById(grid.getAttribute('aria-activedescendant') ?? '')
      const header = grid.querySelector('[role="columnheader"]')
      const body = element.closest('.mf-workspace-body')
      return {
        scrollTop: element.scrollTop,
        clientHeight: element.clientHeight,
        offsetHeight: element.offsetHeight,
        scrollHeight: element.scrollHeight,
        clientWidth: element.clientWidth,
        scrollWidth: element.scrollWidth,
        workspace: box(element),
        gridOffsetTop: grid.offsetTop,
        grid: box(grid),
        header: header ? box(header) : null,
        headerRow: header?.parentElement ? box(header.parentElement) : null,
        active: active ? box(active) : null,
        bodyScrollTop: body?.scrollTop,
        bodyClientHeight: body?.clientHeight,
        fonts: getComputedStyle(grid).fontFamily,
        rowHeightVar: getComputedStyle(grid).getPropertyValue('--mf-row-height'),
      }
    },
    await grid.elementHandle(),
  )
  console.log(`CHARTS-DIAGNOSTIC ${JSON.stringify({ header, port, rowHeight, shown, diagnostic })}`)
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

test('scrolling the table leaves the charts in place above its sticky header', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
  const area = page.locator('.mf-chart-area')
  await expect(page.getByRole('region', { name: 'Charts' })).toHaveAttribute('aria-busy', 'false')
  const placed = (await area.boundingBox())!
  const port = (await workspace(page).boundingBox())!
  const header = (await page.getByRole('columnheader').first().boundingBox())!
  expect(header.y).toBeGreaterThanOrEqual(placed.y + placed.height)

  // Record the charts' top on every frame, so a jump between two checks still shows.
  await area.evaluate((element) => {
    const tops = new Set<number>()
    ;(window as unknown as { chartTops: Set<number> }).chartTops = tops
    const sample = () => {
      tops.add(Math.round(element.getBoundingClientRect().top))
      requestAnimationFrame(sample)
    }
    sample()
  })
  await page.mouse.move(port.x + port.width / 2, port.y + port.height - 20)
  for (let step = 0; step < 12; step++) await page.mouse.wheel(0, 150)
  await expect.poll(() => workspace(page).evaluate((element) => element.scrollTop)).toBe(1800)
  await page.keyboard.press('PageDown')
  await page.keyboard.press('j')
  await expectCursorRowShown(page)
  const keyed = await workspace(page).evaluate((element) => element.scrollTop)
  for (let step = 0; step < 4; step++) await page.mouse.wheel(0, -150)
  await expect
    .poll(() => workspace(page).evaluate((element) => element.scrollTop))
    .toBe(keyed - 600)

  // The header sticks to the top of the table's own scroller, below the charts.
  const headerRow = page.getByRole('grid', { name: 'Financial results' }).getByRole('row').first()
  expect(Math.abs((await headerRow.boundingBox())!.y - port.y)).toBeLessThanOrEqual(1)
  const tops = await page.evaluate(() => [
    ...(window as unknown as { chartTops: Set<number> }).chartTops,
  ])
  expect(tops).toEqual([Math.round(placed.y)])
  expect(await area.boundingBox()).toEqual(placed)
})

// In a short window the charts would take every pixel; the table keeps room for its rows, and
// the cursor row stays on screen as the keys move it.
for (const size of [
  { width: 820, height: 500 },
  { width: 1440, height: 500 },
]) {
  test(`a ${size.width}x${size.height} window keeps the table's rows reachable`, async ({
    page,
    server,
  }) => {
    await page.setViewportSize(size)
    await openMoneyflow(page, server)
    await page.keyboard.press('d')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    await expect(page.getByRole('region', { name: 'Charts' })).toBeVisible()
    const rowHeight = (await (await activeRow(page)).boundingBox())!.height
    const header = (await page.getByRole('columnheader').first().boundingBox())!
    const rows =
      ((await workspace(page).evaluate((element) => element.clientHeight)) - header.height) /
      rowHeight
    expect(rows).toBeGreaterThanOrEqual(5)

    for (let step = 0; step < 12; step++) {
      await page.keyboard.press('j')
      await expect(await activeRow(page)).toBeInViewport({ ratio: 1 })
    }
    await page.keyboard.press('End')
    await expect(await activeRow(page)).toBeInViewport({ ratio: 1 })
  })
}

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

test('an empty month inside a merchant keeps the strip and chart row above the empty table', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page
    .getByRole('radiogroup', { name: 'Period', exact: true })
    .getByRole('radio', { name: 'Month' })
    .click()
  const period = page.getByRole('banner').locator('button[aria-keyshortcuts="p"]')
  await expect(period).toHaveAccessibleName('Oct 2026')
  await page.getByRole('grid', { name: 'Financial results' }).focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/[?&]drill=merchant/)
  await page.getByRole('button', { name: 'Next period' }).click()
  await expect(period).toHaveAccessibleName('Nov 2026')

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
  return page.getByRole('banner').locator('button[aria-keyshortcuts="p"]')
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
  await expect(periodControl(page)).toHaveAccessibleName('All time')
  await expect(column(page, 'Sep 2026')).toHaveAccessibleName(
    /^Sep 2026, in \+[\d,]+\.\d\d, out -[\d,]+\.\d\d, net [+-][\d,]+\.\d\d$/,
  )
  await column(page, 'Sep 2026').click()
  await expect(periodControl(page)).toHaveAccessibleName('Sep 2026')
  await expect(page).toHaveURL(/[?&]from=2026-09-01&to=2026-09-30/)
  // The scope band now covers September, and the table keeps the focus.
  await expect(column(page, 'Sep 2026')).toHaveClass(/mf-cashflow__column--scope/)
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
})

test('a Year scope charts one column per year, and a year column scopes to it', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page
    .getByRole('radiogroup', { name: 'Period', exact: true })
    .getByRole('radio', { name: 'Year' })
    .click()
  await expect(periodControl(page)).toHaveAccessibleName('2026')
  const ticks = cashflow(page).locator('.mf-cashflow__tick')
  await expect(ticks).toHaveText(['2024', '2025', '2026'])
  // The year in progress names itself as running to date; its axis label stays short.
  await expect(column(page, '2026 \\(to date\\)')).toHaveClass(/mf-cashflow__column--scope/)
  await expect(column(page, '2025')).not.toHaveClass(/mf-cashflow__column--scope/)

  await column(page, '2025').click()
  await expect(periodControl(page)).toHaveAccessibleName('2025')
  await expect(page).toHaveURL(/[?&]from=2025-01-01&to=2025-12-31/)
  await expect(ticks).toHaveText(['2024', '2025'])
  await expect(column(page, '2025')).toHaveClass(/mf-cashflow__column--scope/)
})

test('Day columns name the month and year where they change', async ({ page, server }) => {
  await openMoneyflow(page, server, 'cur=USD%3A2&from=2025-12-15&to=2026-01-15&v=1')
  await page.keyboard.press('t')
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toHaveText('Days')
  const ticks = cashflow(page).locator('.mf-cashflow__tick')
  await expect(ticks.filter({ hasText: 'Jan 2026' })).toHaveCount(1)
  const labels = (await ticks.allTextContents()).filter((label) => label !== '')
  expect(labels[0]).toMatch(/^Dec \d+$/)
  expect(labels.slice(1).every((label) => /^(\d+|Jan 2026)$/.test(label))).toBe(true)
})

for (const theme of ['light', 'dark'] as const) {
  for (const width of [1000, 2200]) {
    test(`${theme} Day labels across a new year stay apart and inside the chart at ${width} px`, async ({
      page,
      server,
    }) => {
      await page.setViewportSize({ width, height: 900 })
      await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), theme)
      // The chart starts on Dec 31, so "Dec 31" and "Jan 2026" are the widest neighbors.
      await openMoneyflow(page, server, 'cur=USD%3A2&from=2025-12-31&to=2026-01-31&v=1')
      await page.keyboard.press('t')
      await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toHaveText('Days')
      const ticks = cashflow(page).locator('.mf-cashflow__tick')
      await expect(ticks.filter({ hasText: 'Jan 2026' })).toHaveCount(1)
      const plot = (await cashflow(page).boundingBox())!
      const shown = await ticks.evaluateAll((spans) =>
        spans
          .filter((span) => span.textContent !== '')
          .map((span) => {
            const { left, right } = span.getBoundingClientRect()
            return { text: span.textContent, left, right }
          }),
      )
      expect(shown.length).toBeGreaterThan(2)
      for (const [index, label] of shown.entries()) {
        expect(label.left, label.text!).toBeGreaterThanOrEqual(plot.x - 1)
        expect(label.right, label.text!).toBeLessThanOrEqual(plot.x + plot.width + 1)
        const next = shown[index + 1]
        if (next) expect(next.left - label.right, `${label.text} → ${next.text}`).toBeGreaterThan(4)
      }
    })
  }
}

test('a drag across cash-flow columns sets a custom range', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await column(page, 'Jul 2026').hover()
  await page.mouse.down()
  await column(page, 'Aug 2026').hover()
  await column(page, 'Sep 2026').hover()
  await page.mouse.up()
  await expect(periodControl(page)).toHaveAccessibleName('Jul 1 – Sep 30, 2026')
  await expect(page).toHaveURL(/[?&]from=2026-07-01&to=2026-09-30/)
})

test('a touch drag from a column’s bars sets a custom range', async ({
  page,
  server,
  browserName,
}) => {
  test.skip(browserName !== 'chromium', 'Touch input is sent through the Chromium protocol.')
  await openMoneyflow(page, server)
  // The press starts on the bars' drawing inside the column, not on the column's own padding.
  const points = await Promise.all(
    ['Jul 2026', 'Aug 2026', 'Sep 2026'].map(async (label) => {
      const box = (await column(page, label).locator('svg').boundingBox())!
      return { x: Math.round(box.x + box.width / 2), y: Math.round(box.y + box.height / 2) }
    }),
  )
  const touch = await page.context().newCDPSession(page)
  const send = (type: string, at: { x: number; y: number }[]) =>
    touch.send('Input.dispatchTouchEvent', { type, touchPoints: at })
  await send('touchStart', [points[0]!])
  await send('touchMove', [points[1]!])
  await send('touchMove', [points[2]!])
  await send('touchEnd', [])
  await expect(periodControl(page)).toHaveAccessibleName('Jul 1 – Sep 30, 2026')
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
  await expect(column(page, 'Oct 2026 \\(to date\\)')).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await expect(column(page, 'Sep 2026')).toBeFocused()
  await expect(page.getByRole('tooltip')).toContainText('Sep 2026')
  await page.keyboard.press('Enter')
  await expect(periodControl(page)).toHaveAccessibleName('Sep 2026')
  await expect(grid).toBeFocused()

  // Shift+Enter extends the scope to the focused column, as a shift-click does.
  await backThroughComposition(page)
  await page.keyboard.press('Shift+Tab')
  await expect(column(page, 'Sep 2026')).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await page.keyboard.press('ArrowLeft')
  await expect(column(page, 'Jul 2026')).toBeFocused()
  await page.keyboard.press('Shift+Enter')
  await expect(periodControl(page)).toHaveAccessibleName('Jul 1 – Sep 30, 2026')
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
  await expect(periodControl(page)).toHaveAccessibleName('Jul 1 – Sep 30, 2026')
})

test('a historical custom range names the days its clipped edge months cover', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server, 'cur=USD%3A2&from=2026-01-15&to=2026-05-10&v=1')
  await expect(column(page, 'Jan 2026 \\(Jan 15–31\\)')).toHaveClass(/mf-cashflow__column--partial/)
  await expect(column(page, 'May 2026 \\(May 1–10\\)')).toHaveClass(/mf-cashflow__column--partial/)
  await expect(column(page, 'Mar 2026')).not.toHaveClass(/mf-cashflow__column--partial/)
  await expect(cashflow(page).getByRole('button', { name: /to date/ })).toHaveCount(0)
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
  // The partial current month keeps the colors of a complete one at a lighter strength.
  const paint = (marks: Locator) =>
    marks.evaluateAll((nodes) =>
      nodes.map((node) => {
        const style = getComputedStyle(node)
        return { fill: style.fill, opacity: Number(style.fillOpacity) }
      }),
    )
  // The cursor row's share stays at full strength, so it reads over the lighter bars.
  const october = column(page, 'Oct 2026 \\(to date\\)')
  const bars = 'rect:not(.mf-cashflow__cursor)'
  const partial = await paint(october.locator(bars))
  const full = await paint(column(page, 'Sep 2026').locator(bars))
  expect(partial.map((mark) => mark.fill)).toEqual(full.map((mark) => mark.fill))
  expect(full.map((mark) => mark.opacity)).toEqual([1, 1])
  expect(partial.map((mark) => mark.opacity)).toEqual([0.5, 0.5])
  const cursor = await paint(october.locator('.mf-cashflow__cursor'))
  expect(cursor.map((mark) => mark.opacity)).toEqual([1, 1])
  const dots = await paint(cashflow(page).locator('.mf-cashflow__dot'))
  expect(dots.slice(0, -1).every((dot) => dot.opacity === 1)).toBe(true)
  expect(dots.at(-1)!.opacity).toBeLessThan(1)
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
  await expect(bar(page, 'Online Megastore')).toHaveClass(/mf-bar--cursor/)
  await expect(await activeRow(page)).toContainText('Online Megastore')

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

test('@smoke the composition list moves with arrows and drills with Enter', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server, september)
  const bars = whereItWent(page).getByRole('button')
  // The list is one Tab stop, reached from the table going back; it starts on the cursor's bar.
  await page.keyboard.press('Shift+Tab')
  await expect(bars.first()).toBeFocused()
  await page.keyboard.press('ArrowDown')
  await expect(bars.nth(1)).toBeFocused()
  await expect(bars.nth(1)).toHaveAccessibleName(/^Online Megastore, /)
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/[?&]drill=merchant/)
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toContainText(
    'Online Megastore',
  )
})

test('a double-click drills a composition bar whose row is below the table’s edge', async ({
  page,
  server,
}) => {
  // Short enough that the last bar's row is below the visible table rows.
  await page.setViewportSize({ width: 1440, height: 500 })
  await openMoneyflow(page, server, september)
  const target = whereItWent(page).getByRole('button').last()
  const label = (await target.getAttribute('aria-label'))!.split(',')[0]!
  const box = (await target.boundingBox())!
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  // A person's double-click: the second press comes a moment after the first click.
  await page.mouse.down()
  await page.mouse.up()
  await page.waitForTimeout(150)
  await page.mouse.down({ clickCount: 2 })
  await page.mouse.up({ clickCount: 2 })
  await expect(page).toHaveURL(/[?&]drill=merchant/)
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toContainText(label)
})

test('a composition click moves the cursor at once and scrolls to its row after a pause', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 1440, height: 500 })
  await openMoneyflow(page, server, september)
  const target = whereItWent(page).getByRole('button').last()
  const label = (await target.getAttribute('aria-label'))!.split(',')[0]!
  await target.click()
  await expect(target).toHaveClass(/mf-bar--cursor/)
  // The scroll waits out the double-click time, then shows the cursor row. This window is too
  // short for the charts and the table, so the charts scroll away above the table.
  await expect
    .poll(() => page.locator('.mf-workspace-body').evaluate((element) => element.scrollTop))
    .toBeGreaterThan(0)
  await expect(await activeRow(page)).toContainText(label)
  await expectCursorRowShown(page)
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

  // Only the day under the pointer has a tooltip, and it opens next to that day.
  const fifteenth = heatmap(page).getByRole('button', { name: /^Sep 15, 2026, / })
  await fifteenth.hover()
  await expect(page.getByRole('tooltip')).toContainText('Sep 15, 2026')
  await expect(heatmap(page).locator('.kit-tooltip-trigger')).toHaveCount(1)
  await fifteenth.click()
  await expect(periodControl(page)).toHaveAccessibleName('Sep 15, 2026')
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

/** Whether the strip's shown month names pass its right edge or cover the name before. */
async function expectTicksApart(strip: Locator, widths: number[]): Promise<void> {
  for (const width of widths) {
    await strip.page().setViewportSize({ width, height: 900 })
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
            return { overhang: overhang > 0.5, overlaps: overlaps.length, few: shown.length < 3 }
          }),
        { message: `${width} px` },
      )
      .toEqual({ overhang: false, overlaps: 0, few: false })
  }
}

test('the strip ending at Latest keeps its month names inside the panel, apart', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  const strip = heatmap(page)
  await expect(strip).toHaveClass(/mf-calendar--weeks/)
  await expectTicksApart(strip, [1440, 1280, 1024, 900])
  await page.setViewportSize({ width: 1440, height: 900 })
  await expect(strip.locator('.mf-calendar__ticks > span').last()).toBeVisible()
  await expect(strip.locator('.mf-calendar__ticks > span').last()).toHaveText('Oct')
})

test('the strip keeps its month names apart however wide the font draws them', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  // Wider than common fonts draw the names, standing in for a machine whose fonts run wide.
  await page.addStyleTag({ content: '.mf-calendar__ticks { letter-spacing: 3px; }' })
  await page.keyboard.press('d')
  const strip = heatmap(page)
  await expect(strip).toHaveClass(/mf-calendar--weeks/)
  await expectTicksApart(strip, [1440, 900])
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
