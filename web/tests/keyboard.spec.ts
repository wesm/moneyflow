import type { Page } from '@playwright/test'

import { activeRow, expect, openMoneyflow, test } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

const breadcrumb = (page: Page) => page.getByRole('navigation', { name: 'Breadcrumb' })
const breakDownBy = (page: Page) => page.getByRole('radiogroup', { name: 'Break down by' })
const header = (page: Page, name: string) =>
  page
    .getByRole('grid', { name: 'Financial results' })
    .getByRole('columnheader', { name, exact: true })

test('@smoke initial grid owns keyboard focus and moves without a pointer', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const first = await activeRow(page)
  await expect(first).toHaveAttribute('aria-rowindex', '2')

  await page.keyboard.press('j')
  const second = await activeRow(page)
  await expect(second).not.toHaveAttribute('id', await first.getAttribute('id'))
  await page.keyboard.press('Home')
  await expect(await activeRow(page)).toHaveAttribute('id', await first.getAttribute('id'))
})

test('the active row stays fully visible while moving past the fold and back', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
  const steps = 60
  for (let step = 1; step <= steps; step++) {
    await page.keyboard.press('j')
    const row = await activeRow(page)
    await expect(row).toHaveAttribute('aria-rowindex', String(step + 2))
    await expect(row).toBeInViewport({ ratio: 1 })
  }
  for (let step = steps - 1; step >= 0; step--) {
    await page.keyboard.press('k')
    const row = await activeRow(page)
    await expect(row).toHaveAttribute('aria-rowindex', String(step + 2))
    await expect(row).toBeInViewport({ ratio: 1 })
  }
})

test('keys 1 to 5 group by merchant, category, group, account, and time', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const groupings = ['Merchants', 'Categories', 'Groups', 'Accounts', 'Time']
  for (const [index, grouping] of groupings.entries()) {
    await page.keyboard.press(String(index + 1))
    await expect(breadcrumb(page)).toHaveText(grouping)
  }
  await page.keyboard.press('2')
  await expect(breadcrumb(page)).toHaveText('Categories')
  await expect(breakDownBy(page)).toHaveCount(0)
})

test('cycles every grouping, account direct view, detail, drill, subgroup, and back', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  for (const grouping of ['Categories', 'Groups', 'Accounts', 'Time', 'Merchants']) {
    await page.keyboard.press('g')
    await expect(breadcrumb(page)).toHaveText(grouping)
  }

  await page.keyboard.press('Shift+A')
  await expect(breadcrumb(page)).toHaveText('Accounts')
  await page.keyboard.press('d')
  await expect(breadcrumb(page)).toHaveText('All transactions')
  await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(breadcrumb(page)).toHaveText('Accounts')
  await expect(page.getByRole('columnheader', { name: 'Count', exact: true })).toBeVisible()

  await page.keyboard.press('g')
  await expect(breadcrumb(page)).toHaveText('Time')
  await page.keyboard.press('g')
  await expect(breadcrumb(page)).toHaveText('Merchants')
  const merchant = await (await activeRow(page)).getByRole('gridcell').first().innerText()
  await page.keyboard.press('Enter')
  await expect(breadcrumb(page).getByRole('listitem')).toHaveText(['Merchants', `› ${merchant}`])
  await expect(breadcrumb(page).getByRole('button', { name: 'Merchants' })).toBeVisible()
  await expect(breadcrumb(page).getByText(merchant)).toHaveAttribute('aria-current', 'page')
  await expect(breakDownBy(page).getByRole('radio')).toHaveText([
    'Transactions',
    'Category',
    'Group',
    'Account',
    'Time',
  ])
  await expect(breakDownBy(page).getByRole('radio', { name: 'Transactions' })).toBeChecked()

  await page.keyboard.press('g')
  await expect(breadcrumb(page).getByRole('listitem').last()).toHaveText('› by Category')
  await expect(breakDownBy(page).getByRole('radio', { name: 'Category' })).toBeChecked()
  await page.keyboard.press('Enter')
  await expect(breadcrumb(page).getByRole('listitem')).toHaveCount(3)
  await expect(breakDownBy(page).getByRole('radio')).toHaveText([
    'Transactions',
    'Group',
    'Account',
    'Time',
  ])
  await page.keyboard.press('Escape')
  await expect(breadcrumb(page)).toContainText('by Category')
  await page.keyboard.press('Escape')
  await expect(breadcrumb(page)).not.toContainText('by Category')
  await expect(breadcrumb(page).getByRole('listitem')).toHaveCount(2)
  await page.keyboard.press('Escape')
  await expect(breadcrumb(page)).toHaveText('Merchants')
  await expect(breakDownBy(page)).toHaveCount(0)
})

