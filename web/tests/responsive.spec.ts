import { activeRow, expect, openMoneyflow, test } from './fixtures'

test('desktop and medium layouts preserve table primacy and chart linkage', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const visualizations = page.getByRole('complementary', { name: 'Visualizations' })
  await expect(grid).toBeVisible()
  await expect(visualizations).toBeVisible()
  await expect(grid.getByRole('columnheader')).toHaveText(['merchant', 'Count', 'In', 'Out', 'Net'])
  const amountHeaders = await Promise.all(
    ['In', 'Out', 'Net'].map((name) =>
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
  await expect(page.getByRole('navigation', { name: 'Active refinements' })).toContainText(label)

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
  await expect(page.getByRole('navigation', { name: 'Active refinements' })).toContainText(
    'Group: category',
  )
  const analyticalURL = page.url()

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page).toHaveURL(analyticalURL)
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeVisible()
  await expect(page.getByRole('complementary', { name: 'Visualizations' })).toHaveCount(0)
  expect(await page.locator('.finance-grid [role="row"]').count()).toBeGreaterThan(1)

  const grid = page.getByRole('grid', { name: 'Financial results' })
  const net = grid.getByRole('columnheader', { name: 'Net', exact: true })
  await net.scrollIntoViewIfNeeded()
  const netBox = (await net.boundingBox())!
  const gridBox = (await grid.boundingBox())!
  expect(netBox.x).toBeGreaterThanOrEqual(gridBox.x)
  expect(netBox.x + netBox.width).toBeLessThanOrEqual(gridBox.x + gridBox.width + 1)
  expect(await page.locator('body').evaluate((body) => body.scrollWidth <= body.clientWidth)).toBe(
    true,
  )

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
  await expect(grid.getByRole('columnheader').first()).toHaveText('category')
  await expect(categories).toHaveAttribute('aria-current', 'page')
  await expect(grid).toBeFocused()

  await nav.getByRole('button', { name: 'Groups', exact: true }).focus()
  await page.keyboard.press('Enter')
  await expect(grid.getByRole('columnheader').first()).toHaveText('group')

  await page.setViewportSize({ width: 1024, height: 768 })
  await expect(page.getByRole('separator', { name: 'Resize sidebar' })).toHaveCount(0)
  await nav.getByRole('button', { name: 'Merchants' }).hover()
  await expect(page.getByRole('tooltip')).toHaveText('Merchants · 1')
})
