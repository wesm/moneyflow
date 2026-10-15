import type { Locator, Page, Route } from '@playwright/test'

import { activeRow, cell, expect, openMoneyflow, test } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

test('desktop and medium layouts put the charts above a full-width table', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const charts = page.getByRole('region', { name: 'Charts' })
  await expect(grid).toBeVisible()
  await expect(charts).toBeVisible()
  await expect(page.locator('.mf-kpis').getByText('Income', { exact: true })).toBeVisible()
  // The selection header holds only the "Select all matching" box.
  await expect(grid.getByRole('columnheader')).toHaveText([
    '',
    'Merchant',
    'Top category',
    'Count',
    'In',
    'Out',
    'Net',
    'Share',
    'Trend',
  ])
  const amountHeaders = await Promise.all(
    ['Count', 'In', 'Out', 'Net'].map((name) =>
      grid.getByRole('columnheader', { name, exact: true }).boundingBox(),
    ),
  )
  for (const header of amountHeaders) {
    expect(header!.width).toBe(amountHeaders[0]!.width)
  }
  expect(await page.evaluate(() => scrollY)).toBe(0)
  await expectChartsAbove(charts, grid)
  await expectChartRow(charts, 'beside')

  await page.setViewportSize({ width: 1024, height: 768 })
  const toggle = page.getByRole('switch', { name: 'Charts' })
  await expect(toggle).toBeChecked()
  await expect(charts).toBeVisible()
  await expectChartRow(charts, 'beside')
  const lastHeader = (await grid.getByRole('columnheader').last().boundingBox())!
  const mediumGrid = (await grid.boundingBox())!
  expect(lastHeader.x).toBeGreaterThanOrEqual(mediumGrid.x)
  expect(lastHeader.x + lastHeader.width).toBeLessThanOrEqual(mediumGrid.x + mediumGrid.width + 1)

  await toggle.uncheck()
  await expect(charts).toHaveCount(0)
  await expect(page.locator('.mf-kpis').getByText('Income', { exact: true })).toBeVisible()
  await toggle.check()

  await page.setViewportSize({ width: 820, height: 700 })
  await expect(charts).toBeVisible()
  await expectChartRow(charts, 'stacked')
  await expectChartsAbove(charts, grid)
  const bounded = (await charts.boundingBox())!
  expect(bounded.x + bounded.width).toBeLessThanOrEqual(820)
})

/** The chart row sits above the grid and inside the workspace's width. */
async function expectChartsAbove(charts: Locator, grid: Locator): Promise<void> {
  const chartBox = (await charts.boundingBox())!
  const gridBox = (await grid.boundingBox())!
  expect(chartBox.y + chartBox.height).toBeLessThanOrEqual(gridBox.y)
  expect(chartBox.x).toBeGreaterThanOrEqual(gridBox.x - 1)
  expect(chartBox.x + chartBox.width).toBeLessThanOrEqual(gridBox.x + gridBox.width + 1)
}

/**
 * The cash-flow chart and the right panel: two-thirds and one-third side by side from 900 px,
 * the panel below the cash-flow chart under that.
 */
async function expectChartRow(charts: Locator, layout: 'beside' | 'stacked'): Promise<void> {
  const slots = charts.locator(':scope > *')
  await expect(slots).toHaveCount(2)
  const cashflow = (await slots.nth(0).boundingBox())!
  const panel = (await slots.nth(1).boundingBox())!
  if (layout === 'stacked') {
    expect(panel.y).toBeGreaterThanOrEqual(cashflow.y + cashflow.height)
    expect(Math.abs(panel.width - cashflow.width)).toBeLessThanOrEqual(1)
    return
  }
  expect(Math.abs(panel.y - cashflow.y)).toBeLessThanOrEqual(1)
  expect(cashflow.width / panel.width).toBeCloseTo(2, 0)
}

test('a grouped view at 960 px never draws the column it drops, on load or resize', async ({
  page,
  server,
}) => {
  await page.addInitScript(() => {
    const seen = new Set<string>()
    Object.assign(window, { droppedHeadersSeen: seen })
    // Records every header added, even one a later microtask removes before the next frame.
    const header = '.mf-table__header [data-column="context"]'
    new MutationObserver((records) => {
      for (const node of records.flatMap((record) => [...record.addedNodes])) {
        if (node instanceof Element && (node.matches(header) || node.querySelector(header))) {
          seen.add('context')
        }
      }
    }).observe(document, { childList: true, subtree: true })
  })
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const seen = () =>
    page.evaluate(() => [
      ...(window as unknown as { droppedHeadersSeen: Set<string> }).droppedHeadersSeen,
    ])
  await page.setViewportSize({ width: 960, height: 900 })
  await openMoneyflow(page, server, 'cur=USD%3A2&from=2026-09-01&to=2026-09-30&v=1')
  await expect(grid.locator('.mf-table__header [data-column="vs_prior"]')).toHaveText('vs Aug')
  await expect(grid.locator('.mf-table__header [data-column="context"]')).toHaveCount(0)
  expect(await seen()).toEqual([])

  // The width last measured at 1440 px is stale by the time phone rows grow back to 960 px.
  await page.setViewportSize({ width: 1440, height: 900 })
  await expect(grid.locator('.mf-table__header [data-column="context"]')).toHaveCount(1)
  await page.evaluate(() =>
    (window as unknown as { droppedHeadersSeen: Set<string> }).droppedHeadersSeen.clear(),
  )
  await page.setViewportSize({ width: 390, height: 900 })
  await expect(page.locator('.mf-table--compact')).toHaveCount(1)
  await page.setViewportSize({ width: 960, height: 900 })
  await expect(grid.locator('.mf-table__header [data-column="vs_prior"]')).toHaveText('vs Aug')
  await expect(grid.locator('.mf-table__header [data-column="context"]')).toHaveCount(0)
  expect(await seen()).toEqual([])
})

