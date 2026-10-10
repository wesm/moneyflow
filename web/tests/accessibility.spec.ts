import AxeBuilder from '@axe-core/playwright'

import { activeRow, cell, expect, openMoneyflow, test } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

// kit CollapsibleSidebar renders its resize separator between the sidebar <aside> and the main
// <section>, outside every landmark. Moneyflow cannot move it without patching kit-ui (upstream gap).
const kitResizeHandle = 'kit-split-resize-handle'

async function expectNoAxeViolations(page: import('@playwright/test').Page): Promise<void> {
  const result = await new AxeBuilder({ page }).analyze()
  const violations = result.violations
    .map((violation) =>
      violation.id === 'region'
        ? {
            ...violation,
            nodes: violation.nodes.filter((node) => !node.html.includes(kitResizeHandle)),
          }
        : violation,
    )
    .filter((violation) => violation.nodes.length > 0)
  expect(violations).toEqual([])
}

for (const colorScheme of ['light', 'dark'] as const) {
  test(`initial ${colorScheme} view has no automated accessibility violations`, async ({
    page,
    server,
  }) => {
    await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), colorScheme)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await openMoneyflow(page, server)
    // The Income tile shows once the insights entry has loaded, so the charts have drawn.
    await expect(page.locator('.mf-kpis').getByText('Income', { exact: true })).toBeVisible()
    await expect(page.getByRole('region', { name: 'Charts' })).toBeVisible()
    await expectNoAxeViolations(page)

    const grid = page.getByRole('grid', { name: 'Financial results' })
    await expect(grid).toHaveAttribute('aria-activedescendant', /moneyflow-row-/)
    await page.keyboard.press('Space')
    await expect(await activeRow(page)).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('columnheader', { name: 'Net' })).toHaveAttribute(
      'aria-sort',
      'descending',
    )
    await expect(page.locator('[aria-live="polite"]')).not.toHaveCount(0)
  })
}

for (const colorScheme of ['light', 'dark'] as const) {
  test(`${colorScheme} rising and falling changes pass axe on cursor and selected rows`, async ({
    page,
    server,
  }) => {
    await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), colorScheme)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await openMoneyflow(
      page,
      server,
      'cur=USD%3A2&from=2026-09-01&group=category&to=2026-09-30&v=1',
    )
    await expect(page.locator('.mf-kpis__caption')).toHaveText('vs Aug')
    const grid = page.getByRole('grid', { name: 'Financial results' })
    await expect(grid.locator('.mf-tone--favorable').first()).toBeVisible()
    await expect(grid.locator('.mf-tone--unfavorable').first()).toBeVisible()
    const falling = grid.getByRole('row').filter({ has: page.locator('.mf-tone--unfavorable') })
    await cell(falling.first(), 'vs_prior').click()
    await expect((await activeRow(page)).locator('.mf-tone--unfavorable')).toBeVisible()
    await expectNoAxeViolations(page)

    await page.keyboard.press('Space')
    await expect(await activeRow(page)).toHaveAttribute('aria-selected', 'true')
    await expectNoAxeViolations(page)
  })
}

for (const colorScheme of ['light', 'dark'] as const) {
  test(`${colorScheme} selected rows, the selection bar, and a hovered pencil pass axe`, async ({
    page,
    server,
  }) => {
    await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), colorScheme)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await openMoneyflow(page, server)
    const grid = page.getByRole('grid', { name: 'Financial results' })
    await cell(grid.locator('[role="row"][aria-rowindex="3"]'), 'select').click()
    await expect(page.getByRole('row', { selected: true })).toHaveCount(1)
    const bar = page.getByRole('region', { name: 'Selection' })
    await expect(bar).toContainText('1 merchant · ')
    await expectNoAxeViolations(page)

    await grid.focus()
    await page.keyboard.press('d')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    await page.keyboard.press('Space')
    const hovered = grid.locator('[role="row"][aria-rowindex="4"]')
    await hovered.hover()
    await expect(
      cell(hovered, 'merchant').getByRole('button', { name: 'Edit merchant' }),
    ).toBeVisible()
    await expect(bar).toContainText('1 transaction · ')
    await expectNoAxeViolations(page)
  })
}

for (const colorScheme of ['light', 'dark'] as const) {
  test(`the ${colorScheme} merchant editor with its list open passes axe`, async ({
    page,
    server,
  }) => {
    await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), colorScheme)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await openMoneyflow(page, server)
    await page.keyboard.press('m')
    const editor = page.getByRole('dialog', { name: 'Edit merchant' })
    await expect(editor.getByRole('button', { name: /^Stage change to / })).toBeVisible()
    await editor.getByRole('combobox', { name: 'Merchant' }).pressSequentially('Co')
    const list = editor.getByRole('listbox', { name: 'Merchant' })
    await page.keyboard.press('ArrowDown')
    await expect(list.getByRole('option').nth(1)).toHaveAttribute('aria-selected', 'true')
    await list.getByRole('option').nth(3).hover()
    await expectNoAxeViolations(page)
  })
}

for (const colorScheme of ['light', 'dark'] as const) {
  test(`the ${colorScheme} icon rail view has no automated accessibility violations`, async ({
    page,
    server,
  }) => {
    await page.setViewportSize({ width: 1024, height: 768 })
    await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), colorScheme)
    await openMoneyflow(page, server)
    await expect(page.getByRole('navigation', { name: 'Workspace' })).toBeVisible()
    await expectNoAxeViolations(page)
  })
}