test('Break down by sub-groups a drill and returns to its transactions', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const merchant = await (await activeRow(page)).getByRole('gridcell').first().innerText()
  await page.keyboard.press('Enter')
  await expect(breakDownBy(page)).toBeVisible()
  const drillURL = page.url()
  const initialLength = await page.evaluate(() => history.length)

  await breakDownBy(page).getByRole('radio', { name: 'Account' }).click()
  await expect(breadcrumb(page).getByRole('listitem')).toHaveText([
    'Merchants',
    `› ${merchant}`,
    '› by Account',
  ])
  await expect(page).toHaveURL(/[?&]subgroup=account/)
  await expect(page.getByRole('columnheader', { name: 'Count', exact: true })).toBeVisible()
  expect(await page.evaluate(() => history.length)).toBe(initialLength + 1)

  await breakDownBy(page).getByRole('radio', { name: 'Transactions' }).click()
  await expect(page).toHaveURL(drillURL)
  await expect(breadcrumb(page).getByRole('listitem')).toHaveText(['Merchants', `› ${merchant}`])
  await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
  expect(await page.evaluate(() => history.length)).toBe(initialLength + 1)
})

test('time, sort, and exact selection refinements remain keyboard driven', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('s')
  await expect(header(page, 'merchant')).toHaveAttribute('aria-sort', 'descending')
  await page.keyboard.press('s')
  await expect(header(page, 'Count')).toHaveAttribute('aria-sort', 'descending')
  await page.keyboard.press('v')
  await expect(header(page, 'Count')).toHaveAttribute('aria-sort', 'ascending')

  await page.keyboard.press('Space')
  await expect(await activeRow(page)).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('Control+a')
  await expect(
    page
      .getByRole('row')
      .filter({ has: page.getByRole('gridcell') })
      .first(),
  ).toHaveAttribute('aria-selected', 'true')

  for (const grouping of ['Categories', 'Groups', 'Accounts', 'Time']) {
    await page.keyboard.press('g')
    await expect(breadcrumb(page)).toHaveText(grouping)
  }
  const period = await (await activeRow(page)).getByRole('gridcell').first().innerText()
  await page.keyboard.press('Enter')
  await expect(breadcrumb(page).getByRole('listitem')).toHaveText(['Time', `› ${period}`])
  const unboundedURL = page.url()
  await page.keyboard.press('ArrowLeft')
  await expect(page).not.toHaveURL(unboundedURL)
  const previousPeriodURL = page.url()
  await page.keyboard.press('ArrowRight')
  await expect(page).not.toHaveURL(previousPeriodURL)
  await page.keyboard.press('a')
  await expect(breadcrumb(page)).toHaveText('All transactions')
  await expect(page).not.toHaveURL(/[?&]drill=time/)
})

test('search previews, commits, cancels, and reports invalid regular expressions', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const initialLength = await page.evaluate(() => history.length)
  await page.keyboard.press('/')
  const search = page.getByRole('searchbox', { name: 'Search transactions' })
  await expect(search).toBeFocused()
  await search.pressSequentially('noodle')
  await expect(page).toHaveURL(/[?&]q=noodle(?:&|$)/)
  await expect(await activeRow(page)).toContainText('Noodle House')
  expect(await page.evaluate(() => history.length)).toBe(initialLength)
  await search.press('Enter')
  await expect(grid).toBeFocused()
  await expect(search).toHaveValue('noodle')
  await expect(page).toHaveURL(/[?&]q=noodle(?:&|$)/)
  expect(await page.evaluate(() => history.length)).toBe(initialLength + 1)

  await page.keyboard.press('/')
  await expect(search).toBeFocused()
  await search.pressSequentially('pizza')
  await expect(page).toHaveURL(/[?&]q=pizza(?:&|$)/)
  await expect(await activeRow(page)).toContainText('Slice Pizza')
  await page.keyboard.press('Escape')
  await expect(grid).toBeFocused()
  await expect(search).toHaveValue('noodle')
  await expect(page).toHaveURL(/[?&]q=noodle(?:&|$)/)
  await expect(await activeRow(page)).toContainText('Noodle House')
  expect(await page.evaluate(() => history.length)).toBe(initialLength + 1)

  await page.keyboard.press('/')
  await search.fill('[')
  await expect(page.getByText('That search expression is invalid.')).toBeVisible()
  await expect(search).toHaveAttribute('aria-invalid', 'true')
  await page.keyboard.press('Enter')
  await expect(search).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page.getByText('That search expression is invalid.')).toBeHidden()
  await expect(page).toHaveURL(/[?&]q=noodle(?:&|$)/)
  await expect(grid).toBeFocused()

  await page.keyboard.press('Escape')
  await expect(page).not.toHaveURL(/[?&]q=/)
  await expect(search).toHaveValue('')
})