test('Trend and vs prior fill grouped rows, and columns drop instead of scrolling', async ({
  page,
  server,
}) => {
  const series: number[] = []
  page.on('request', (request) => {
    const rows = request.url().endsWith('/view/insights') && request.postDataJSON()?.rows
    if (rows) series.push(rows.length)
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await openMoneyflow(page, server, 'cur=USD%3A2&from=2026-09-01&to=2026-09-30&v=1')
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const header = (key: string) => grid.locator(`.mf-table__header [data-column="${key}"]`)
  await expect(header('trend')).toHaveText('Trend')
  await expect(header('vs_prior')).toHaveText('vs Aug')
  await expect(header('vs_prior')).not.toHaveAttribute('aria-sort')
  const megastore = grid.getByRole('row').filter({ hasText: 'Online Megastore' })
  await expect(cell(megastore, 'vs_prior')).toHaveText(/^▲ 15\.1%\s*up 15\.1% vs Aug$/)
  await expect(cell(megastore, 'vs_prior').locator('.mf-tone--unfavorable')).toHaveCount(1)
  const transit = grid.getByRole('row').filter({ hasText: 'Metro Transit' })
  await expect(cell(transit, 'vs_prior').locator('.mf-tone--favorable')).toHaveText(/^▼ 20\.0%/)
  const sparkline = cell(megastore, 'trend').locator('svg')
  await expect(sparkline).toHaveAttribute('aria-hidden', 'true')
  await expect(sparkline.locator('polyline')).toHaveCount(1)
  const requested = series.length
  expect(requested).toBeGreaterThan(0)

  const workspace = page.locator('.mf-workspace')
  const toggle = page.getByRole('switch', { name: 'Charts' })
  const shownKeys = async () =>
    grid
      .locator('.mf-table__header [data-column]')
      .evaluateAll((cells) => cells.map((node) => (node as HTMLElement).dataset['column']))
  // Workspaces 1264, 1104, 976, 912, and 852 px wide: the full sidebar takes 15rem at 1440 px
  // and wider, and the rail 3.5rem from 901 to 1439 px.
  for (const width of [1504, 1160, 1032, 968, 908]) {
    await page.setViewportSize({ width, height: 900 })
    for (const charts of [true, false]) {
      await toggle.setChecked(charts)
      await expect(page.getByRole('region', { name: 'Charts' })).toHaveCount(charts ? 1 : 0)
      const context = `${width} px, Charts ${charts ? 'on' : 'off'}`
      await expect
        .poll(() => workspace.evaluate((node) => node.scrollWidth - node.clientWidth), {
          message: context,
        })
        .toBe(0)
      const shown = (await workspace.boundingBox())!
      const last = (await header('net').boundingBox())!
      expect(last.x + last.width, context).toBeLessThanOrEqual(shown.x + shown.width)
      // Every row has a cell under every header.
      const keys = await shownKeys()
      const cells = await megastore
        .locator('[role="gridcell"]')
        .evaluateAll((nodes) => nodes.map((node) => (node as HTMLElement).dataset['column']))
      expect(cells, context).toEqual(keys)
    }
    const keys = await shownKeys()
    if (width >= 1160) expect(keys).toContain('context')
    if (width === 1032) {
      expect(keys).not.toContain('context')
      expect(keys.slice(-2)).toEqual(['trend', 'vs_prior'])
    }
    if (width === 908) expect(keys).not.toContain('trend')
  }
  // Trend comes back from the cached series, without asking the server again.
  await page.setViewportSize({ width: 1440, height: 900 })
  await expect(header('context')).toHaveText('Top category')
  await expect(sparkline.locator('polyline')).toHaveCount(1)
  expect(series).toHaveLength(requested)

  // The longest everyday label, the partial year's, fits its header.
  await openMoneyflow(page, server, 'cur=USD%3A2&from=2026-01-01&to=2026-12-31&v=1')
  await expect(header('vs_prior')).toHaveText('vs 2025 to date')
  expect(await header('vs_prior').evaluate((node) => node.scrollWidth - node.clientWidth)).toBe(0)

  // All has no prior range, so only Trend joins the grouped columns.
  await openMoneyflow(page, server)
  await expect(header('trend')).toHaveText('Trend')
  await expect(header('vs_prior')).toHaveCount(0)
  await expect(grid.locator('[data-column="trend"] svg').first()).toBeVisible()

  await page.setViewportSize({ width: 700, height: 900 })
  await expect(header('trend')).toHaveCount(0)
  await expect(grid.locator('[data-column="trend"]')).toHaveCount(0)
})

test('narrow resize preserves analytical URL and moves charts into a drawer', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('g')
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toHaveText('Categories')
  const analyticalURL = page.url()

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page).toHaveURL(analyticalURL)
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeVisible()
  // Phones show no KPI strip inline; the Charts switch opens it in a drawer.
  await expect(page.locator('.mf-kpis').getByText('Income', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('region', { name: 'Charts' })).toHaveCount(0)
  const rows = page.getByRole('grid', { name: 'Financial results' }).getByRole('row')
  expect(await rows.count()).toBeGreaterThan(1)

  await expectCompactRows(page)

  const charts = page.getByRole('switch', { name: 'Charts' })
  await expect(charts).not.toBeChecked()
  await charts.check()
  const drawer = page.getByRole('dialog', { name: 'Moneyflow visualizations' })
  await expect(drawer).toBeVisible()
  await expect(drawer.locator('.mf-kpis').getByText('Income', { exact: true })).toBeVisible()
  await expectChartRow(drawer.getByRole('region', { name: 'Charts' }), 'stacked')
  await expect(page).toHaveURL(analyticalURL)

  await page.setViewportSize({ width: 820, height: 844 })
  await expect(drawer).toHaveCount(0)
  await expect(page.getByRole('region', { name: 'Charts' })).toBeVisible()
})

test('the sidebar groups by its entries and the rail names them in flyouts', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const nav = page.getByRole('navigation', { name: 'Workspace' })
  const categories = nav.getByRole('link', { name: 'Categories', exact: true })
  await categories.click()
  await expect(grid.locator('[role="columnheader"][data-column="name"]')).toHaveText('Category')
  await expect(categories).toHaveAttribute('aria-current', 'page')
  await expect(grid).toBeFocused()
  expect(new URL(page.url()).hash).toBe('')

  await nav.getByRole('link', { name: 'Groups', exact: true }).focus()
  await page.keyboard.press('Enter')
  await expect(grid.locator('[role="columnheader"][data-column="name"]')).toHaveText('Group')
  // Enter followed the link without also drilling the cursor row. Such a drill would land just
  // before Groups in the history, so Back shows it.
  await page.goBack()
  await expect(page).not.toHaveURL(/[?&]drill=/)
  await expect(grid.locator('[role="columnheader"][data-column="name"]')).toHaveText('Category')

  await page.setViewportSize({ width: 1024, height: 768 })
  await nav.getByRole('link', { name: 'Merchants' }).hover()
  // A flyout without views repeats the link's name, so kit hides it from assistive tech.
  const flyout = nav.locator('.kit-app-sidebar__flyout')
  await expect(flyout).toBeVisible()
  await expect(flyout).toHaveText('Merchants')
  await expect(nav.getByRole('group', { name: 'Merchants' })).toHaveCount(0)
})

