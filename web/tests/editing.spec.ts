import { expect, test, type Page } from '@playwright/test'

import { activeAggregateCount, activeRow, cell, openMoneyflow } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

test('every editing workflow starts from its TUI key and restores the table', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    await page.keyboard.press('Enter')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()

    await page.keyboard.press('m')
    const merchant = page.getByRole('dialog', { name: 'Edit merchant' })
    await expect(merchant).toBeVisible()
    await expect(merchant).toContainText(/\d+\s+transactions? affected/)
    await expect(merchant.getByRole('table')).toBeVisible()
    await expect(merchant.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    const merchantName = page.getByLabel('Merchant name')
    await expect(merchantName).toBeFocused()
    await merchantName.fill('Browser Merchant')
    await merchantName.press('Enter')
    await expect(merchant).toBeHidden()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()

    await page.keyboard.press('c')
    await expect(page.getByRole('dialog', { name: 'Change category' })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()

    await page.keyboard.press('Shift+C')
    await expect(page.getByRole('dialog', { name: 'Manage categories' })).toBeVisible()
    await page.keyboard.press('Escape')
    await page.keyboard.press('Shift+G')
    await expect(page.getByRole('dialog', { name: 'Manage category groups' })).toBeVisible()
    await page.keyboard.press('Escape')

    await page.keyboard.press('h')
    await expect(page.getByText(/2 pending edit/)).toBeVisible()
    await expect(page.getByRole('status').filter({ hasText: /^Hidden 1 transaction/ })).toHaveText(
      'Hidden 1 transaction · Undo u',
    )
    await expect((await activeRow(page)).getByLabel('Pending edits')).toBeVisible()
    await page.keyboard.press('u')
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await expect(page.getByText(/1 redo/)).toBeVisible()
    await expect(page.getByRole('status').filter({ hasText: /^Undid / })).toHaveText(
      'Undid Toggle report visibility · Redo U',
    )
    await page.keyboard.press('Shift+U')
    await expect(page.getByText(/2 pending edit/)).toBeVisible()
    await expect(page.getByRole('status').filter({ hasText: /^Redid / })).toHaveText(
      'Redid Toggle report visibility · Undo u',
    )
    await page.keyboard.press('w')
    await expect(page.getByRole('dialog', { name: 'Review pending changes' })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  } finally {
    await server.stop()
  }
})

test('a staged rename shows inline in gold while the grouped Net stays committed', async ({
  page,
}) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    const grid = page.getByRole('grid', { name: 'Financial results' })
    const group = await activeRow(page)
    const merchant = await cell(group, 'name').innerText()
    const net = await cell(group, 'net').innerText()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()

    await page.keyboard.press('m')
    const name = page.getByLabel('Merchant name')
    await expect(name).toBeFocused()
    await name.fill('Renamed Example')
    await expect(page.getByRole('button', { name: 'Save pending change' })).toBeEnabled()
    await name.press('Enter')
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    const staged = cell(await activeRow(page), 'merchant').locator('.mf-staged')
    await expect(staged).toHaveText(`${merchant} → Renamed Example`)
    await expect(staged.locator('s')).toHaveText(merchant)
    const colors = await staged.evaluate((element) => {
      const probe = document.createElement('span')
      probe.style.color = 'var(--mf-pending)'
      document.body.append(probe)
      const pending = getComputedStyle(probe).color
      probe.remove()
      return { staged: getComputedStyle(element).color, pending }
    })
    expect(colors.staged).toBe(colors.pending)

    await page.keyboard.press('Escape')
    const committed = grid
      .getByRole('row')
      .filter({ has: page.getByText(merchant, { exact: true }) })
    await expect(cell(committed, 'net')).toHaveText(net)
    await expect(committed.getByLabel('Pending edits')).toHaveCount(1)
  } finally {
    await server.stop()
  }
})

