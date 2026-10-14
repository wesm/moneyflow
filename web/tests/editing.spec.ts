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
    const affected = merchant.getByRole('list', { name: 'Affected transactions' })
    await expect(affected.getByRole('listitem')).toHaveCount(1)
    await expect(
      merchant.getByRole('button', { name: 'Stage change to 1 transaction' }),
    ).toBeVisible()
    const merchantName = merchant.getByRole('combobox', { name: 'Merchant' })
    await expect(merchantName).toBeFocused()
    await merchantName.fill('Browser Merchant')
    await expect(merchant.getByRole('option', { name: 'Create "Browser Merchant"' })).toBeVisible()
    await merchantName.press('Enter')
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
    const toast = await page
      .getByRole('status')
      .filter({ hasText: /^Hidden 1 transaction/ })
      .boundingBox()
    const bar = await page.getByRole('banner').boundingBox()
    expect(toast!.y).toBeGreaterThanOrEqual(bar!.y + bar!.height)
    await expect((await activeRow(page)).getByLabel('Pending edits')).toBeVisible()
    await page.keyboard.press('u')
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await expect(page.getByText(/1 redo/)).toBeVisible()
    await expect(page.getByRole('status').filter({ hasText: /^Undid / })).toHaveText(
      'Undid hide · 1 transaction · Redo U',
    )
    await page.keyboard.press('Shift+U')
    await expect(page.getByText(/2 pending edit/)).toBeVisible()
    await expect(page.getByRole('status').filter({ hasText: /^Redid / })).toHaveText(
      'Redid hide · 1 transaction · Undo u',
    )
    await page.keyboard.press('w')
    await expect(page.getByRole('dialog', { name: 'Review pending changes' })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  } finally {
    await server.stop()
  }
})

/**
 * Holds the next request to `path` in the browser until `release` is called, so a test can press
 * keys while that request is on its way. `sent` resolves once the browser has made the request.
 * The request goes on unchanged: a refetch would lose Chromium's Sec-Fetch-Site header.
 */
async function holdRequest(
  page: Page,
  path: string,
): Promise<{ sent: Promise<void>; release(): void }> {
  let release!: () => void
  const released = new Promise<void>((resolve) => (release = resolve))
  let markSent!: () => void
  const sent = new Promise<void>((resolve) => (markSent = resolve))
  await page.route(
    `**/api/v1/profiles/*/${path}`,
    async (route) => {
      markSent()
      await released
      await route.continue()
    },
    { times: 1 },
  )
  return { sent, release }
}

test('an undo pressed while its edit is on its way runs once the edit lands', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    await page.keyboard.press('Enter')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    const hide = await holdRequest(page, 'mutations')
    const undo = page.waitForRequest('**/api/v1/profiles/*/undo')
    await page.keyboard.press('h')
    await hide.sent
    // Nothing was pending when u was pressed; the hide still on its way makes undo available.
    await page.keyboard.press('u')
    hide.release()
    await undo
    await expect(page.getByRole('status').filter({ hasText: /^Undid / })).toHaveText(
      'Undid hide · 1 transaction · Redo U',
    )
    await expect(page.getByText(/1 redo/)).toBeVisible()
  } finally {
    await server.stop()
  }
})

test('a redo pressed while its undo is on its way runs once the undo lands', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    await page.keyboard.press('Enter')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    await page.keyboard.press('h')
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    const undone = await holdRequest(page, 'undo')
    const redo = page.waitForRequest('**/api/v1/profiles/*/redo')
    await page.keyboard.press('u')
    await undone.sent
    // The browser still shows the hide, with nothing to redo, when U is pressed.
    await page.keyboard.press('Shift+U')
    undone.release()
    await redo
    await expect(page.getByRole('status').filter({ hasText: /^Redid / })).toHaveText(
      'Redid hide · 1 transaction · Undo u',
    )
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
  } finally {
    await server.stop()
  }
})