test('the rail expands to the full sidebar, stays expanded after a reload, and collapses', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 })
  await openMoneyflow(page, server)
  const nav = page.getByRole('navigation', { name: 'Workspace' })
  const collapse = page.getByRole('button', { name: 'Collapse sidebar' })
  const expand = page.getByRole('button', { name: 'Expand sidebar' })
  const card = nav.getByRole('status', { name: 'Pending profile changes' })
  await expect(expand).toBeVisible()
  await expect(card).toHaveCount(0)

  await expand.click()
  await expect(card).toBeVisible()

  await page.reload()
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  await expect(collapse).toBeVisible()

  // The full sidebar always shows at 1440 px and wider, so it offers nothing to collapse.
  await page.setViewportSize({ width: 1440, height: 900 })
  await expect(collapse).toHaveCount(0)
  await page.setViewportSize({ width: 1280, height: 800 })

  await collapse.focus()
  await page.keyboard.press('Enter')
  await expect(card).toHaveCount(0)
})

const phone = { width: 390, height: 844 }

/**
 * Phone rows: two lines at least 52 px tall, a header with only select-all and the sort field,
 * no sideways scrolling in the grid or any row, and 44 px tap areas and buttons.
 */
async function expectCompactRows(page: Page): Promise<void> {
  const grid = page.getByRole('grid', { name: 'Financial results' })
  await expect(grid.locator('.mf-table__header [role="columnheader"]')).toHaveCount(2)
  await expect(grid.getByRole('button', { name: 'Select all matching' })).toBeVisible()
  const layout = await grid.evaluate((element) => {
    const rows = [...element.querySelectorAll<HTMLElement>('[role="row"][aria-rowindex]')]
    const workspace = element.closest<HTMLElement>('.mf-workspace')!
    const wide = [element, workspace, ...rows].filter((node) => node.scrollWidth > node.clientWidth)
    const controls = [
      ...rows.map((row) => row.querySelector<HTMLElement>('[data-column="select"]')!),
      ...element.querySelectorAll<HTMLElement>('button'),
    ]
    const small = controls
      .map((control) => control.getBoundingClientRect())
      .filter((box) => box.width < 44 || box.height < 44)
    const heights = rows.map((row) => row.getBoundingClientRect().height)
    return { rows: rows.length, wide: wide.length, small: small.length, low: Math.min(...heights) }
  })
  expect(layout.rows).toBeGreaterThan(1)
  expect(layout).toMatchObject({ wide: 0, small: 0 })
  expect(layout.low).toBeGreaterThanOrEqual(52)
}

/**
 * The page cannot scroll sideways and every visible control in the sidebar's top bar and the
 * command bar ends inside the viewport. The shell hides overflow, so a scroll width alone would
 * not show a clipped control.
 */
async function expectNoHorizontalScroll(page: Page): Promise<void> {
  const overflow = await page.evaluate(() => {
    const root = document.documentElement
    const selector = 'button, input, [role="radio"], [role="combobox"], label.kit-toggle'
    const controls = Array.from(
      document.querySelectorAll<HTMLElement>(`:is(header, .mf-command-bar) :is(${selector})`),
    )
    const outside = controls
      .filter((element) => element.getClientRects().length > 0)
      .filter((element) => element.getBoundingClientRect().right > root.clientWidth + 0.5)
      .map((element) => element.textContent?.trim() || element.ariaLabel)
    return { scrolls: root.scrollWidth > root.clientWidth, outside }
  })
  expect(overflow).toEqual({ scrolls: false, outside: [] })
}

/** Every visible control inside `container` is at least 44 px tall and, if square, wide. */
async function expectTouchTargets(container: Locator): Promise<void> {
  const small = await container.evaluate((root) => {
    const selector =
      'a[href], button, [role="radio"], [role="combobox"], [role="option"], label.kit-toggle, ' +
      '.kit-text-input'
    return Array.from(root.querySelectorAll<HTMLElement>(selector))
      .filter((element) => element.getClientRects().length > 0)
      .map((element) => ({
        name: element.textContent?.trim() || element.ariaLabel,
        box: element.getBoundingClientRect(),
      }))
      .filter(({ box }) => box.height < 44 || box.width < 44)
      .map(({ name, box }) => `${name} ${Math.round(box.width)}×${Math.round(box.height)}`)
  })
  expect(small).toEqual([])
}

const focusInside = (dialog: Locator) =>
  dialog.evaluate((node) => node.contains(document.activeElement))

