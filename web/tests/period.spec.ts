import type { Page } from '@playwright/test'

import type { E2EServer } from '../scripts/e2e-server'
import { cell, expect, openMoneyflow, test } from './fixtures'

const grid = (page: Page) => page.getByRole('grid', { name: 'Financial results' })
const periodLabel = (page: Page) =>
  page.getByRole('banner').locator('button[aria-haspopup="dialog"][aria-keyshortcuts="t"]')
const segment = (page: Page, name: string) =>
  page.getByRole('radiogroup', { name: 'Period', exact: true }).getByRole('radio', { name })
const chooser = (page: Page) => page.getByRole('dialog', { name: 'Choose period' })

// A link can carry a period with no transactions; the workspace then shows its empty state.
async function openEmptyPeriod(page: Page, server: E2EServer, from: string, to: string) {
  await page.goto(`${server.url}?cur=USD%3A2&from=${from}&to=${to}&v=1`)
  const workspace = page.getByRole('main', { name: 'Moneyflow workspace' })
  await expect(workspace.getByText(/^No transactions in /)).toBeVisible()
}

async function rowCount(page: Page, merchant: string) {
  const row = page
    .getByRole('row')
    .filter({ has: page.getByRole('gridcell', { name: merchant, exact: true }) })
  return cell(row, 'count')
}

test('the Month segment and ‹ move the scope and the grid follows', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await expect(periodLabel(page)).toHaveAccessibleName('All time')
  await expect(segment(page, 'All')).toBeChecked()

  await segment(page, 'Month').click()
  await expect(periodLabel(page)).toHaveAccessibleName('Oct 2026')
  await expect(segment(page, 'Month')).toBeChecked()

  await page.getByRole('button', { name: 'Previous period' }).click()
  await expect(periodLabel(page)).toHaveAccessibleName('Sep 2026')
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
  await expect(periodLabel(page)).toHaveAccessibleName('Oct 2026')
  await segment(page, 'Month').focus()
  await page.keyboard.press('ArrowLeft')
  await expect(periodLabel(page)).toHaveAccessibleName('Oct 7, 2026')
  await expect(segment(page, 'Day')).toBeFocused()
  await page.waitForLoadState('networkidle')
  await expect(periodLabel(page)).toHaveAccessibleName('Oct 7, 2026')
})

test('the period label shows its t key', async ({ page, server }) => {
  await openMoneyflow(page, server)
  const badge = periodLabel(page).locator('kbd')
  await expect(badge).toBeVisible()
  await expect(badge).toContainText('t')
  await expect(periodLabel(page)).toHaveAccessibleName('All time')
})

test('t opens on the current month and arrows with Enter pick another', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('t')
  await expect(chooser(page)).toBeVisible()
  await expect(chooser(page).getByRole('button', { name: 'Oct 2026' })).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await expect(chooser(page).getByRole('button', { name: 'Sep 2026' })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(chooser(page)).toBeHidden()
  await expect(periodLabel(page)).toHaveAccessibleName('Sep 2026')
  await expect(segment(page, 'Month')).toBeChecked()
  await expect(grid(page)).toBeFocused()
})

test('y switches to Year and arrows with Enter pick a year; Esc returns to the label', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('t')
  await page.keyboard.press('y')
  await expect(chooser(page).getByRole('radio', { name: 'Year' })).toBeChecked()
  await expect(chooser(page).getByRole('button', { name: '2026' })).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await expect(chooser(page).getByRole('button', { name: '2025' })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(chooser(page)).toBeHidden()
  await expect(periodLabel(page)).toHaveAccessibleName('2025')
  await expect(segment(page, 'Year')).toBeChecked()
  await expect(grid(page)).toBeFocused()

  await periodLabel(page).click()
  await expect(chooser(page)).toBeVisible()
  await page.keyboard.press('j')
  await expect(chooser(page)).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(chooser(page)).toBeHidden()
  await expect(periodLabel(page)).toBeFocused()
  await expect(periodLabel(page)).toHaveAccessibleName('2025')
})

test('d moves into the calendar, where arrows cross into the previous month', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('t')
  await page.keyboard.press('d')
  await expect(chooser(page).getByRole('radio', { name: 'Day' })).toBeChecked()
  const latest = chooser(page).getByRole('button', { name: 'Oct 7, 2026', exact: true })
  await expect(latest).toBeFocused()
  await page.keyboard.press('ArrowRight')
  await expect(latest).toBeFocused()
  await page.keyboard.press('ArrowUp')
  await expect(chooser(page).getByRole('button', { name: /^September 2026/ })).toBeVisible()
  await page.keyboard.press('ArrowLeft')
  const day = chooser(page).getByRole('button', { name: 'Sep 29, 2026', exact: true })
  await expect(day).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(chooser(page)).toBeHidden()
  await expect(periodLabel(page)).toHaveAccessibleName('Sep 29, 2026')
  await expect(segment(page, 'Day')).toBeChecked()
})

test('c picks custom endpoints with arrows and Enter', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('t')
  await page.keyboard.press('c')
  await expect(chooser(page).getByRole('radio', { name: 'Custom' })).toBeChecked()
  await expect(
    chooser(page).getByRole('button', { name: 'Oct 7, 2026', exact: true }),
  ).toBeFocused()
  await page.keyboard.press('ArrowUp')
  await page.keyboard.press('Enter')
  await page.keyboard.press('ArrowRight')
  await page.keyboard.press('Enter')
  await expect(chooser(page)).toContainText('Sep 30, 2026')
  await expect(chooser(page)).toContainText('Oct 1, 2026')
  await expect(chooser(page).getByRole('button', { name: 'Apply' })).toBeEnabled()
  await expect(periodLabel(page)).toHaveAccessibleName('All time')
})