test('the Filters chip shows and hides hidden transactions and keeps the period', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const trigger = page.getByRole('button', { name: 'Filters' })
  const hidden = page.getByRole('button', { name: 'Show hidden transactions' })
  const noodleCount = page
    .getByRole('row')
    .filter({ has: page.getByRole('gridcell', { name: 'Noodle House', exact: true }) })
    .getByRole('gridcell')
    .nth(1)
  await expect(noodleCount).toHaveText('144')
  await expect(trigger).toHaveText('Filters 1')

  await page.keyboard.press('f')
  await expect(trigger).toBeFocused()
  await hidden.click()
  await expect(page).toHaveURL(/[?&]hidden=0/)
  await expect(noodleCount).toHaveText('138')
  await expect(trigger).toHaveText('Filters')
  await expect(hidden).toBeVisible()
  await hidden.click()
  await expect(page).not.toHaveURL(/[?&]hidden=0/)
  await expect(noodleCount).toHaveText('144')
  await page.keyboard.press('Escape')
  await expect(hidden).toBeHidden()
  await expect(trigger).toBeFocused()

  await page
    .getByRole('radiogroup', { name: 'Period', exact: true })
    .getByRole('radio', { name: 'Month' })
    .click()
  await expect(page).toHaveURL(/[?&]from=2026-10-01&to=2026-10-31/)
  await trigger.click()
  await page.getByRole('button', { name: 'Show transfers' }).click()
  await expect(page).toHaveURL(/[?&]transfers=1/)
  await expect(page).toHaveURL(/[?&]from=2026-10-01&to=2026-10-31/)
  await expect(trigger).toHaveText('Filters 2')
})

test('switching currency inside a drill returns to the top level in that currency', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('Enter')
  await expect(breadcrumb(page).getByRole('listitem')).toHaveCount(2)
  await expect(page).toHaveURL(/[?&]drill=/)

  await page.getByRole('combobox', { name: 'Currency: USD' }).click()
  const options = page.getByRole('listbox').getByRole('option')
  await expect(options).toHaveText(['USD · 2,163 transactions', 'EUR · 34 transactions'])
  await options.filter({ hasText: 'EUR' }).click()

  await expect(page.getByRole('combobox', { name: 'Currency: EUR' })).toBeVisible()
  await expect(page).toHaveURL(/[?&]cur=EUR%3A2/)
  await expect(page).not.toHaveURL(/[?&]drill=/)
  await expect(breadcrumb(page)).toHaveText('Merchants')
  await expect(breakDownBy(page)).toHaveCount(0)
})

test('help traps shortcuts and restores the grid', async ({ page, server }) => {
  await openMoneyflow(page, server)
  const viewBefore = page.url()
  await page.keyboard.press('?')
  const help = page.getByRole('dialog', { name: 'Keyboard shortcuts' })
  await expect(help).toBeVisible()
  await expect(help).toContainText('Quit application · TUI only')
  await page.keyboard.press('g')
  await expect(breadcrumb(page)).toHaveText('Merchants')
  await expect(page).toHaveURL(viewBefore)
  await page.keyboard.press('Escape')
  await expect(help).toBeHidden()
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
})

test('narrow layout keeps the same keyboard refinement surface', async ({ page, server }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await openMoneyflow(page, server)
  await page.keyboard.press('g')
  await expect(breadcrumb(page)).toHaveText('Categories')
  await page.keyboard.press('Space')
  await expect(await activeRow(page)).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('/')
  await expect(page.getByRole('searchbox', { name: 'Search transactions' })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
})

test('duplicate review remains keyboard driven and restores the finance grid', async ({ page }) => {
  const server = await startE2EServer({
    fixturePath: 'testdata/fixtures/duplicate_transactions.json',
  })
  try {
    await openMoneyflow(page, server)
    await page.keyboard.press('Shift+D')
    const review = page.getByRole('dialog', { name: 'Duplicate transactions' })
    await expect(review).toBeVisible()
    await expect(review).toContainText('1 duplicate group')
    await page.keyboard.press('Space')
    await expect(review.getByRole('row', { selected: true })).toHaveCount(1)
    await page.keyboard.press('i')
    await expect(review.getByRole('region', { name: 'Transaction information' })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(review.getByRole('grid', { name: 'Likely duplicate transactions' })).toBeVisible()
    await page.keyboard.press('x')
    await expect(page.getByRole('dialog', { name: 'Confirm deletion' })).toContainText(
      'Delete 1 transaction?',
    )
    await page.keyboard.press('Enter')
    await expect(review).toContainText('1 duplicate group')
    await expect(review.getByRole('row')).toHaveCount(3)
    await expect(review).toContainText('Staged deletion for 1 transaction.')
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()

    await page.keyboard.press('w')
    const pendingReview = page.getByRole('dialog', { name: 'Review pending changes' })
    await expect(pendingReview).toContainText('Delete transaction')
    await pendingReview.getByRole('button', { name: 'Commit reviewed changes' }).focus()
    await page.keyboard.press('Enter')
    await expect(pendingReview).toBeHidden()
    await expect(page.getByText(/0 pending/)).toBeVisible()
    await page.keyboard.press('Shift+D')
    await expect(review).toContainText('No duplicate transactions match the current view.')
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  } finally {
    await server.stop()
  }
})