test('@smoke the phone drawer holds the sidebar and closes on Esc', async ({ page, server }) => {
  await page.setViewportSize(phone)
  await openMoneyflow(page, server)
  await expectNoHorizontalScroll(page)
  await expectTouchTargets(page.getByRole('banner'))
  const url = page.url()
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const cursor = await grid.getAttribute('aria-activedescendant')
  const menu = page.getByRole('button', { name: 'Open navigation' })
  await menu.click()
  const drawer = page.getByRole('dialog', { name: 'Navigation' })
  await expect(drawer.getByRole('navigation', { name: 'Workspace' })).toBeVisible()
  await expect(page.getByText(/0 pending edit/)).toHaveCount(1)
  await expect.poll(() => focusInside(drawer)).toBe(true)
  await expectNoHorizontalScroll(page)

  // Keys pressed in the drawer stay there: j does not move the grid cursor behind it.
  await page.keyboard.press('j')
  await page.keyboard.press('Escape')
  await expect(drawer).toBeHidden()
  await expect(menu).toBeFocused()
  await expect(page).toHaveURL(url)
  expect(await grid.getAttribute('aria-activedescendant')).toBe(cursor)

  await menu.click()
  await drawer.getByRole('link', { name: 'Categories', exact: true }).click()
  await expect(drawer).toBeHidden()
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toHaveText('Categories')
  await expect(grid).toBeFocused()

  await menu.click()
  await drawer.getByRole('link', { name: 'Edit categories', exact: true }).click()
  await expect(drawer).toBeHidden()
  await expect(
    page.getByRole('dialog', { name: 'Manage categories' }).getByLabel('Filter categories'),
  ).toBeFocused()
})

test.describe('the phone drawer by touch', () => {
  test.use({ hasTouch: true, viewport: phone })

  test('the drawer entries and footer controls are touch-sized', async ({ page, server }) => {
    await openMoneyflow(page, server)
    await page.getByRole('button', { name: 'Open navigation' }).tap()
    const drawer = page.getByRole('dialog', { name: 'Navigation' })
    await expect(drawer.getByRole('link', { name: 'Categories', exact: true })).toBeVisible()
    await expectTouchTargets(drawer)
  })
})

test('Review in the phone drawer closes the drawer and moves focus into the review', async ({
  page,
}) => {
  const server = await startE2EServer()
  try {
    await page.setViewportSize(phone)
    await openMoneyflow(page, server)
    await page.keyboard.press('h')
    // At phone width the hide's notice has the status line in place of the pending text.
    await expect(page.getByRole('status', { name: 'Notice' })).toHaveText(
      /^Hidden \d+ transactions? · Undo u$/,
    )
    await page.getByRole('button', { name: 'Open navigation' }).click()
    const drawer = page.getByRole('dialog', { name: 'Navigation' })
    // kit closes the drawer for a link. For a button in its footer, moneyflow closes it first.
    await drawer.getByRole('button', { name: 'Review pending changes' }).click()
    await expect(drawer).toBeHidden()
    const review = page.getByRole('dialog', { name: 'Review pending changes' })
    await expect(review).toBeVisible()
    await expect.poll(() => focusInside(review)).toBe(true)

    // Closing the review returns focus to the menu button: the Review button left with the drawer.
    await page.keyboard.press('Escape')
    await expect(review).toBeHidden()
    await expect(page.getByRole('button', { name: 'Open navigation' })).toBeFocused()
  } finally {
    await server.stop()
  }
})

test('a write confirmation that arrives with the phone drawer open takes over from it', async ({
  page,
  server,
}) => {
  let confirming = false
  await page.route('**/provider/write-status', async (route) => {
    if (!confirming) return route.continue()
    await route.fulfill({
      json: {
        version: '1',
        revision: '1',
        generation: '1',
        batch_version: '1',
        phase: 'reconcile_confirmation_required',
        total: 4,
        completed: 1,
        failed: 1,
        remaining: 2,
        overrides: 0,
        actions: ['confirm'],
      },
    })
  })
  await page.setViewportSize(phone)
  await openMoneyflow(page, server)
  await page.getByRole('button', { name: 'Open navigation' }).click()
  const drawer = page.getByRole('dialog', { name: 'Navigation' })
  await expect(drawer.getByRole('navigation', { name: 'Workspace' })).toBeVisible()

  // The next status poll, within a second, asks to confirm the reconciliation.
  confirming = true
  const write = page.getByRole('dialog', { name: 'Monarch write status' })
  await expect(write).toBeVisible()
  await expect(drawer).toBeHidden()
  await expect.poll(() => focusInside(write)).toBe(true)
})

test('the View sheet changes the period and filters at phone width', async ({ page, server }) => {
  await page.setViewportSize(phone)
  await openMoneyflow(page, server)
  const bar = page.getByRole('region', { name: 'View controls' })
  await expect(bar.getByRole('radiogroup', { name: 'Period', exact: true })).toHaveCount(0)
  await expectTouchTargets(bar)
  const view = bar.getByRole('button', { name: 'View', exact: true })
  await expect(view).toHaveAttribute('aria-haspopup', 'dialog')
  await view.click()
  const sheet = page.getByRole('dialog', { name: 'View' })
  for (let press = 0; press < 12; press += 1) {
    await page.keyboard.press('Tab')
    expect(await focusInside(sheet)).toBe(true)
  }
  await page.keyboard.press('Shift+Tab')
  expect(await focusInside(sheet)).toBe(true)
  const filtersBox = (await sheet.getByRole('button', { name: 'Filters' }).boundingBox())!
  const currencyBox = (await sheet.getByRole('combobox', { name: /^Currency/ }).boundingBox())!
  expect(Math.round(filtersBox.width)).toBe(Math.round(currencyBox.width))
  await sheet.getByRole('radio', { name: 'Month' }).click()
  await expect(
    bar.locator('button[aria-haspopup="dialog"][aria-keyshortcuts="t"]'),
  ).toHaveAccessibleName('Oct 2026')
  await expect(sheet.getByRole('radio', { name: 'Month' })).toBeChecked()

  await sheet.getByRole('button', { name: 'Filters' }).click()
  await page.getByRole('button', { name: 'Show transfers' }).click()
  await expect(page).toHaveURL(/[?&]transfers=/)
  await expectTouchTargets(sheet)
  await expectNoHorizontalScroll(page)

  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: 'Show transfers' })).toBeHidden()
  await expect(sheet).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(sheet).toBeHidden()
  await expect(view).toBeFocused()
})

