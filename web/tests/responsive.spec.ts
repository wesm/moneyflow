import type { Locator, Page } from '@playwright/test'

import { activeRow, cell, expect, openMoneyflow, test } from './fixtures'

test('desktop and medium layouts preserve table primacy and chart linkage', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const visualizations = page.getByRole('complementary', { name: 'Visualizations' })
  await expect(grid).toBeVisible()
  await expect(visualizations).toBeVisible()
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

  const gridBox = await grid.boundingBox()
  const chartBox = await visualizations.boundingBox()
  expect(gridBox).not.toBeNull()
  expect(chartBox).not.toBeNull()
  expect(chartBox!.x).toBeGreaterThan(gridBox!.x)

  const marks = visualizations.getByRole('button')
  const secondMark = marks.nth(1)
  const label = (await secondMark.getAttribute('aria-label'))!.split(',')[0]
  await secondMark.click()
  await expect(secondMark).toHaveAttribute('aria-pressed', 'true')
  await expect(await activeRow(page)).toContainText(label)
  await secondMark.dblclick()
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toContainText(label)

  await page.setViewportSize({ width: 1024, height: 768 })
  await expect(grid).toBeVisible()
  await expect(visualizations).toHaveCount(0)
  const charts = page.getByRole('switch', { name: 'Charts' })
  await expect(charts).not.toBeChecked()
  const lastHeader = (await grid.getByRole('columnheader').last().boundingBox())!
  const mediumGrid = (await grid.boundingBox())!
  expect(lastHeader.x).toBeGreaterThanOrEqual(mediumGrid.x)
  expect(lastHeader.x + lastHeader.width).toBeLessThanOrEqual(mediumGrid.x + mediumGrid.width + 1)

  await charts.check()
  await expect(visualizations).toBeVisible()
  const mediumChart = await visualizations.boundingBox()
  expect(mediumChart!.x).toBeGreaterThan((await grid.boundingBox())!.x)

  await page.setViewportSize({ width: 768, height: 700 })
  const boundedChart = await visualizations.boundingBox()
  expect(boundedChart).not.toBeNull()
  expect(boundedChart!.x + boundedChart!.width).toBeLessThanOrEqual(768)
  expect(boundedChart!.y + boundedChart!.height).toBeLessThanOrEqual(700)
  await expect(grid).toBeVisible()
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
  await expect(page.getByRole('complementary', { name: 'Visualizations' })).toHaveCount(0)
  const rows = page.getByRole('grid', { name: 'Financial results' }).getByRole('row')
  expect(await rows.count()).toBeGreaterThan(1)

  await expectCompactRows(page)

  const charts = page.getByRole('switch', { name: 'Charts' })
  await expect(charts).not.toBeChecked()
  await charts.check()
  await expect(page.getByRole('dialog', { name: 'Moneyflow visualizations' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Visualizations' })).toBeVisible()
  await expect(page).toHaveURL(analyticalURL)
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
  await expect(page.getByText(/0 pending/)).toHaveCount(1)
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

  const charts = page.getByRole('switch', { name: 'Charts' })
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
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await expect(more).toBeFocused()
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

test('641–899 px: Context drops below 760 px and the table scrolls under sticky ☐ and Name', async ({
  page,
  server,
}) => {
  await page.setViewportSize({ width: 700, height: 800 })
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
  const after = await boxes()
  expect(after).toMatchObject({ select: before.select, name: before.name, header: before.header })

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