test('u and U with nothing to undo or redo say so', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    await page.keyboard.press('u')
    await expect(page.getByText('No pending edit to undo.')).toBeVisible()
    await page.keyboard.press('Shift+U')
    await expect(page.getByText('No undone edit to redo.')).toBeVisible()
    await expect(page.getByText('No pending edit to undo.')).toHaveCount(0)
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

    const row = await activeRow(page)
    const rowBox = (await row.boundingBox())!
    await page.keyboard.press('m')
    const editor = page.getByRole('dialog', { name: 'Edit merchant' })
    const editorBox = (await editor.boundingBox())!
    expect(editorBox.y).toBeGreaterThanOrEqual(rowBox.y + rowBox.height)
    const name = editor.getByRole('combobox', { name: 'Merchant' })
    await expect(name).toBeFocused()
    await name.fill('Renamed Example')
    await expect(editor.getByRole('option', { name: 'Create "Renamed Example"' })).toBeVisible()
    await name.press('Enter')
    const stage = editor.getByRole('button', { name: 'Stage change to 1 transaction' })
    await expect(stage).toBeEnabled()
    await name.press('Enter')
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await expect(grid).toBeFocused()
    await expect(await activeRow(page)).toHaveAttribute('aria-rowindex', '2')
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
    // The cursor stays on the edited row, so j continues the flow.
    await page.keyboard.press('j')
    await expect(await activeRow(page)).toHaveAttribute('aria-rowindex', '3')

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
    const name = merchant.getByRole('combobox', { name: 'Merchant' })
    await name.fill('Pointer Merchant')
    await expect(merchant.getByRole('option', { name: 'Create "Pointer Merchant"' })).toBeVisible()
    await name.press('Enter')
    await expect(
      merchant.getByRole('button', { name: 'Stage change to 1 transaction' }),
    ).toBeEnabled()
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

test('a bulk rename keeps whole-merchant scope off and clears the selection', async ({ page }) => {
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
    const bar = page.getByRole('region', { name: 'Selection' })
    await page.keyboard.press('m')
    const editor = page.getByRole('dialog', { name: 'Edit merchant' })
    await expect(editor).toContainText('· 2 selected')
    await expect(editor.getByRole('radio', { name: 'Whole merchant' })).toBeDisabled()
    await expect(editor).toContainText('Clear the selection to rename the whole merchant')
    await expect(
      editor.getByRole('radio', { name: `${affected} selected transactions` }),
    ).toBeChecked()
    // The editor grows when its preview arrives, then repositions on the next frame.
    const barTop = (await bar.boundingBox())!.y
    await expect
      .poll(async () => {
        const box = (await editor.boundingBox())!
        return box.y + box.height
      })
      .toBeLessThanOrEqual(barTop)
    const name = editor.getByRole('combobox', { name: 'Merchant' })
    await name.fill('Normalized Merchant')
    await expect(editor.getByRole('option', { name: 'Create "Normalized Merchant"' })).toBeVisible()
    await name.press('Enter')
    await expect(
      editor.getByRole('button', { name: `Stage change to ${affected} transactions` }),
    ).toBeEnabled()
    await name.press('Enter')
    await expect(editor).toBeHidden()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    await expect(page.getByText(new RegExp(`\\b${affected} transactions\\b`))).toBeVisible()
    await expect(page.getByRole('row', { selected: true })).toHaveCount(0)
  } finally {
    await server.stop()
  }
})

test('a bulk edit still clears the selection after a sort during its request', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    await page.keyboard.press('d')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    await page.keyboard.press('Space')
    await page.keyboard.press('j')
    await page.keyboard.press('Space')
    await expect(page.getByRole('row', { selected: true })).toHaveCount(2)
    let release!: () => void
    const held = new Promise<void>((resolve) => (release = resolve))
    await page.route(`${server.profileAPI}mutations`, async (route) => {
      await held
      await route.continue()
    })
    const sent = page.waitForRequest(`${server.profileAPI}mutations`)
    await page.keyboard.press('c')
    const editor = page.getByRole('dialog', { name: 'Change category' })
    const field = editor.getByRole('combobox', { name: 'Category' })
    await field.fill('Uncategorized')
    await field.press('Enter')
    await expect(editor.getByRole('button', { name: /^Stage change to / })).toBeEnabled()
    await field.press('Enter')
    await sent
    await page.keyboard.press('Escape')
    await expect(editor).toBeHidden()
    const unsorted = page.url()
    await page.keyboard.press('s')
    await expect(page).not.toHaveURL(unsorted)
    // The sort moves the selected rows out of view, so the selection bar counts them.
    const bar = page.getByRole('region', { name: 'Selection' })
    await expect(bar).toContainText(/^2 transactions/)

    release()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await expect(bar).toBeHidden()
  } finally {
    await server.stop()
  }
})