test('the phone search icon reveals the field, which stays while a search applies', async ({
  page,
  server,
}) => {
  await page.setViewportSize(phone)
  await openMoneyflow(page, server)
  const searchbox = page.getByRole('searchbox', { name: 'Search transactions' })
  await expect(searchbox).toBeHidden()
  const icon = page.getByRole('button', { name: 'Search', exact: true })
  await icon.click()
  await expect(searchbox).toBeFocused()
  const box = (await searchbox.boundingBox())!
  expect(box.width).toBeGreaterThan(phone.width / 2)
  await expectTouchTargets(page.getByRole('region', { name: 'View controls' }))
  await searchbox.fill('noodle')
  await expect(page).toHaveURL(/[?&]q=noodle(?:&|$)/)
  await page.keyboard.press('Enter')
  await expect(icon).toBeFocused()
  await expect(page).toHaveURL(/[?&]q=noodle(?:&|$)/)
  await expect(searchbox).toHaveValue('noodle')
  await expectNoHorizontalScroll(page)

  await page.getByRole('button', { name: 'Clear search' }).click()
  await page.keyboard.press('Enter')
  await expect(page).not.toHaveURL(/[?&]q=/)
  await expect(searchbox).toBeHidden()
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()

  await icon.click()
  await expect(searchbox).toBeFocused()
  await expect(icon).toHaveAttribute('aria-pressed', 'true')
  await icon.click()
  await expect(searchbox).toBeHidden()
  await expect(icon).toHaveAttribute('aria-pressed', 'false')

  await icon.click()
  await expect(searchbox).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(searchbox).toBeHidden()
  await expect(icon).toBeFocused()

  await icon.click()
  await searchbox.pressSequentially('pizz')
  await expect(page).toHaveURL(/[?&]q=pizz(?:&|$)/)
  await page.setViewportSize({ width: 700, height: 844 })
  await expect(searchbox).toHaveValue('pizz')
  await page.setViewportSize(phone)

  // The switch shows the chart row at 700 px; wait for it to become the phone's dialog switch.
  const charts = page.getByRole('switch', { name: 'Charts' })
  await expect(charts).not.toBeChecked()
  await charts.check()
  await expect(page.getByRole('dialog', { name: 'Moneyflow visualizations' })).toBeVisible()
})

test('the phone search field stays while its applied search is cleared', async ({
  page,
  server,
}) => {
  await page.setViewportSize(phone)
  await openMoneyflow(page, server)
  const searchbox = page.getByRole('searchbox', { name: 'Search transactions' })
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await searchbox.fill('noodle')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/[?&]q=noodle(?:&|$)/)

  await searchbox.click()
  await searchbox.fill('')
  await expect(page).not.toHaveURL(/[?&]q=/)
  await expect(searchbox).toBeVisible()
  await expect(searchbox).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page).toHaveURL(/[?&]q=noodle(?:&|$)/)
  await expect(searchbox).toHaveValue('noodle')
})

test('narrowing past the phone width keeps typing in the search field', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 1024, height: 768 })
  await openMoneyflow(page, server)
  const searchbox = page.getByRole('searchbox', { name: 'Search transactions' })
  await page.keyboard.press('/')
  await expect(searchbox).toBeFocused()
  await page.setViewportSize(phone)
  await expect(page.getByRole('button', { name: 'Open navigation' })).toBeVisible()
  await expect(searchbox).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(searchbox).toBeHidden()

  await page.setViewportSize({ width: 1024, height: 768 })
  await page.keyboard.press('/')
  await searchbox.pressSequentially('pizz')
  await expect(page).toHaveURL(/[?&]q=pizz(?:&|$)/)
  await page.setViewportSize(phone)
  await expect(page.getByRole('button', { name: 'Open navigation' })).toBeVisible()
  await expect(searchbox).toBeFocused()
  await expect(searchbox).toHaveValue('pizz')
  await page.keyboard.type('a')
  await expect(searchbox).toHaveValue('pizza')
  await expect(page).toHaveURL(/[?&]q=pizza(?:&|$)/)
})

test('from 641 to 900 px the top bar opens the sidebar and the controls stay inline', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 700, height: 800 })
  await openMoneyflow(page, server)
  await expect(page.getByRole('button', { name: 'Open navigation' })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Workspace' })).toHaveCount(0)
  const bar = page.getByRole('region', { name: 'View controls' })
  await expect(bar.getByRole('button', { name: 'View', exact: true })).toHaveCount(0)
  await expect(bar.getByRole('radiogroup', { name: 'Period', exact: true })).toBeVisible()
  await expect(bar.getByRole('searchbox', { name: 'Search transactions' })).toBeVisible()
  await expectNoHorizontalScroll(page)
})

test('a cross-year period label shrinks to fit the phone command bar', async ({ page, server }) => {
  await page.setViewportSize(phone)
  await openMoneyflow(page, server, 'cur=USD%3A2&from=2025-11-15&to=2026-02-14&v=1')
  const label = page
    .getByRole('region', { name: 'View controls' })
    .locator('button[aria-keyshortcuts="t"]')
  await expect(label).toHaveAccessibleName('Nov 15, 2025 – Feb 14, 2026')
  await expect(label).toHaveAttribute('title', /^Nov 15, 2025 – Feb 14, 2026/)
  await expectNoHorizontalScroll(page)
})

test('growing past 900 px closes the drawer and focuses the grid', async ({ page, server }) => {
  await page.setViewportSize(phone)
  await openMoneyflow(page, server)
  await page.getByRole('button', { name: 'Open navigation' }).click()
  const drawer = page.getByRole('dialog', { name: 'Navigation' })
  await expect(drawer).toBeVisible()

  await page.setViewportSize({ width: 1024, height: 768 })

  await expect(drawer).toBeHidden()
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
})

