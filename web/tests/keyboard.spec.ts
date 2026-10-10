import type { Page } from '@playwright/test'

import { activeRow, cell, expect, openMoneyflow, test } from './fixtures'
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
  const groupings = ['Merchants', 'Categories', 'Groups', 'Accounts', 'Months']
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
  for (const grouping of ['Categories', 'Groups', 'Accounts', 'Months', 'Merchants']) {
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
  await expect(breadcrumb(page)).toHaveText('Months')
  await page.keyboard.press('g')
  await expect(breadcrumb(page)).toHaveText('Merchants')
  const merchant = await cell(await activeRow(page), 'name').innerText()
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
  const merchant = await cell(await activeRow(page), 'name').innerText()
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

test('the root crumb of a month opened from Months returns to Months', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  await expect(breadcrumb(page)).toHaveText('All transactions')
  await page.keyboard.press('5')
  await expect(breadcrumb(page)).toHaveText('Months')
  const period = await cell(await activeRow(page), 'period').innerText()
  await page.keyboard.press('Enter')
  await expect(breadcrumb(page).getByRole('listitem')).toHaveText(['Time', `› ${period}`])
  await breadcrumb(page).getByRole('button', { name: 'Time' }).click()
  await expect(breadcrumb(page)).toHaveText('Months')
  await expect(page.getByRole('columnheader', { name: 'Count', exact: true })).toBeVisible()
})

test('time, sort, and exact selection refinements remain keyboard driven', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('s')
  await expect(header(page, 'Merchant')).toHaveAttribute('aria-sort', 'descending')
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

  for (const grouping of ['Categories', 'Groups', 'Accounts', 'Months']) {
    await page.keyboard.press('g')
    await expect(breadcrumb(page)).toHaveText(grouping)
  }
  const period = await cell(await activeRow(page), 'period').innerText()
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

test('a row click moves the cursor and only the box toggles selection', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const third = grid.locator('[role="row"][aria-rowindex="4"]')
  await cell(third, 'net').click()
  await expect(grid).toHaveAttribute('aria-activedescendant', (await third.getAttribute('id'))!)
  await expect(grid).toBeFocused()
  await expect(third).toHaveAttribute('aria-selected', 'false')

  const fifth = grid.locator('[role="row"][aria-rowindex="6"]')
  await cell(fifth, 'select').click()
  await expect(fifth).toHaveAttribute('aria-selected', 'true')
  await expect(grid).toHaveAttribute('aria-activedescendant', (await third.getAttribute('id'))!)
  await expect(page.getByRole('row', { selected: true })).toHaveCount(1)
  await expect(grid).toBeFocused()

  await page.getByRole('button', { name: 'Select all matching' }).click()
  await expect(third).toHaveAttribute('aria-selected', 'true')
  await expect(grid).toBeFocused()

  const merchant = await cell(fifth, 'name').innerText()
  await cell(fifth, 'count').dblclick()
  await expect(breadcrumb(page).getByRole('listitem')).toHaveText(['Merchants', `› ${merchant}`])
})

test('header clicks sort by the server fields and keep the selection', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('Space')
  await expect(page.getByRole('row', { selected: true })).toHaveCount(1)
  const count = header(page, 'Count')
  await count.getByRole('button', { name: 'Count' }).click()
  await expect(count).toHaveAttribute('aria-sort', 'descending')
  await count.getByRole('button', { name: 'Count' }).click()
  await expect(count).toHaveAttribute('aria-sort', 'ascending')
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  await expect(page.getByRole('row', { selected: true })).toHaveCount(1)
  for (const name of ['In', 'Out', 'Share']) {
    await expect(header(page, name).getByRole('button')).toHaveCount(0)
    await expect(header(page, name)).not.toHaveAttribute('aria-sort')
  }
})