test('help overlay traps focus, passes axe, and restores the grid', async ({ page, server }) => {
  await page.addInitScript(() => localStorage.setItem('kit-ui-theme', 'dark'))
  await openMoneyflow(page, server)
  await page.keyboard.press('?')
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expectNoAxeViolations(page)
  await page.keyboard.press('Tab')
  expect(await dialog.evaluate((node) => node.contains(document.activeElement))).toBe(true)
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
})

for (const colorScheme of ['light', 'dark'] as const) {
  test(`${colorScheme} command bar controls pass axe while open`, async ({ page, server }) => {
    await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), colorScheme)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await openMoneyflow(page, server)
    await page.getByRole('grid', { name: 'Financial results' }).focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('radiogroup', { name: 'Break down by' })).toBeVisible()
    await page.keyboard.press('f')
    await expect(page.getByRole('button', { name: 'Show transfers' })).toBeVisible()
    await expectNoAxeViolations(page)
    await page.keyboard.press('Escape')

    await page.getByRole('combobox', { name: /^Currency/ }).click()
    await expect(page.getByRole('listbox')).toBeVisible()
    await expectNoAxeViolations(page)
    await page.keyboard.press('Escape')

    await page.keyboard.press('/')
    await page.getByRole('searchbox', { name: 'Search transactions' }).fill('[')
    await expect(page.getByText('That search expression is invalid.')).toBeVisible()
    await expectNoAxeViolations(page)
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  })
}

for (const colorScheme of ['light', 'dark'] as const) {
  test(`${colorScheme} phone drawer, sheets, compact rows, and search pass axe`, async ({
    page,
    server,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), colorScheme)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await openMoneyflow(page, server)
    await page.getByRole('button', { name: 'Open navigation' }).click()
    await expect(page.getByRole('dialog', { name: 'Navigation' })).toBeVisible()
    await expectNoAxeViolations(page)
    await page.keyboard.press('Escape')

    await page.getByRole('grid', { name: 'Financial results' }).focus()
    await page.keyboard.press('Enter')
    await page.getByRole('button', { name: 'View', exact: true }).click()
    const sheet = page.getByRole('dialog', { name: 'View' })
    await expect(sheet.getByRole('radiogroup', { name: 'Break down by' })).toBeVisible()
    await expectNoAxeViolations(page)
    await sheet.getByRole('button', { name: 'Filters' }).click()
    await expect(page.getByRole('button', { name: 'Show transfers' })).toBeVisible()
    await expectNoAxeViolations(page)
    await page.keyboard.press('Escape')
    await page.keyboard.press('Escape')
    await expect(sheet).toBeHidden()

    // Compact transaction rows with a selected row, then the row actions sheet over them.
    const grid = page.getByRole('grid', { name: 'Financial results' })
    await cell(grid.locator('[role="row"][aria-rowindex="2"]'), 'select').click()
    await expect(grid.getByRole('row', { selected: true })).toHaveCount(1)
    await expect(grid.locator('.mf-compact').first()).toBeVisible()
    await expectNoAxeViolations(page)
    await grid
      .getByRole('button', { name: /^Actions for / })
      .first()
      .click()
    await expect(page.getByRole('button', { name: 'Info', exact: true })).toBeVisible()
    await expectNoAxeViolations(page)
    await page.keyboard.press('Escape')

    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page.getByRole('searchbox', { name: 'Search transactions' })).toBeFocused()
    await expectNoAxeViolations(page)
  })
}

test('duplicate review and deletion confirmation pass axe and trap focus', async ({ page }) => {
  const server = await startE2EServer({
    fixturePath: 'testdata/fixtures/duplicate_transactions.json',
  })
  try {
    await page.addInitScript(() => localStorage.setItem('kit-ui-theme', 'dark'))
    await openMoneyflow(page, server)
    await page.keyboard.press('Shift+D')
    const review = page.getByRole('dialog', { name: 'Duplicate transactions' })
    await expect(review).toBeVisible()
    await expectNoAxeViolations(page)
    await page.keyboard.press('Tab')
    expect(await review.evaluate((node) => node.contains(document.activeElement))).toBe(true)
    await page.keyboard.press('x')
    const confirmation = page.getByRole('dialog', { name: 'Confirm deletion' })
    await expect(confirmation).toBeVisible()
    await expectNoAxeViolations(page)
    await page.keyboard.press('Escape')
    await expect(review).toBeVisible()
  } finally {
    await server.stop()
  }
})

for (const colorScheme of ['light', 'dark'] as const) {
  test(`${colorScheme} edit lock banner and toast pass axe`, async ({ page }) => {
    const server = await startE2EServer()
    await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), colorScheme)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.route('**/provider/write-status', (route) =>
      route.fulfill({
        json: {
          version: '1',
          revision: '1',
          generation: '1',
          phase: 'writing',
          total: 4,
          completed: 1,
          failed: 0,
          remaining: 3,
          overrides: 0,
          actions: ['pause'],
        },
      }),
    )
    try {
      await openMoneyflow(page, server)
      await expect(page.getByText(/Editing is locked until the write finishes/)).toBeVisible()
      await page.keyboard.press('Enter')
      await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
      await page.keyboard.press('h')
      await expect(page.getByRole('status').filter({ hasText: /^Hidden / })).toBeVisible()
      await expectNoAxeViolations(page)
    } finally {
      await server.stop()
    }
  })
}