test('the editor keeps its draft when the window crosses the phone width', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await page.setViewportSize({ width: 1024, height: 768 })
    // September 2026: Maple Court Apartments has one rent payment in view and 33 outside it.
    await openMoneyflow(page, server, 'cur=USD%3A2&from=2026-09-01&to=2026-09-30&v=1')
    await page.keyboard.press('m')
    const editor = page.getByRole('dialog', { name: 'Edit merchant' })
    const name = editor.getByRole('combobox', { name: 'Merchant' })
    const whole = editor.getByRole('radio', { name: 'Whole merchant · 34 transactions' })
    await name.fill('Maple Court Homes')
    await name.press('Enter')
    await whole.click()
    await expect(whole).toBeChecked()

    await page.setViewportSize(phone)
    const sheet = page.locator('.mf-sheet__panel')
    await expect(sheet).toBeVisible()
    await expect(sheet.getByRole('combobox', { name: 'Merchant' })).toHaveValue('Maple Court Homes')
    await expect(sheet.getByRole('combobox', { name: 'Merchant' })).toBeFocused()
    await expect(sheet.getByRole('radio', { name: /^Whole merchant/ })).toBeChecked()

    await page.setViewportSize({ width: 1024, height: 768 })
    await expect(page.locator('.mf-anchored-editor')).toBeVisible()
    await expect(name).toHaveValue('Maple Court Homes')
    await expect(name).toBeFocused()
    await expect(whole).toBeChecked()
    await editor.getByRole('button', { name: 'Stage change to 34 transactions' }).click()
    await expect(editor).toBeHidden()
    await expect(page.getByText(/1 pending edit · 34 transactions/)).toBeVisible()
  } finally {
    await server.stop()
  }
})

test.describe('phone rows by touch', () => {
  test.use({ hasTouch: true, viewport: phone })

  test('the tap area selects, a row drills or opens Info, and ⋯ opens the row actions', async ({
    page,
    server,
  }) => {
    await openMoneyflow(page, server)
    await expectCompactRows(page)
    const grid = page.getByRole('grid', { name: 'Financial results' })
    const grocery = grid.getByRole('row').filter({ hasText: 'Corner Grocery' })
    await cell(grocery, 'select').tap()
    await expect(grocery).toHaveAttribute('aria-selected', 'true')
    await cell(grocery, 'select').tap()
    await expect(grocery).toHaveAttribute('aria-selected', 'false')
    await cell(grocery, 'net').tap()
    await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toContainText(
      'Corner Grocery',
    )
    const first = grid.locator('[role="row"][aria-rowindex="2"]')
    await expect(cell(first, 'merchant')).toHaveText('Corner Grocery')
    await expectCompactRows(page)
    await cell(first, 'category').tap()
    const information = page.getByRole('dialog', { name: 'Transaction information' })
    await expect(information).toContainText('Corner Grocery')
    await page.keyboard.press('Escape')
    await expect(information).toBeHidden()

    const more = first.getByRole('button', { name: 'Actions for Corner Grocery' })
    await more.tap()
    const sheet = page.getByRole('dialog', { name: 'Corner Grocery' })
    await expect(sheet.getByRole('button')).toHaveText([
      '',
      'Merchant',
      'Category',
      'Hide',
      'Delete',
      'Info',
    ])
    await expectTouchTargets(sheet)
    await page.keyboard.press('Escape')
    await expect(sheet).toBeHidden()
    await expect(more).toBeFocused()
    // Esc closed only the sheet; it did not also go back a level.
    await expect(cell(first, 'merchant')).toHaveText('Corner Grocery')

    for (const [item, role, editor] of [
      ['Merchant', 'dialog', 'Edit merchant'],
      ['Category', 'dialog', 'Change category'],
      ['Delete', 'alertdialog', 'Confirm deletion'],
    ] as const) {
      await more.tap()
      await sheet.getByRole('button', { name: item }).tap()
      const dialog = page.getByRole(role, { name: editor, exact: true })
      await expect(dialog).toBeVisible()
      await expect(sheet).toBeHidden()
      await page.keyboard.press('Escape')
      await expect(dialog).toBeHidden()
      await expect(more).toBeFocused()
    }

    await more.tap()
    await sheet.getByRole('button', { name: 'Hide' }).tap()
    await expect(sheet).toBeHidden()
    await expect(first).toContainText('Hidden')
    await expect(more).toBeFocused()
    // At phone width the notice has the status line until the next action brings back the
    // pending text.
    const notice = page.getByRole('status', { name: 'Notice' })
    await expect(notice).toHaveText('Hidden 1 transaction · Undo u')
    await page.keyboard.press('j')
    await expect(notice).toBeEmpty()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
  })
})