test('x in a grouped view explains why it cannot delete', async ({ page, server }) => {
  await openMoneyflow(page, server)
  const notice = page.getByRole('status', { name: 'Notice' })
  await page.keyboard.press('x')
  await expect(notice).toHaveText('Open the group to delete its transactions.')
  await expect(page.getByRole('dialog', { name: 'Confirm deletion' })).toHaveCount(0)
  // Well under the notice's 4 s, so only the next action can have cleared it.
  const cleared = { timeout: 1000 }
  await page.keyboard.press('j')
  await expect(notice).toHaveText('', cleared)

  // A box click and a header click send transitions without moving the cursor.
  const reason = 'Open the group to delete its transactions.'
  const grid = page.getByRole('grid', { name: 'Financial results' })
  await page.keyboard.press('x')
  await expect(notice).toHaveText(reason)
  await cell(grid.locator('[role="row"][aria-rowindex="5"]'), 'select').click()
  await expect(notice).toHaveText('', cleared)
  await page.keyboard.press('x')
  await expect(notice).toHaveText(reason)
  await header(page, 'Count').getByRole('button', { name: 'Count' }).click()
  await expect(notice).toHaveText('', cleared)
  await page.keyboard.press('x')
  await expect(notice).toHaveText(reason)
  await page
    .getByRole('group', { name: 'Cash flow' })
    .getByRole('button', { name: /^Sep 2026, / })
    .click()
  await expect(notice).toHaveText('', cleared)
  // The click scopes the table to September; its rows replace the ones read below.
  await expect(page.getByRole('banner').locator('button[aria-keyshortcuts="t"]')).toHaveText(
    'Sep 2026',
  )

  await page.keyboard.press('x')
  await expect(notice).toHaveText(reason)
  const row = await activeRow(page)
  const merchant = await cell(row, 'name').innerText()
  await cell(row, 'count').dblclick()
  await expect(breadcrumb(page).getByRole('listitem')).toHaveText(['Merchants', `› ${merchant}`])
  await expect(notice).toHaveText('', cleared)
})

test('a double-click drill waits for an earlier table transition', async ({ page, server }) => {
  await openMoneyflow(page, server)
  const events: string[] = []
  await page.route('**/view/transition', async (route) => {
    const action = route.request().postDataJSON().action
    events.push(`${action} sent`)
    if (action !== 'selection.toggle') return route.continue()
    await new Promise((resolve) => setTimeout(resolve, 500))
    const response = await route.fetch()
    events.push(`${action} answered`)
    await route.fulfill({ response })
  })
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const second = grid.locator('[role="row"][aria-rowindex="3"]')
  const label = await cell(second, 'name').innerText()
  await cell(grid.locator('[role="row"][aria-rowindex="4"]'), 'select').click()
  await cell(second, 'count').dblclick()
  await expect(breadcrumb(page)).toContainText(label)
  expect(events).toEqual(['selection.toggle sent', 'selection.toggle answered', 'view.drill sent'])
})

test('two quick box clicks keep both rows selected', async ({ page, server }) => {
  await openMoneyflow(page, server)
  // A slow toggle response gives the second click the chance to race the first.
  await page.route('**/view/transition', async (route) => {
    if (route.request().postDataJSON().action === 'selection.toggle') {
      await new Promise((resolve) => setTimeout(resolve, 300))
    }
    await route.continue()
  })
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const third = grid.locator('[role="row"][aria-rowindex="4"]')
  const fifth = grid.locator('[role="row"][aria-rowindex="6"]')
  await cell(third, 'select').click()
  await cell(fifth, 'select').click()
  await expect(third).toHaveAttribute('aria-selected', 'true')
  await expect(fifth).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('row', { selected: true })).toHaveCount(2)
})

/** Holds every selection toggle response for 300 ms, so keys pressed meanwhile queue behind it. */
async function slowToggles(page: Page): Promise<void> {
  await page.route('**/view/transition', async (route) => {
    if (route.request().postDataJSON().action === 'selection.toggle') {
      await new Promise((resolve) => setTimeout(resolve, 300))
    }
    await route.continue()
  })
}

test('Space, j, Space behind a slow toggle selects both rows the cursor was on', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  await slowToggles(page)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const row = (index: number) => grid.locator(`[role="row"][aria-rowindex="${index}"]`)
  await cell(row(6), 'select').click()
  await page.keyboard.press('Space')
  await page.keyboard.press('j')
  await page.keyboard.press('Space')
  for (const index of [2, 3, 6]) await expect(row(index)).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('row', { selected: true })).toHaveCount(3)
})

test('h right after Space hides the selection that Space made', async ({ page, server }) => {
  await openMoneyflow(page, server)
  await page.keyboard.press('d')
  const grid = page.getByRole('grid', { name: 'Financial results' })
  await expect(grid.getByRole('columnheader', { name: 'Date' })).toBeVisible()
  const first = grid.locator('[role="row"][aria-rowindex="2"]')
  const second = grid.locator('[role="row"][aria-rowindex="3"]')
  await page.keyboard.press('Space')
  await expect(first).toHaveAttribute('aria-selected', 'true')
  await slowToggles(page)
  await page.keyboard.press('j')
  await page.keyboard.press('Space')
  await page.keyboard.press('h')
  await expect(cell(second, 'flags')).toContainText('Hidden')
  await expect(cell(first, 'flags')).toContainText('Hidden')

  await page.keyboard.press('u')
  await expect(cell(second, 'flags')).not.toContainText('Hidden')
})