test('the hover pencil and double-click open editors for the pointed row', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    await page.keyboard.press('d')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    const grid = page.getByRole('grid', { name: 'Financial results' })
    const cursorPencil = cell(await activeRow(page), 'merchant').getByRole('button', {
      name: 'Edit merchant',
    })
    await expect(cursorPencil).toBeVisible()

    const third = grid.locator('[role="row"][aria-rowindex="4"]')
    const pencil = cell(third, 'merchant').getByRole('button', { name: 'Edit merchant' })
    await expect(pencil).toBeHidden()
    await third.hover()
    await expect(pencil).toBeVisible()
    await pencil.click()
    const merchant = page.getByRole('dialog', { name: 'Edit merchant' })
    await expect(merchant).toBeVisible()
    const name = page.getByLabel('Merchant name')
    await name.fill('Pointer Merchant')
    await expect(page.getByRole('button', { name: 'Save pending change' })).toBeEnabled()
    await name.press('Enter')
    await expect(merchant).toBeHidden()
    await expect(grid).toBeFocused()
    await expect(cell(third, 'merchant').locator('.mf-staged')).toContainText('→ Pointer Merchant')

    const fifth = grid.locator('[role="row"][aria-rowindex="6"]')
    await cell(fifth, 'category').dblclick()
    await expect(page.getByRole('dialog', { name: 'Change category' })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(grid).toBeFocused()
    await cell(fifth, 'date').dblclick()
    await expect(page.getByRole('dialog', { name: 'Transaction information' })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(grid).toBeFocused()
    await expect(page.getByRole('row', { selected: true })).toHaveCount(0)
  } finally {
    await server.stop()
  }
})

test('bulk merchant edit clears a selection resolved beyond browser presentation state', async ({
  page,
}) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    let affected = 0
    await page.keyboard.press('j')
    affected += await activeAggregateCount(page, 3)
    await page.keyboard.press('Space')
    await expect(await activeRow(page)).toHaveAttribute('aria-selected', 'true')
    await page.keyboard.press('j')
    affected += await activeAggregateCount(page, 4)
    await page.keyboard.press('Space')
    await expect(page.getByRole('row', { selected: true })).toHaveCount(2)
    await page.keyboard.press('m')
    await expect(page.getByRole('combobox', { name: /Selected transactions/ })).toBeVisible()
    await expect(page.getByRole('dialog', { name: 'Edit merchant' })).toContainText(
      `${affected} transactions affected`,
    )
    const name = page.getByLabel('Merchant name')
    await name.fill('Normalized Merchant')
    await name.press('Enter')
    await expect(page.getByRole('dialog', { name: 'Edit merchant' })).toBeHidden()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    await expect(page.getByText(new RegExp(`\\b${affected} transactions\\b`))).toBeVisible()
    await expect(page.getByRole('row', { selected: true })).toHaveCount(0)
  } finally {
    await server.stop()
  }
})

test('x stages count-only deletion and review keeps commit explicit', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    await page.keyboard.press('d')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    const merchant = await cell(await activeRow(page), 'merchant').innerText()
    await page.keyboard.press('x')
    const confirmation = page.getByRole('dialog', { name: 'Confirm deletion' })
    await expect(confirmation).toContainText('Delete 1 transaction?')
    await expect(confirmation).not.toContainText(merchant)
    await page.keyboard.press('Escape')
    await expect(page.getByText(/0 pending edit/)).toBeVisible()

    await page.keyboard.press('x')
    await page.keyboard.press('Enter')
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await expect(page.getByRole('status').filter({ hasText: /^Deleted / })).toHaveText(
      'Deleted 1 transaction · Undo u',
    )
    await page.keyboard.press('w')
    const review = page.getByRole('dialog', { name: 'Review pending changes' })
    await expect(review).toContainText('Delete transaction')
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  } finally {
    await server.stop()
  }
})

/** The keys the status line hints at, in order. */
async function hintKeys(page: Page): Promise<string[]> {
  return await page
    .locator('.mf-status-line__hint kbd')
    .evaluateAll((keys) => keys.map((key) => key.getAttribute('aria-label') ?? ''))
}