test('a period before the data opens the chooser on the first year with data', async ({
  page,
  server,
}) => {
  await openEmptyPeriod(page, server, '2020-03-01', '2020-03-31')
  await expect(periodLabel(page)).toHaveAccessibleName('Mar 2020')
  await periodLabel(page).focus()
  await page.keyboard.press('t')
  const focused = chooser(page).locator('button:focus')
  await expect(focused).toBeEnabled()
  await expect(focused).toHaveAccessibleName('Jan 2024')

  await page.keyboard.press('y')
  await expect(chooser(page).getByRole('radio', { name: 'Year' })).toBeChecked()
  await expect(chooser(page).getByRole('button', { name: '2024' })).toBeFocused()
  await page.keyboard.press('m')
  await expect(chooser(page).getByRole('button', { name: 'Jan 2024' })).toBeFocused()
  await page.keyboard.press('ArrowRight')
  await page.keyboard.press('Enter')
  await expect(periodLabel(page)).toHaveAccessibleName('Feb 2024')
})

test('a period after the data opens the chooser on the latest month', async ({ page, server }) => {
  await openEmptyPeriod(page, server, '2030-05-01', '2030-05-31')
  await periodLabel(page).focus()
  await page.keyboard.press('t')
  await expect(chooser(page).getByRole('button', { name: 'Oct 2026' })).toBeFocused()
  await page.keyboard.press('d')
  await expect(
    chooser(page).getByRole('button', { name: 'Oct 7, 2026', exact: true }),
  ).toBeFocused()
})

test('from the tabs, Left and Right switch and Down or Enter moves into the body', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('t')
  await page.keyboard.press('y')
  await page.keyboard.press('Shift+Tab')
  const kinds = chooser(page).getByRole('radiogroup', { name: 'Period type' })
  await expect(kinds.getByRole('radio', { name: 'Year' })).toBeFocused()
  await page.keyboard.press('ArrowDown')
  await expect(chooser(page).getByRole('button', { name: '2026' })).toBeFocused()

  await page.keyboard.press('Shift+Tab')
  await page.keyboard.press('ArrowLeft')
  await expect(kinds.getByRole('radio', { name: 'Month' })).toBeChecked()
  await expect(kinds.getByRole('radio', { name: 'Month' })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(chooser(page).getByRole('button', { name: 'Oct 2026' })).toBeFocused()
  await expect(chooser(page)).toBeVisible()
  await expect(periodLabel(page)).toHaveAccessibleName('All time')
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
  await expect(periodLabel(page)).toHaveAccessibleName('Mar 10 – Apr 9, 2026')
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
  await expect(periodLabel(page)).toHaveAccessibleName('All time')
  await expect(segment(page, 'All')).toBeChecked()
})

test('an empty month jumps to the latest month', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await segment(page, 'Month').click()
  await expect(periodLabel(page)).toHaveAccessibleName('Oct 2026')
  await grid(page).focus()
  await page.keyboard.press('ArrowRight')
  await expect(periodLabel(page)).toHaveAccessibleName('Nov 2026')
  const workspace = page.getByRole('main', { name: 'Moneyflow workspace' })
  await expect(workspace.getByText('No transactions in Nov 2026', { exact: true })).toBeVisible()
  await expect(grid(page)).toHaveCount(0)
  const jump = workspace.getByRole('button', { name: 'Jump to latest' })
  await expect(jump).toBeFocused()
  await expect(page.getByRole('contentinfo')).not.toContainText('Open')

  await page.keyboard.press('Enter')
  await expect(periodLabel(page)).toHaveAccessibleName('Oct 2026')
  await expect(grid(page)).toBeFocused()
})

test('an empty month inside a merchant keeps the table empty state', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await segment(page, 'Month').click()
  await expect(periodLabel(page)).toHaveAccessibleName('Oct 2026')
  await grid(page).focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/[?&]drill=merchant/)
  await page.getByRole('button', { name: 'Next period' }).click()
  await expect(periodLabel(page)).toHaveAccessibleName('Nov 2026')
  const workspace = page.getByRole('main', { name: 'Moneyflow workspace' })
  await expect(workspace.getByText('No transactions', { exact: true })).toBeVisible()
  await expect(workspace.getByRole('button', { name: 'Jump to latest' })).toHaveCount(0)
})

test('Esc closes the chooser inside a drill, then a second Esc goes back', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/[?&]drill=merchant/)
  await periodLabel(page).focus()
  await page.keyboard.press('t')
  await expect(chooser(page)).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(chooser(page)).toBeHidden()
  await expect(periodLabel(page)).toBeFocused()
  await expect(page).toHaveURL(/[?&]drill=merchant/)
  await page.keyboard.press('Escape')
  await expect(page).not.toHaveURL(/[?&]drill=/)
})

test('tabbing out of the chooser closes it and restores the shortcuts', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await periodLabel(page).click()
  await expect(chooser(page).getByRole('button', { name: 'Oct 2026' })).toBeFocused()
  await chooser(page).getByRole('radio', { name: 'Month' }).focus()
  await page.keyboard.press('Shift+Tab')
  await expect(chooser(page)).toBeHidden()
  await expect(segment(page, 'All')).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(periodLabel(page)).toBeFocused()
  await expect(chooser(page)).toBeHidden()
  await page.keyboard.press('g')
  await expect(page).toHaveURL(/[?&]group=category/)
})