test('Enter opens Info on a transaction row and drills a grouped row', async ({ page, server }) => {
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  await page.keyboard.press('Enter')
  await expect(breadcrumb(page).getByRole('listitem')).toHaveCount(2)
  await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
  const url = page.url()

  await page.keyboard.press('Enter')
  const information = page.getByRole('dialog', { name: 'Transaction information' })
  await expect(information).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(information).toBeHidden()
  await expect(grid).toBeFocused()
  await expect(breadcrumb(page).getByRole('listitem')).toHaveCount(2)
  expect(page.url()).toBe(url)
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
  const noodleRows = await grid.getAttribute('aria-rowcount')
  const noodleRow = (await (await activeRow(page)).textContent()) ?? ''

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
  await expect(grid).toHaveAttribute('aria-rowcount', noodleRows ?? '')
  await expect(await activeRow(page)).toHaveText(noodleRow)
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
  const noodleCount = cell(
    page
      .getByRole('row')
      .filter({ has: page.getByRole('gridcell', { name: 'Noodle House', exact: true }) }),
    'count',
  )
  await expect(noodleCount).toHaveText('144')
  await expect(trigger).toHaveText('Filters')

  await page.keyboard.press('f')
  await expect(trigger).toBeFocused()
  await hidden.click()
  await expect(page).toHaveURL(/[?&]hidden=0/)
  await expect(noodleCount).toHaveText('138')
  await expect(trigger).toHaveText('Filters 1')
  await expect(hidden).toBeVisible()
  await hidden.click()
  await expect(page).not.toHaveURL(/[?&]hidden=0/)
  await expect(noodleCount).toHaveText('144')
  await expect(trigger).toHaveText('Filters')
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
  await expect(trigger).toHaveText('Filters 1')
})

test('keys pressed in an open Filters menu or Currency list stay in it', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const viewBefore = page.url()
  const cursorBefore = await grid.getAttribute('aria-activedescendant')

  await page.keyboard.press('f')
  const filters = page.getByRole('button', { name: 'Filters' })
  const transfers = page.getByRole('button', { name: 'Show transfers' })
  await expect(transfers).toBeVisible()
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('d')
  await expect(transfers).toBeVisible()
  await expect(page).toHaveURL(viewBefore)
  await expect(grid).toHaveAttribute('aria-activedescendant', cursorBefore ?? '')
  await page.keyboard.press('Escape')
  await expect(transfers).toBeHidden()
  await expect(filters).toBeFocused()

  const currency = page.getByRole('combobox', { name: 'Currency: USD' })
  await currency.click()
  const list = page.getByRole('listbox')
  await expect(list).toBeVisible()
  await page.keyboard.press('d')
  await page.keyboard.press('g')
  await expect(list).toBeVisible()
  await expect(page).toHaveURL(viewBefore)
  await expect(breadcrumb(page)).toHaveText('Merchants')
  await page.keyboard.press('Escape')
  await expect(list).toBeHidden()
  await expect(currency).toBeFocused()
  await expect(grid).toHaveAttribute('aria-activedescendant', cursorBefore ?? '')
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
  // Compact phone rows move and select with the same keys.
  const first = await activeRow(page)
  expect((await first.boundingBox())!.height).toBeGreaterThanOrEqual(52)
  const cursorRow = async () => (await activeRow(page)).getAttribute('aria-rowindex')
  await page.keyboard.press('j')
  await expect.poll(cursorRow).toBe('3')
  await page.keyboard.press('k')
  await expect.poll(cursorRow).toBe('2')
  await page.keyboard.press('Space')
  await expect(await activeRow(page)).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('/')
  await expect(page.getByRole('searchbox', { name: 'Search transactions' })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  await expect(page.getByRole('searchbox', { name: 'Search transactions' })).toBeHidden()

  // f opens the Filters menu inside the View sheet; Esc closes one layer at a time and
  // never reaches the grid's Esc, which would clear the selection.
  await page.keyboard.press('f')
  const sheet = page.getByRole('dialog', { name: 'View' })
  await expect(sheet.getByRole('button', { name: 'Show transfers' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(sheet.getByRole('button', { name: 'Show transfers' })).toBeHidden()
  await expect(sheet).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(sheet).toBeHidden()
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  await expect(await activeRow(page)).toHaveAttribute('aria-selected', 'true')
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
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()

    await page.keyboard.press('w')
    const pendingReview = page.getByRole('dialog', { name: 'Review pending changes' })
    await expect(pendingReview).toContainText('Delete transaction')
    await pendingReview.getByRole('button', { name: 'Commit reviewed changes' }).focus()
    await page.keyboard.press('Enter')
    await expect(pendingReview).toBeHidden()
    await expect(page.getByText(/0 pending edit/)).toBeVisible()
    await page.keyboard.press('Shift+D')
    await expect(review).toContainText('No duplicate transactions match the current view.')
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  } finally {
    await server.stop()
  }
})