test('the selection bar counts two merchants, explains Delete, and Clear empties it', async ({
  page,
}) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    const grid = page.getByRole('grid', { name: 'Financial results' })
    const bar = page.getByRole('region', { name: 'Selection' })
    await expect(bar).toBeHidden()
    // Maple Court Apartments (34 transactions, -73,100.00) and Corner Grocery (130, -12,611.08).
    await page.keyboard.press('Space')
    await expect(page.getByRole('row', { selected: true })).toHaveCount(1)
    await page.keyboard.press('j')
    await page.keyboard.press('Space')
    await expect(page.getByRole('row', { selected: true })).toHaveCount(2)
    await expect(bar).toContainText('2 merchants · 164 transactions · -85,711.08')
    await expect.poll(() => hintKeys(page)).toEqual(['m', 'c', 'h', 'x', 'Esc'])

    const reason = 'Open the group to delete its transactions.'
    const remove = bar.getByRole('button', { name: 'Delete' })
    await expect(remove).toBeDisabled()
    await expect(remove).not.toHaveAttribute('title')
    const deleteButton = page.getByRole('button', { name: 'Delete' })
    await bar.locator('.kit-tooltip-trigger').filter({ has: deleteButton }).hover()
    await expect(page.getByRole('tooltip')).toHaveText(reason)

    await bar.getByRole('button', { name: 'Merchant' }).click()
    const merchant = page.getByRole('dialog', { name: 'Edit merchant' })
    await expect(merchant).toContainText('164 transactions affected')
    await page.keyboard.press('Escape')
    await expect(merchant).toBeHidden()
    await expect(grid).toBeFocused()

    await bar.getByRole('button', { name: 'Clear' }).click()
    await expect(page.getByRole('row', { selected: true })).toHaveCount(0)
    await expect(bar).toBeHidden()
    await expect(grid).toBeFocused()
    await expect(page.getByRole('columnheader', { name: 'Merchant' })).toBeVisible()
  } finally {
    await server.stop()
  }
})

test('the selection bar edits selected transactions as their keys do', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    const grid = page.getByRole('grid', { name: 'Financial results' })
    await page.keyboard.press('d')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    await page.keyboard.press('Space')
    await expect(page.getByRole('row', { selected: true })).toHaveCount(1)
    await page.keyboard.press('j')
    await page.keyboard.press('Space')
    await expect(page.getByRole('row', { selected: true })).toHaveCount(2)
    const bar = page.getByRole('region', { name: 'Selection' })
    await expect(bar).toContainText(/^2 transactions · [+-][\d,]+\.\d\d/)

    await bar.getByRole('button', { name: 'Delete' }).click()
    const confirmation = page.getByRole('dialog', { name: 'Confirm deletion' })
    await expect(confirmation).toContainText('Delete 2 transactions?')
    await page.keyboard.press('Escape')
    await expect(grid).toBeFocused()

    await bar.getByRole('button', { name: 'Hide' }).click()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await expect(grid).toBeFocused()
  } finally {
    await server.stop()
  }
})

test('Clear goes back once per click and works again after a failed request', async ({ page }) => {
  const server = await startE2EServer()
  try {
    const backs: string[] = []
    await page.route('**/view/transition', async (route) => {
      if (route.request().postDataJSON().action !== 'view.back') return route.continue()
      backs.push('view.back')
      if (backs.length === 1) return route.abort()
      await new Promise((resolve) => setTimeout(resolve, 300))
      await route.continue()
    })
    await openMoneyflow(page, server)
    await page.keyboard.press('Enter')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    await page.keyboard.press('Space')
    const bar = page.getByRole('region', { name: 'Selection' })
    const clear = bar.getByRole('button', { name: 'Clear' })

    await clear.click()
    await expect.poll(() => backs.length).toBe(1)
    await expect(bar).toBeVisible()

    // A double click while the request runs sends one back, which only clears the selection.
    await clear.dblclick()
    await expect(bar).toBeHidden()
    expect(backs).toHaveLength(2)
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
  } finally {
    await server.stop()
  }
})

test.describe('on a touch phone', () => {
  test.use({ hasTouch: true, viewport: { width: 390, height: 844 } })

  test('a tap on the disabled Delete shows why', async ({ page }) => {
    const server = await startE2EServer()
    try {
      await openMoneyflow(page, server)
      await cell(page.locator('[role="row"][aria-rowindex="2"]'), 'select').tap()
      const bar = page.getByRole('region', { name: 'Selection' })
      await expect(bar).toContainText('1 merchant · ')
      const remove = page.getByRole('button', { name: 'Delete' })
      await bar.locator('.kit-tooltip-trigger').filter({ has: remove }).tap()
      await expect(page.getByRole('tooltip')).toHaveText(
        'Open the group to delete its transactions.',
      )
    } finally {
      await server.stop()
    }
  })
})