test('a merchant row renames the whole merchant through review and commit', async ({ page }) => {
  const server = await startE2EServer()
  try {
    // September 2026: Maple Court Apartments has one rent payment in view and 33 outside it.
    await openMoneyflow(page, server, 'cur=USD%3A2&from=2026-09-01&to=2026-09-30&v=1')
    const grid = page.getByRole('grid', { name: 'Financial results' })
    await expect(cell(await activeRow(page), 'name')).toHaveText('Maple Court Apartments')
    await page.keyboard.press('m')
    const editor = page.getByRole('dialog', { name: 'Edit merchant' })
    const whole = editor.getByRole('radio', { name: 'Whole merchant · 34 transactions' })
    await expect(whole).toBeEnabled()
    await page.keyboard.press('Tab')
    await expect(editor.getByRole('radio', { name: '1 transaction' })).toBeFocused()
    await page.keyboard.press('ArrowRight')
    await expect(whole).toBeChecked()
    await expect(editor).toContainText('Changes all 34 transactions of this merchant.')
    await expect(editor).toContainText(
      '33 of these are outside the current view (other periods or filters).',
    )
    const name = editor.getByRole('combobox', { name: 'Merchant' })
    await name.fill('Maple Court Homes')
    await expect(editor.getByRole('option', { name: 'Create "Maple Court Homes"' })).toBeVisible()
    await name.press('Enter')
    const stage = editor.getByRole('button', { name: 'Stage change to 34 transactions' })
    await expect(stage).toBeEnabled()
    await stage.click()
    await expect(editor).toBeHidden()
    await expect(grid).toBeFocused()
    await expect(page.getByText(/1 pending edit · 34 transactions/)).toBeVisible()

    await page.keyboard.press('w')
    const review = page.getByRole('dialog', { name: 'Review pending changes' })
    await expect(
      review.getByRole('button', { name: /^Rename merchant 34 transactions / }),
    ).toBeVisible()
    await review.getByRole('button', { name: 'Commit reviewed changes' }).click()
    await expect(page.getByText(/0 pending edit/)).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(cell(await activeRow(page), 'name')).toHaveText('Maple Court Homes')
  } finally {
    await server.stop()
  }
})

test('a new category asks for its group, and Esc there returns to the field', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    await page.keyboard.press('Enter')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    await page.keyboard.press('c')
    const editor = page.getByRole('dialog', { name: 'Change category' })
    await expect(editor.getByRole('radiogroup')).toHaveCount(0)
    const field = editor.getByRole('combobox', { name: 'Category' })
    await field.pressSequentially('Pet Supplies')
    await expect(editor.getByRole('option', { name: 'Create "Pet Supplies"' })).toBeVisible()
    await field.press('Enter')
    const group = editor.getByRole('combobox', { name: /^Category group:/ })
    await expect(group).toBeFocused()

    await page.keyboard.press('Escape')
    await expect(group).toHaveCount(0)
    await expect(editor).toBeVisible()
    await expect(field).toBeFocused()
    await expect(field).toHaveValue('Pet Supplies')

    await field.press('ArrowDown')
    await field.press('Enter')
    await expect(group).toBeFocused()
    await page.keyboard.press('Enter')
    await page.keyboard.press('ArrowDown')
    await page.keyboard.press('Enter')
    await expect(group).toHaveAccessibleName('Category group: Food')
    await expect(editor).toBeVisible()
    await page.keyboard.press('ControlOrMeta+Enter')
    await expect(editor).toBeHidden()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await expect(cell(await activeRow(page), 'category').locator('.mf-staged')).toContainText(
      '→ Pet Supplies',
    )
  } finally {
    await server.stop()
  }
})

test('Enter accepts then stages, and Esc closes the list then cancels', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    const grid = page.getByRole('grid', { name: 'Financial results' })
    await page.keyboard.press('m')
    const editor = page.getByRole('dialog', { name: 'Edit merchant' })
    const field = editor.getByRole('combobox', { name: 'Merchant' })
    const list = editor.getByRole('listbox', { name: 'Merchant' })
    await field.pressSequentially('Corner')
    await expect(list).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(list).toBeHidden()
    await expect(field).toHaveValue('Corner')
    await expect(editor).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(editor).toBeHidden()
    await expect(grid).toBeFocused()
    await expect(page.getByText(/0 pending edit/)).toBeVisible()

    await page.keyboard.press('m')
    await field.pressSequentially('Corner Gro')
    const first = list.getByRole('option').first()
    await expect(first).toHaveAccessibleName(/^Corner Grocery, /)
    await expect(first).toHaveAttribute('aria-selected', 'true')
    await page.keyboard.press('Enter')
    await expect(list).toBeHidden()
    await expect(field).toHaveValue('Corner Grocery')
    await expect(editor).toContainText('Merges into Corner Grocery')
    await expect(page.getByText(/0 pending edit/)).toBeVisible()
    await page.keyboard.press('Enter')
    await expect(editor).toBeHidden()
    await expect(grid).toBeFocused()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
  } finally {
    await server.stop()
  }
})

