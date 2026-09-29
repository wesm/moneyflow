import { activeRow, expect, openMoneyflow, test } from './fixtures'

const refinements = (page: import('@playwright/test').Page) =>
  page.getByRole('navigation', { name: 'Active refinements' })

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
  await expect(refinements(page)).toContainText(`M: ${merchant}`)
  const detailURL = page.url()
  expect(await page.evaluate(() => history.length)).toBe(initialLength + 1)

  await page.goBack()
  await expect(refinements(page)).toContainText('Merchants')
  await page.goForward()
  await expect(page).toHaveURL(detailURL)
  await expect(refinements(page)).toContainText(`M: ${merchant}`)

  await page.keyboard.press('Escape')
  await expect(refinements(page)).toContainText('Merchants')
  await expect(page).not.toHaveURL(detailURL)

  await page.keyboard.press('/')
  await page.getByRole('searchbox', { name: 'Search transactions' }).fill('091')
  await page.getByRole('searchbox', { name: 'Search transactions' }).press('Enter')
  await expect(refinements(page)).toContainText('Search: 091')
  const searchURL = page.url()
  await page.reload()
  await expect(page).toHaveURL(searchURL)
  await expect(refinements(page)).toContainText('Search: 091')
})

test('direct invalid bookmarks fail safely and can reset', async ({ page, server }) => {
  await page.goto(`${server.url}?missing=1`)
  await expect(
    page.getByRole('heading', { name: 'This Moneyflow link cannot be opened' }),
  ).toBeVisible()
  await page.getByRole('button', { name: 'Reset view' }).click()
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  await expect(page).toHaveURL(`${server.url}?v=1`)
})

test('Esc falls back to a Go-derived parent when owned history is unavailable', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const merchant = await (await activeRow(page)).getByRole('gridcell').first().innerText()
  await page.keyboard.press('Enter')
  await expect(refinements(page)).toContainText(`M: ${merchant}`)
  await page.evaluate(() => history.replaceState(null, '', location.href))
  await page.keyboard.press('Escape')
  await expect(refinements(page)).toContainText('Merchants')
})
