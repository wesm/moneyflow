import { activeRow, expect, openMoneyflow, test } from './fixtures'

const breadcrumb = (page: import('@playwright/test').Page) =>
  page.getByRole('navigation', { name: 'Breadcrumb' })

test('@smoke browser history, Esc, refresh, and selection retain server state', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const initialLength = await page.evaluate(() => history.length)
  const merchant = await (await activeRow(page)).getByRole('gridcell').first().innerText()

  await page.keyboard.press('Space')
  await expect(await activeRow(page)).toHaveAttribute('aria-selected', 'true')
  expect(await page.evaluate(() => history.length)).toBe(initialLength)

  await page.keyboard.press('Enter')
  await expect(breadcrumb(page)).toContainText(merchant)
  const detailURL = page.url()
  expect(await page.evaluate(() => history.length)).toBe(initialLength + 1)

  await page.goBack()
  await expect(breadcrumb(page)).toHaveText('Merchants')
  await page.goForward()
  await expect(page).toHaveURL(detailURL)
  await expect(breadcrumb(page)).toContainText(merchant)

  await page.keyboard.press('Escape')
  await expect(breadcrumb(page)).toHaveText('Merchants')
  await expect(page).not.toHaveURL(detailURL)

  await page.keyboard.press('/')
  await page.getByRole('searchbox', { name: 'Search transactions' }).fill('noodle')
  await page.getByRole('searchbox', { name: 'Search transactions' }).press('Enter')
  await expect(page).toHaveURL(/[?&]q=noodle(?:&|$)/)
  const searchURL = page.url()
  await page.reload()
  await expect(page).toHaveURL(searchURL)
  await expect(await activeRow(page)).toContainText('Noodle House')
  await expect(page.getByRole('searchbox', { name: 'Search transactions' })).toHaveValue('noodle')
})

test('Esc after a selection returns to the same entry, and Back and Forward land on views', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const initialLength = await page.evaluate(() => history.length)
  const topURL = page.url()
  const merchant = await (await activeRow(page)).getByRole('gridcell').first().innerText()
  const selectedRows = page.locator('[role="row"][aria-selected="true"]')

  await page.keyboard.press('Space')
  await expect(selectedRows).toHaveCount(1)
  await page.keyboard.press('Enter')
  await expect(breadcrumb(page)).toContainText(merchant)
  const drillURL = page.url()
  await page.keyboard.press('Escape')
  await expect(breadcrumb(page)).toHaveText('Merchants')
  await expect(page).toHaveURL(topURL)
  await expect(selectedRows).toHaveCount(0)
  expect(await page.evaluate(() => history.length)).toBe(initialLength + 1)

  await page.goForward()
  await expect(page).toHaveURL(drillURL)
  await expect(breadcrumb(page)).toContainText(merchant)
  await page.goBack()
  await expect(page).toHaveURL(topURL)
  await expect(breadcrumb(page)).toHaveText('Merchants')
  await expect(selectedRows).toHaveCount(0)
})

test('a breadcrumb click returns to its level and keeps one history entry per view', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const initialLength = await page.evaluate(() => history.length)
  const topURL = page.url()
  const merchant = await (await activeRow(page)).getByRole('gridcell').first().innerText()
  await page.keyboard.press('Enter')
  await expect(breadcrumb(page)).toContainText(merchant)
  const drillURL = page.url()
  await page.keyboard.press('g')
  await expect(breadcrumb(page)).toContainText('by Category')
  const category = await (await activeRow(page)).getByRole('gridcell').first().innerText()
  await page.keyboard.press('Enter')
  await expect(breadcrumb(page).getByRole('listitem')).toHaveText([
    'Merchants',
    `› ${merchant}`,
    `› ${category}`,
  ])
  expect(await page.evaluate(() => history.length)).toBe(initialLength + 3)

  await breadcrumb(page).getByRole('button', { name: merchant }).click()
  await expect(breadcrumb(page).getByRole('listitem')).toHaveText(['Merchants', `› ${merchant}`])
  await expect(page).toHaveURL(drillURL)
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  expect(await page.evaluate(() => history.length)).toBe(initialLength + 3)

  await page.keyboard.press('Escape')
  await expect(breadcrumb(page)).toHaveText('Merchants')
  await expect(page).toHaveURL(topURL)
  await page.goForward()
  await expect(page).toHaveURL(drillURL)
  await expect(breadcrumb(page).getByRole('listitem')).toHaveText(['Merchants', `› ${merchant}`])

  await breadcrumb(page).getByRole('button', { name: 'Merchants' }).click()
  await expect(breadcrumb(page)).toHaveText('Merchants')
  await expect(page).toHaveURL(topURL)
  expect(await page.evaluate(() => history.length)).toBe(initialLength + 3)
})

test('direct invalid bookmarks fail safely and can reset', async ({ page, server }) => {
  await page.goto(`${server.url}?missing=1`)
  await expect(
    page.getByRole('heading', { name: 'This Moneyflow link cannot be opened' }),
  ).toBeVisible()
  await page.getByRole('button', { name: 'Reset view' }).click()
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  await expect(page).toHaveURL(`${server.url}?cur=USD%3A2&v=1`)
})

test('Esc falls back to a Go-derived parent when owned history is unavailable', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const merchant = await (await activeRow(page)).getByRole('gridcell').first().innerText()
  await page.keyboard.press('Enter')
  await expect(breadcrumb(page)).toContainText(merchant)
  await page.evaluate(() => history.replaceState(null, '', location.href))
  await page.keyboard.press('Escape')
  await expect(breadcrumb(page)).toHaveText('Merchants')
})
