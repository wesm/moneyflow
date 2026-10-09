import type { Page } from '@playwright/test'

import { expect, openMoneyflow, test } from './fixtures'

const grid = (page: Page) => page.getByRole('grid', { name: 'Financial results' })
const periodLabel = (page: Page) =>
  page.getByRole('banner').locator('button[aria-haspopup="dialog"][aria-keyshortcuts="t"]')
const segment = (page: Page, name: string) =>
  page.getByRole('radiogroup', { name: 'Period', exact: true }).getByRole('radio', { name })
const chooser = (page: Page) => page.getByRole('dialog', { name: 'Choose period' })

async function rowCount(page: Page, merchant: string) {
  const row = page
    .getByRole('row')
    .filter({ has: page.getByRole('gridcell', { name: merchant, exact: true }) })
  return row.getByRole('gridcell').nth(1)
}

test('the Month segment and ‹ move the scope and the grid follows', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await expect(periodLabel(page)).toHaveText('All time')
  await expect(segment(page, 'All')).toBeChecked()

  await segment(page, 'Month').click()
  await expect(periodLabel(page)).toHaveText('Oct 2026')
  await expect(segment(page, 'Month')).toBeChecked()

  await page.getByRole('button', { name: 'Previous period' }).click()
  await expect(periodLabel(page)).toHaveText('Sep 2026')
  await expect(await rowCount(page, 'Maple Court Apartments')).toHaveText('1')

  await grid(page).focus()
  await page.keyboard.press('5')
  await expect(grid(page)).toHaveAttribute('aria-rowcount', '31')
})

test('arrows inside the Period segments change the kind without shifting it', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await segment(page, 'Month').click()
  await expect(periodLabel(page)).toHaveText('Oct 2026')
  await segment(page, 'Month').focus()
  await page.keyboard.press('ArrowLeft')
  await expect(periodLabel(page)).toHaveText('Oct 7, 2026')
  await expect(segment(page, 'Day')).toBeFocused()
  await page.waitForLoadState('networkidle')
  await expect(periodLabel(page)).toHaveText('Oct 7, 2026')
})

test('t chooses a year from the keyboard and Esc returns to the label', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('t')
  await expect(chooser(page)).toBeVisible()
  const kinds = chooser(page).getByRole('radiogroup', { name: 'Period type' })
  await expect(kinds.getByRole('radio', { name: 'Month' })).toBeFocused()
  await page.keyboard.press('ArrowRight')
  await expect(kinds.getByRole('radio', { name: 'Year' })).toBeChecked()
  await page.keyboard.press('Tab')
  await expect(chooser(page).getByRole('button', { name: '2026' })).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await expect(chooser(page).getByRole('button', { name: '2025' })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(chooser(page)).toBeHidden()
  await expect(periodLabel(page)).toHaveText('2025')
  await expect(segment(page, 'Year')).toBeChecked()
  await expect(grid(page)).toBeFocused()

  await periodLabel(page).click()
  await expect(chooser(page)).toBeVisible()
  await page.keyboard.press('j')
  await expect(chooser(page)).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(chooser(page)).toBeHidden()
  await expect(periodLabel(page)).toBeFocused()
  await expect(periodLabel(page)).toHaveText('2025')
})

test('Custom sets a range and All time clears it', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await periodLabel(page).click()
  await chooser(page).getByRole('radio', { name: 'Custom' }).click()
  const apply = chooser(page).getByRole('button', { name: 'Apply' })
  await expect(apply).toBeDisabled()
  for (let month = 0; month < 7; month++) {
    await chooser(page).getByRole('button', { name: 'Previous month' }).click()
  }
  await chooser(page).getByRole('button', { name: 'Mar 10, 2026', exact: true }).click()
  await expect(apply).toBeDisabled()
  await chooser(page).getByRole('button', { name: 'Next month' }).click()
  await chooser(page).getByRole('button', { name: 'Apr 9, 2026', exact: true }).click()
  await expect(chooser(page)).toContainText('Mar 10, 2026')
  await expect(chooser(page)).toContainText('Apr 9, 2026')
  await apply.click()
  await expect(periodLabel(page)).toHaveText('Mar 10 – Apr 9, 2026')
  for (const name of ['Day', 'Month', 'Year', 'All']) {
    await expect(segment(page, name)).not.toBeChecked()
  }
  await expect(page.getByRole('button', { name: 'Previous period' })).toBeDisabled()

  await periodLabel(page).click()
  await expect(chooser(page).getByRole('radio', { name: 'Month' })).toBeChecked()
  await chooser(page)
    .getByRole('button', { name: /^All time/ })
    .click()
  await expect(chooser(page)).toBeHidden()
  await expect(periodLabel(page)).toHaveText('All time')
  await expect(segment(page, 'All')).toBeChecked()
})
