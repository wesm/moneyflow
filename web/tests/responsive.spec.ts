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

  const charts = page.getByRole('switch', { name: 'Charts' })
  await expect(charts).not.toBeChecked()
  await charts.check()
  await expect(page.getByRole('dialog', { name: 'Moneyflow visualizations' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Visualizations' })).toBeVisible()
  await expect(page).toHaveURL(analyticalURL)
})