test.describe('phone rows: drilling, keys, and the sheet', () => {
  test.use({ hasTouch: true, viewport: phone })

  test('Back after a tap drill returns the cursor to the tapped group', async ({
    page,
    server,
  }) => {
    await openMoneyflow(page, server)
    const grid = page.getByRole('grid', { name: 'Financial results' })
    const third = grid.locator('[role="row"][aria-rowindex="4"]')
    const name = await cell(third, 'name').innerText()
    const id = (await third.getAttribute('id'))!
    await cell(third, 'net').tap()
    await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toContainText(name)
    await page.goBack()
    await expect(grid).toHaveAttribute('aria-activedescendant', id)
  })

  test('Enter drills a group and opens Info on a transaction', async ({ page, server }) => {
    await openMoneyflow(page, server)
    const grid = page.getByRole('grid', { name: 'Financial results' })
    const name = await cell(await activeRow(page), 'name').innerText()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toContainText(name)
    await expect(cell(grid.locator('[role="row"][aria-rowindex="2"]'), 'merchant')).toBeVisible()
    await page.keyboard.press('Enter')
    const information = page.getByRole('dialog', { name: 'Transaction information' })
    await expect(information).toContainText(name)
  })

  test('the ⋯ sheet closes when the window grows past the phone width', async ({
    page,
    server,
  }) => {
    await openMoneyflow(page, server)
    await page.keyboard.press('d')
    const grid = page.getByRole('grid', { name: 'Financial results' })
    const first = grid.locator('[role="row"][aria-rowindex="2"]')
    const merchant = await cell(first, 'merchant').innerText()
    await cell(first, 'actions').getByRole('button').tap()
    const sheet = page.getByRole('dialog', { name: merchant })
    await expect(sheet).toBeVisible()
    await page.setViewportSize({ width: 1024, height: 768 })
    await expect(sheet).toBeHidden()
    await expect(grid).toBeFocused()
  })

  test('an Amazon match mark is a 44 px target out of the Tab order', async ({ page, server }) => {
    // The demo household has no Amazon matches, so every transaction row gets one here.
    const withMatches = async (route: Route) => {
      const response = await route.fetch()
      const body = (await response.json()) as { detail_rows?: Record<string, unknown>[] | null }
      for (const row of body.detail_rows ?? []) {
        row['amazon_match'] = {
          class: 'exact_order',
          confidence: 'high',
          first_product: 'Example Product',
          total_matches: 1,
        }
      }
      await route.fulfill({ response, json: body })
    }
    await page.route('**/api/v1/profiles/*/view', withMatches)
    await page.route('**/api/v1/profiles/*/view/transition', withMatches)
    await openMoneyflow(page, server)
    await page.keyboard.press('d')
    const grid = page.getByRole('grid', { name: 'Financial results' })
    const mark = grid.getByRole('button', { name: 'Amazon match: Example Product, high' }).first()
    await expect(mark).toBeVisible()
    await expect(mark).toHaveAttribute('tabindex', '-1')
    await expectCompactRows(page)
    // The whole 44 px target sits inside its row: a tap just above the row's bottom edge hits
    // the mark, and a tap just below it lands in the next row.
    const edge = await mark.evaluate((button) => {
      const own = button.closest('[role="row"]')!
      const box = button.getBoundingClientRect()
      const row = own.getBoundingClientRect()
      const x = box.left + box.width / 2
      const rowAt = (y: number) => document.elementFromPoint(x, y)?.closest('[role="row"]')
      return {
        height: box.height,
        inside: box.top >= row.top && box.bottom <= row.bottom,
        // Two pixels in: a fallback font can move the mark by half a pixel.
        above: document.elementFromPoint(x, row.bottom - 2)?.closest('button') === button,
        below: rowAt(row.bottom + 1) === own.nextElementSibling,
      }
    })
    expect(edge).toEqual({ height: 44, inside: true, above: true, below: true })
    await mark.tap()
    await expect(page.getByRole('dialog', { name: 'Transaction information' })).toBeVisible()
  })

  test('⋯ edits the merchant in a bottom sheet with the editor keys', async ({ page, server }) => {
    await openMoneyflow(page, server)
    await page.keyboard.press('d')
    const grid = page.getByRole('grid', { name: 'Financial results' })
    const second = grid.locator('[role="row"][aria-rowindex="3"]')
    const merchant = await cell(second, 'merchant').innerText()
    const more = cell(second, 'actions').getByRole('button')
    const actions = page.getByRole('dialog', { name: merchant })
    const editor = page.getByRole('dialog', { name: 'Edit merchant', exact: true })
    const name = editor.getByRole('combobox', { name: 'Merchant' })

    await more.tap()
    await actions.getByRole('button', { name: 'Merchant' }).tap()
    await expect(name).toBeFocused()
    await expect(actions).toBeHidden()
    await expect(editor).toHaveAccessibleDescription('· 1 transaction')
    // A bottom sheet across the phone's width, on its bottom edge.
    const box = (await editor.boundingBox())!
    expect([box.x, box.width, box.y + box.height].map(Math.round)).toEqual([
      0,
      phone.width,
      phone.height,
    ])
    await expectTouchTargets(editor)
    await editor.getByRole('button', { name: 'Cancel' }).tap()
    await expect(editor).toBeHidden()
    await expect(more).toBeFocused()

    await more.tap()
    await actions.getByRole('button', { name: 'Merchant' }).tap()
    // A long suggestion list: 44 px options that scroll, with Stage and Cancel still in reach.
    await name.fill('a')
    const list = editor.getByRole('listbox', { name: 'Merchant' })
    await expect(list.getByRole('option').nth(5)).toBeAttached()
    await expectTouchTargets(editor)
    expect(await list.evaluate((element) => element.scrollHeight > element.clientHeight)).toBe(true)
    for (const label of [/^Stage change/, /^Cancel$/]) {
      const button = editor.getByRole('button', { name: label })
      await expect(button).toBeInViewport({ ratio: 1 })
      const reachable = await button.evaluate((element) => {
        const box = element.getBoundingClientRect()
        const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2)
        return element.contains(hit)
      })
      expect(reachable).toBe(true)
    }
    await name.fill('Renamed Example')
    await name.press('Enter')
    await expect(
      editor.getByRole('button', { name: 'Stage change to 1 transaction' }),
    ).toBeEnabled()
    await name.press('Enter')
    await expect(editor).toBeHidden()
    await expect(cell(second, 'merchant')).toHaveText(`${merchant} → Renamed Example`)
    await expect(grid).toBeFocused()
    await expect(await activeRow(page)).toHaveAttribute('aria-rowindex', '3')
    // The server outlives this test; undo leaves the row as the later tests expect it.
    await page.keyboard.press('u')
    await expect(cell(second, 'merchant')).toHaveText(merchant)
  })

  test('⋯ confirms a delete in a bottom sheet', async ({ page, server }) => {
    await openMoneyflow(page, server)
    await page.keyboard.press('d')
    const grid = page.getByRole('grid', { name: 'Financial results' })
    const second = grid.locator('[role="row"][aria-rowindex="3"]')
    const merchant = await cell(second, 'merchant').innerText()
    const more = cell(second, 'actions').getByRole('button')
    const actions = page.getByRole('dialog', { name: merchant })
    const confirmation = page.getByRole('alertdialog', { name: 'Confirm deletion' })

    await more.tap()
    await actions.getByRole('button', { name: 'Delete' }).tap()
    await expect(actions).toBeHidden()
    await expect(confirmation).toHaveAccessibleDescription('Delete 1 transaction?')
    expect(await focusInside(confirmation)).toBe(true)
    // A bottom sheet across the phone's width, on its bottom edge.
    const box = (await confirmation.boundingBox())!
    expect([box.x, box.width, box.y + box.height].map(Math.round)).toEqual([
      0,
      phone.width,
      phone.height,
    ])
    await expectTouchTargets(confirmation)
    await confirmation.getByRole('button', { name: 'Cancel' }).tap()
    await expect(confirmation).toBeHidden()
    await expect(more).toBeFocused()
    await expect(second).not.toHaveClass(/mf-table__row--deleted/)

    await more.tap()
    await actions.getByRole('button', { name: 'Delete' }).tap()
    await confirmation.getByRole('button', { name: 'Delete' }).tap()
    await expect(confirmation).toBeHidden()
    await expect(page.getByRole('status').filter({ hasText: /^Deleted / })).toHaveText(
      'Deleted 1 transaction · Undo u',
    )
    await expect(second).toHaveClass(/mf-table__row--deleted/)
    await expect(more).toBeFocused()
    // The server outlives this test; undo leaves the row as the later tests expect it.
    await page.keyboard.press('u')
    await expect(second).not.toHaveClass(/mf-table__row--deleted/)
  })
})

