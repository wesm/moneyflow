import type { Locator, Page, Route } from '@playwright/test'

import { activeRow, cell, expect, openMoneyflow, test } from './fixtures'

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

test('Trend and vs prior fill grouped rows, and from 900 px columns drop instead of scrolling', async ({
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
  for (const width of [1440, 1280, 1024, 960, 900]) {
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
    if (width >= 1280) expect(keys).toContain('context')
    if (width === 1024) {
      expect(keys).not.toContain('context')
      expect(keys.slice(-2)).toEqual(['trend', 'vs_prior'])
    }
    if (width === 900) expect(keys).not.toContain('trend')
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

test('the sidebar groups by its entries and the rail names them in tooltips', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const nav = page.getByRole('navigation', { name: 'Workspace' })
  const categories = nav.getByRole('button', { name: 'Categories', exact: true })
  await categories.click()
  await expect(grid.locator('[role="columnheader"][data-column="name"]')).toHaveText('Category')
  await expect(categories).toHaveAttribute('aria-current', 'page')
  await expect(grid).toBeFocused()

  await nav.getByRole('button', { name: 'Groups', exact: true }).focus()
  await page.keyboard.press('Enter')
  await expect(grid.locator('[role="columnheader"][data-column="name"]')).toHaveText('Group')

  await page.setViewportSize({ width: 1024, height: 768 })
  await expect(page.getByRole('separator', { name: 'Resize sidebar' })).toHaveCount(0)
  await nav.getByRole('button', { name: 'Merchants' }).hover()
  await expect(page.getByRole('tooltip')).toHaveText('Merchants · 1')
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
 * The page cannot scroll sideways and every visible command bar control ends inside the
 * viewport. The shell hides overflow, so a scroll width alone would not show a clipped control.
 */
async function expectNoHorizontalScroll(page: Page): Promise<void> {
  const overflow = await page.evaluate(() => {
    const root = document.documentElement
    const selector = 'button, input, [role="radio"], [role="combobox"], label.kit-toggle'
    const controls = Array.from(
      document.querySelectorAll<HTMLElement>(`[role="banner"] :is(${selector})`),
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
    const selector = 'button, [role="radio"], [role="combobox"], label.kit-toggle, .kit-text-input'
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

test('the phone drawer holds the sidebar, traps focus, and closes on Esc', async ({
  page,
  server,
}) => {
  await page.setViewportSize(phone)
  await openMoneyflow(page, server)
  await expectNoHorizontalScroll(page)
  const url = page.url()
  const menu = page.getByRole('button', { name: 'Open navigation' })
  await menu.click()
  const drawer = page.getByRole('dialog', { name: 'Navigation' })
  await expect(drawer.getByRole('navigation', { name: 'Workspace' })).toBeVisible()
  await expect(page.getByText(/0 pending edit/)).toHaveCount(1)
  await expectTouchTargets(drawer)
  for (let press = 0; press < 20; press += 1) {
    await page.keyboard.press('Tab')
    expect(await focusInside(drawer)).toBe(true)
  }
  await page.keyboard.press('Shift+Tab')
  expect(await focusInside(drawer)).toBe(true)
  await expectNoHorizontalScroll(page)

  await page.keyboard.press('Escape')
  await expect(drawer).toBeHidden()
  await expect(menu).toBeFocused()
  await expect(page).toHaveURL(url)

  await menu.click()
  await expect(drawer.getByRole('button').first()).toHaveAccessibleName('Close navigation')
  await drawer.getByRole('button', { name: 'Close navigation' }).click()
  await expect(drawer).toBeHidden()
  await expect(menu).toBeFocused()

  await menu.click()
  await page.mouse.click(phone.width - 10, phone.height / 2)
  await expect(drawer).toBeHidden()

  await menu.click()
  await drawer.getByRole('button', { name: 'Categories', exact: true }).click()
  await expect(drawer).toBeHidden()
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toHaveText('Categories')
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
})

test('the View sheet changes the period and filters at phone width', async ({ page, server }) => {
  await page.setViewportSize(phone)
  await openMoneyflow(page, server)
  const banner = page.getByRole('banner')
  await expect(banner.getByRole('radiogroup', { name: 'Period', exact: true })).toHaveCount(0)
  await expectTouchTargets(banner)
  const view = banner.getByRole('button', { name: 'View', exact: true })
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
  await expect(banner.locator('button[aria-haspopup="dialog"][aria-keyshortcuts="t"]')).toHaveText(
    'Oct 2026',
  )
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
  await expectTouchTargets(page.getByRole('banner'))
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

test('just above the phone breakpoint the rail frame keeps the controls inline', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 700, height: 800 })
  await openMoneyflow(page, server)
  await expect(page.getByRole('button', { name: 'Open navigation' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'View', exact: true })).toHaveCount(0)
  await expect(page.getByRole('navigation', { name: 'Workspace' })).toBeVisible()
  await expect(page.getByRole('radiogroup', { name: 'Period', exact: true })).toBeVisible()
  await expect(page.getByRole('searchbox', { name: 'Search transactions' })).toBeVisible()
  await expectNoHorizontalScroll(page)
})

test('a cross-year period label shrinks to fit the phone command bar', async ({ page, server }) => {
  await page.setViewportSize(phone)
  await openMoneyflow(page, server, 'cur=USD%3A2&from=2025-11-15&to=2026-02-14&v=1')
  const label = page.getByRole('banner').locator('button[aria-keyshortcuts="t"]')
  await expect(label).toHaveAccessibleName('Nov 15, 2025 – Feb 14, 2026')
  await expect(label).toHaveAttribute('title', /^Nov 15, 2025 – Feb 14, 2026/)
  await expectNoHorizontalScroll(page)
})

test('growing past the phone width closes the drawer and focuses the grid', async ({
  page,
  server,
}) => {
  await page.setViewportSize(phone)
  await openMoneyflow(page, server)
  await page.getByRole('button', { name: 'Open navigation' }).click()
  const drawer = page.getByRole('dialog', { name: 'Navigation' })
  await expect(drawer).toBeVisible()

  await page.setViewportSize({ width: 1024, height: 768 })

  await expect(drawer).toBeHidden()
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
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

    for (const [item, editor] of [
      ['Merchant', 'Edit merchant'],
      ['Category', 'Change category'],
      ['Delete', 'Confirm deletion'],
    ]) {
      await more.tap()
      await sheet.getByRole('button', { name: item }).tap()
      const dialog = page.getByRole('dialog', { name: editor, exact: true })
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
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await expect(more).toBeFocused()
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
    await mark.tap()
    await expect(page.getByRole('dialog', { name: 'Transaction information' })).toBeVisible()
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

test('641–899 px: Net fits, and a wide table scrolls under sticky ☐, Name, and the bar', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 641, height: 800 })
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const workspace = page.locator('.mf-workspace')
  const header = (key: string) => grid.locator(`.mf-table__header [data-column="${key}"]`)
  await expect(header('context')).toHaveCount(0)
  const shown = (await workspace.boundingBox())!
  const net = (await header('net').boundingBox())!
  expect(net.x + net.width).toBeLessThanOrEqual(shown.x + shown.width)

  await page.setViewportSize({ width: 820, height: 800 })
  await expect(header('context')).toHaveText('Top category')
  const row = grid.locator('[role="row"][aria-rowindex="2"]')
  const boxes = async () => ({
    select: (await cell(row, 'select').boundingBox())!.x,
    name: (await cell(row, 'name').boundingBox())!.x,
    header: (await header('name').boundingBox())!.x,
    context: (await cell(row, 'context').boundingBox())!.x,
  })
  const before = await boxes()
  const overflow = await workspace.evaluate((node) => node.scrollWidth - node.clientWidth)
  expect(overflow).toBeGreaterThan(0)
  await workspace.evaluate((node) => (node.scrollLeft = node.scrollWidth))
  await expect.poll(async () => (await boxes()).context).toBeLessThan(before.context - overflow + 1)
  // ☐ sticks at the scroller's edge, over its focus-ring padding, and Name sticks beside it.
  const after = await boxes()
  expect(after.select).toBe((await workspace.boundingBox())!.x)
  expect(after.name - after.select).toBe(before.name - before.select)
  expect(after.header).toBe(after.name)

  // The selection bar stays put too, inside the workspace.
  await page.keyboard.press('Space')
  const bar = page.getByRole('region', { name: 'Selection' })
  await expect(bar).toBeVisible()
  const pinned = (await bar.boundingBox())!
  await workspace.evaluate((node) => (node.scrollLeft = 0))
  await expect.poll(async () => (await bar.boundingBox())!.x).toBe(pinned.x)
  expect(pinned.x).toBeGreaterThanOrEqual(shown.x - 1)
  expect(pinned.x + pinned.width).toBeLessThanOrEqual(
    (await workspace.boundingBox())!.x + (await workspace.boundingBox())!.width + 1,
  )
  await page.keyboard.press('Escape')

  // The header stays on top of the rows while the table scrolls down.
  const scrollport = (await workspace.boundingBox())!
  await workspace.evaluate((node) => (node.scrollTop = 600))
  await expect.poll(async () => (await header('name').boundingBox())!.y).toBe(scrollport.y)
  const covering = await header('name').evaluate((node) => {
    const box = node.getBoundingClientRect()
    return document.elementFromPoint(box.x + 4, box.y + box.height / 2)?.closest('[role]')?.role
  })
  expect(covering).toBe('columnheader')
})