test('a held Enter accepts the suggestion but does not stage it', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    await page.keyboard.press('Enter')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    await page.keyboard.press('m')
    const editor = page.getByRole('dialog', { name: 'Edit merchant' })
    const field = editor.getByRole('combobox', { name: 'Merchant' })
    await field.fill('Browser Merchant')
    await expect(editor.getByRole('option', { name: 'Create "Browser Merchant"' })).toBeVisible()
    await page.keyboard.down('Enter')
    await page.keyboard.down('Enter')
    await page.keyboard.up('Enter')
    await expect(field).toHaveValue('Browser Merchant')
    await expect(editor.getByRole('listbox', { name: 'Merchant' })).toBeHidden()
    await expect(editor).toBeVisible()
    await expect(page.getByText(/0 pending edit/)).toBeVisible()
  } finally {
    await server.stop()
  }
})

test('x confirms inline below the row, Enter stages once, and review keeps commit explicit', async ({
  page,
}) => {
  const server = await startE2EServer()
  try {
    // Each delete stays in flight long enough for a second Enter to arrive.
    const deletes: string[] = []
    await page.route('**/mutations', async (route) => {
      const action = route.request().postDataJSON().action
      if (action === 'transaction.delete') deletes.push(action)
      await new Promise((resolve) => setTimeout(resolve, 300))
      await route.continue()
    })
    await openMoneyflow(page, server)
    const grid = page.getByRole('grid', { name: 'Financial results' })
    await page.keyboard.press('d')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    const row = await activeRow(page)
    const merchant = await cell(row, 'merchant').innerText()
    await page.keyboard.press('x')
    const confirmation = page.getByRole('alertdialog', { name: 'Confirm deletion' })
    await expect(confirmation).toHaveAccessibleDescription('Delete 1 transaction?')
    await expect(confirmation).not.toContainText(merchant)
    const hints = confirmation.locator('kbd')
    expect(await hints.evaluateAll((keys) => keys.map((key) => key.ariaLabel))).toEqual([
      'Enter',
      'Esc',
    ])
    // Anchored just below the cursor row, not a centered modal.
    const rowBox = (await row.boundingBox())!
    const box = (await confirmation.boundingBox())!
    expect(box.y).toBeGreaterThanOrEqual(rowBox.y + rowBox.height)
    expect(box.y).toBeLessThan(rowBox.y + rowBox.height + 16)
    await page.keyboard.press('Escape')
    await expect(confirmation).toBeHidden()
    await expect(grid).toBeFocused()
    await expect(page.getByText(/0 pending edit/)).toBeVisible()

    // Two Enters while the first delete is in flight stage it once.
    await page.keyboard.press('x')
    await expect(confirmation).toBeVisible()
    await page.keyboard.press('Enter')
    await page.keyboard.press('Enter')
    await expect(confirmation).toBeHidden()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await expect(page.getByRole('status').filter({ hasText: /^Deleted / })).toHaveText(
      'Deleted 1 transaction · Undo u',
    )
    expect(deletes).toHaveLength(1)
    await expect(grid).toBeFocused()
    await expect(row).toHaveClass(/mf-table__row--deleted/)

    // A held Enter's repeats stage nothing more.
    await page.keyboard.press('j')
    await page.keyboard.press('x')
    await expect(confirmation).toBeVisible()
    await page.keyboard.down('Enter')
    await page.keyboard.down('Enter')
    await page.keyboard.down('Enter')
    await expect(page.getByText(/2 pending edits/)).toBeVisible()
    await expect(confirmation).toBeHidden()
    // A repeat that reaches the grid after the confirmation closes does not open the row.
    await page.keyboard.down('Enter')
    await page.keyboard.up('Enter')
    await expect(grid).toBeFocused()
    await expect(page.getByRole('dialog', { name: 'Transaction information' })).toHaveCount(0)
    expect(deletes).toHaveLength(2)

    await page.keyboard.press('w')
    const review = page.getByRole('dialog', { name: 'Review pending changes' })
    await expect(review).toContainText('Delete transaction')
    await page.keyboard.press('Escape')
    await expect(grid).toBeFocused()
  } finally {
    await server.stop()
  }
})