test.describe('phone row actions with a selection', () => {
  test.use({ hasTouch: true, viewport: phone })

  test('the ⋯ sheet leaves edits to the selection bar and still opens Info', async ({
    page,
    server,
  }) => {
    await openMoneyflow(page, server)
    await page.keyboard.press('d')
    const grid = page.getByRole('grid', { name: 'Financial results' })
    const selected = grid.locator('[role="row"][aria-rowindex="2"]')
    const other = grid.locator('[role="row"][aria-rowindex="3"]')
    await cell(selected, 'select').tap()
    await expect(selected).toHaveAttribute('aria-selected', 'true')
    const merchant = await cell(other, 'merchant').innerText()
    const more = cell(other, 'actions').getByRole('button')
    await more.tap()
    const sheet = page.getByRole('dialog', { name: merchant })
    for (const item of ['Merchant', 'Category', 'Hide', 'Delete']) {
      await expect(sheet.getByRole('button', { name: item })).toBeDisabled()
      await expect(sheet.getByRole('button', { name: item })).toHaveAccessibleDescription(
        'Clear the selection to edit one transaction',
      )
    }
    await sheet.getByRole('button', { name: 'Info' }).tap()
    const information = page.getByRole('dialog', { name: 'Transaction information' })
    await expect(information).toContainText(merchant)
    await page.keyboard.press('Escape')
    await expect(information).toBeHidden()
    await expect(more).toBeFocused()
    await expect(selected).toHaveAttribute('aria-selected', 'true')
    await expect(other).toHaveAttribute('aria-selected', 'false')
  })
})

/** The views whose columns differ: grouped with Context, Trend, and vs prior, Time, and detail. */
const FIT_VIEWS = [
  { view: 'merchants', query: 'cur=USD%3A2&from=2026-09-01&to=2026-09-30&v=1', name: 'name' },
  { view: 'categories', query: 'cur=USD%3A2&group=category&v=1', name: 'name' },
  { view: 'time', query: 'cur=USD%3A2&group=time&v=1', name: 'period' },
  { view: 'transactions', query: 'cur=USD%3A2&mode=detail&v=1', name: 'merchant' },
] as const

test('the table drops columns instead of scrolling sideways, at every width', async ({
  page,
  server,
}) => {
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const workspace = page.locator('.mf-workspace')
  const headerKeys = () =>
    grid
      .locator('.mf-table__header [data-column]')
      .evaluateAll((cells) => cells.map((node) => (node as HTMLElement).dataset['column']))
  for (const { view, query, name } of FIT_VIEWS) {
    await page.setViewportSize({ width: 1280, height: 800 })
    await openMoneyflow(page, server, query)
    let fewest = 0
    for (const width of [641, 700, 759, 760, 820, 900, 1010, 1280]) {
      await page.setViewportSize({ width, height: 800 })
      const context = `${view} at ${width} px`
      await expect
        .poll(() => workspace.evaluate((node) => node.scrollWidth - node.clientWidth), {
          message: context,
        })
        .toBeLessThanOrEqual(0)
      const keys = await headerKeys()
      expect(keys, context).toContain(name)
      const shown = (await workspace.boundingBox())!
      const last = (await grid.locator('.mf-table__header [data-column]').last().boundingBox())!
      expect(last.x + last.width, context).toBeLessThanOrEqual(shown.x + shown.width)
      // A wider window never shows fewer columns than a narrower one.
      expect(keys.length, context).toBeGreaterThanOrEqual(fewest)
      fewest = keys.length
    }
  }
})

test('641–899 px: the selection bar and header stay inside the workspace', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 820, height: 800 })
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const workspace = page.locator('.mf-workspace')
  const header = (key: string) => grid.locator(`.mf-table__header [data-column="${key}"]`)
  const shown = (await workspace.boundingBox())!

  await page.keyboard.press('Space')
  const bar = page.getByRole('region', { name: 'Selection' })
  await expect(bar).toBeVisible()
  const pinned = (await bar.boundingBox())!
  expect(pinned.x).toBeGreaterThanOrEqual(shown.x - 1)
  expect(pinned.x + pinned.width).toBeLessThanOrEqual(shown.x + shown.width + 1)
  await page.keyboard.press('Escape')

  // The header stays on top of the rows while the table scrolls down.
  const scrollport = (await workspace.boundingBox())!
  await workspace.evaluate((node) => (node.scrollTop = node.scrollHeight))
  const headerRow = grid.locator('.mf-table__header')
  await expect.poll(async () => (await headerRow.boundingBox())!.y).toBe(scrollport.y)
  const covering = await header('name').evaluate((node) => {
    const box = node.getBoundingClientRect()
    return document.elementFromPoint(box.x + 4, box.y + box.height / 2)?.closest('[role]')?.role
  })
  expect(covering).toBe('columnheader')
})
