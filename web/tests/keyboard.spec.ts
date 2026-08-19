import type { Page } from '@playwright/test'

import { activeRow, expect, openMoneyflow, test } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

const refinement = (page: Page) => page.getByRole('navigation', { name: 'Active refinements' })

test('@smoke initial grid owns keyboard focus and moves without a pointer', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const first = await activeRow(page)
  await expect(first).toContainText('Example Housing')

  await page.keyboard.press('j')
  const second = await activeRow(page)
  await expect(second).not.toHaveAttribute('id', await first.getAttribute('id'))
  await page.keyboard.press('Home')
  await expect(await activeRow(page)).toHaveAttribute('id', await first.getAttribute('id'))
})

test('cycles every grouping, account direct view, detail, drill, subgroup, and back', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  for (const grouping of ['category', 'group', 'account', 'time', 'merchant']) {
    await page.keyboard.press('g')
    await expect(refinement(page)).toContainText(`Group: ${grouping}`)
  }

  await page.keyboard.press('Shift+A')
  await expect(refinement(page)).toContainText('Group: account')
  await page.keyboard.press('d')
  await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(refinement(page)).toContainText('Group: account')

  await page.keyboard.press('g')
  await expect(refinement(page)).toContainText('Group: time')
  await page.keyboard.press('g')
  await expect(refinement(page)).toContainText('Group: merchant')
  await page.keyboard.press('Enter')
  await expect(refinement(page)).toContainText('Example Housing')
  await page.keyboard.press('g')
  await expect(refinement(page)).toContainText('(by Category)')
  await page.keyboard.press('Enter')
  await expect(refinement(page)).toContainText('C:')
  await page.keyboard.press('Escape')
  await expect(refinement(page)).toContainText('by Category')
  await page.keyboard.press('Escape')
  await expect(refinement(page)).not.toContainText('by Category')
  await page.keyboard.press('Escape')
  await expect(refinement(page)).toContainText('Merchants')
})

test('time, sort, and exact selection refinements remain keyboard driven', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('s')
  await expect(refinement(page)).toContainText('Sort: merchant desc')
  await page.keyboard.press('s')
  await expect(refinement(page)).toContainText('Sort: count desc')
  await page.keyboard.press('v')
  await expect(refinement(page)).toContainText('Sort: count asc')

  await page.keyboard.press('Space')
  await expect(await activeRow(page)).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('Control+a')
  await expect(
    page
      .getByRole('row')
      .filter({ has: page.getByRole('gridcell') })
      .first(),
  ).toHaveAttribute('aria-selected', 'true')

  for (const grouping of ['category', 'group', 'account', 'time']) {
    await page.keyboard.press('g')
    await expect(refinement(page)).toContainText(`Group: ${grouping}`)
  }
  await page.keyboard.press('t')
  await expect(refinement(page)).toContainText('Months')
  await page.keyboard.press('Enter')
  await expect(refinement(page)).toContainText('Jan 2023')
  const unboundedURL = page.url()
  await page.keyboard.press('ArrowLeft')
  await expect(page).not.toHaveURL(unboundedURL)
  const previousPeriodURL = page.url()
  await page.keyboard.press('ArrowRight')
  await expect(page).not.toHaveURL(previousPeriodURL)
  await page.keyboard.press('a')
  await expect(refinement(page)).toContainText('Transactions')
  await expect(page).not.toHaveURL(/[?&]drill=time/)
})

test('search previews, commits, cancels, and reports invalid regular expressions', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('/')
  const search = page.getByRole('searchbox', { name: 'Search transactions' })
  await expect(search).toBeFocused()
  await search.fill('grocer')
  await expect(refinement(page)).toContainText('1 results')
  await search.press('Enter')
  await expect(page.getByRole('dialog', { name: 'Search transactions' })).toBeHidden()
  await expect(refinement(page)).toContainText('Search: grocer')
  await expect(page).toHaveURL(/[?&]q=grocer(?:&|$)/)

  await page.keyboard.press('/')
  await search.fill('[')
  await expect(page.getByText('That search expression is invalid.')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(refinement(page)).toContainText('Search: grocer')
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
})

test('filters and help trap shortcuts and restore the grid', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('f')
  const filters = page.getByRole('dialog', { name: 'Filter transactions' })
  await expect(filters).toBeVisible()
  await page.getByRole('checkbox', { name: 'Show hidden transactions' }).uncheck()
  await page.getByRole('checkbox', { name: 'Show transfers' }).check()
  await page.getByRole('button', { name: 'Apply filters' }).click()
  await expect(filters).toBeHidden()
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()

  const groupBefore = await refinement(page).textContent()
  await page.keyboard.press('?')
  const help = page.getByRole('dialog', { name: 'Keyboard shortcuts' })
  await expect(help).toBeVisible()
  await expect(help).toContainText('Quit application · TUI only')
  await page.keyboard.press('g')
  await expect(refinement(page)).toHaveText(groupBefore!)
  await page.keyboard.press('Escape')
  await expect(help).toBeHidden()
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
})

test('narrow layout keeps the same keyboard refinement surface', async ({ page, server }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await openMoneyflow(page, server)
  await page.keyboard.press('g')
  await expect(refinement(page)).toContainText('Group: category')
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
    await expect(review).toContainText('No duplicate transactions match the current view.')
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  } finally {
    await server.stop()
  }
})