test('a banner locks editing while a provider write runs and opens write status', async ({
  page,
}) => {
  const server = await startE2EServer()
  try {
    let phase: string | undefined = 'writing'
    await page.route('**/provider/write-status', (route) =>
      route.fulfill({
        json: {
          version: '1',
          revision: '1',
          generation: '1',
          total: 4,
          completed: 1,
          failed: 0,
          remaining: 3,
          overrides: 0,
          actions: ['pause'],
          ...(phase ? { phase } : {}),
        },
      }),
    )
    await openMoneyflow(page, server)
    const banner = page.getByText('Writing to Monarch · Editing is locked until the write finishes')
    await expect(banner).toBeVisible()
    const box = await banner.boundingBox()
    const kpi = await page.locator('.mf-chart-area').boundingBox()
    expect(box!.y + box!.height).toBeLessThanOrEqual(kpi!.y)

    await page.getByRole('button', { name: /^Status\s*w$/ }).click()
    await expect(page.getByRole('dialog', { name: 'Monarch write status' })).toBeVisible()
    await page.keyboard.press('Escape')

    phase = undefined
    await expect(banner).toBeHidden({ timeout: 5_000 })
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
    await expect(
      merchant.getByRole('button', { name: 'Stage change to 164 transactions' }),
    ).toBeVisible()
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
    const confirmation = page.getByRole('alertdialog', { name: 'Confirm deletion' })
    await expect(confirmation).toHaveAccessibleDescription('Delete 2 transactions?')
    // Anchored just above the selection bar.
    const barBox = (await bar.boundingBox())!
    const box = (await confirmation.boundingBox())!
    expect(box.y + box.height).toBeLessThanOrEqual(barBox.y)
    expect(box.y + box.height).toBeGreaterThan(barBox.y - 16)
    await page.keyboard.press('Escape')
    await expect(confirmation).toBeHidden()
    await expect(grid).toBeFocused()

    await bar.getByRole('button', { name: 'Delete' }).click()
    await confirmation.getByRole('button', { name: 'Delete' }).click()
    await expect(confirmation).toBeHidden()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await expect(page.getByRole('status').filter({ hasText: /^Deleted / })).toHaveText(
      'Deleted 2 transactions · Undo u',
    )
    await expect(grid).toBeFocused()

    await page.keyboard.press('j')
    await page.keyboard.press('Space')
    await page.keyboard.press('j')
    await page.keyboard.press('Space')
    await expect(page.getByRole('row', { selected: true })).toHaveCount(2)
    await bar.getByRole('button', { name: 'Hide' }).click()
    await expect(page.getByText(/2 pending edits/)).toBeVisible()
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

test('Tab and Shift+Tab stay inside the anchored editor', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    await page.keyboard.press('Enter')
    await page.keyboard.press('m')
    const editor = page.getByRole('dialog', { name: 'Edit merchant' })
    await expect(editor.getByRole('combobox', { name: 'Merchant' })).toBeFocused()
    // WebKit's plain Tab skips buttons; the editor must move focus itself.
    const inside = () => editor.evaluate((node) => node.contains(document.activeElement))
    for (let press = 0; press < 8; press += 1) {
      await page.keyboard.press('Tab')
      expect(await inside()).toBe(true)
    }
    for (let press = 0; press < 8; press += 1) {
      await page.keyboard.press('Shift+Tab')
      expect(await inside()).toBe(true)
    }
  } finally {
    await server.stop()
  }
})

test('@smoke overlays opened by a click return focus to the control clicked', async ({ page }) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    const grid = page.getByRole('grid', { name: 'Financial results' })

    const sidebarItem = page.getByRole('button', { name: 'Edit categories', exact: true })
    await sidebarItem.click()
    await expect(page.getByRole('dialog', { name: 'Manage categories' })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog', { name: 'Manage categories' })).toBeHidden()
    await expect(sidebarItem).toBeFocused()

    await page.keyboard.press('h')
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    const review = page.getByRole('button', { name: 'Review pending changes' })
    await review.click()
    await expect(page.getByRole('dialog', { name: 'Review pending changes' })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog', { name: 'Review pending changes' })).toBeHidden()
    await expect(review).toBeFocused()
    await expect(grid).not.toBeFocused()
  } finally {
    await server.stop()
  }
})

test('@smoke the selection bar returns focus to the grid row, not to its button', async ({
  page,
}) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    const grid = page.getByRole('grid', { name: 'Financial results' })
    await page.keyboard.press('d')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    await page.keyboard.press('Space')
    await page.keyboard.press('j')
    await page.keyboard.press('Space')
    await expect(page.getByRole('row', { selected: true })).toHaveCount(2)

    const merchant = page
      .getByRole('region', { name: 'Selection' })
      .getByRole('button', { name: 'Merchant' })
    await merchant.click()
    const editor = page.getByRole('dialog', { name: 'Edit merchant' })
    await expect(editor).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(editor).toBeHidden()
    await expect(grid).toBeFocused()
    await expect(merchant).not.toBeFocused()
  } finally {
    await server.stop()
  }
})
