import AxeBuilder from '@axe-core/playwright'

import { activeRow, expect, openMoneyflow, test } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

async function expectNoAxeViolations(page: import('@playwright/test').Page): Promise<void> {
  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations).toEqual([])
}

for (const colorScheme of ['light', 'dark'] as const) {
  test(`initial ${colorScheme} view has no automated accessibility violations`, async ({
    page,
    server,
  }) => {
    await page.emulateMedia({ colorScheme, reducedMotion: 'reduce' })
    await openMoneyflow(page, server)
    await expectNoAxeViolations(page)

    const grid = page.getByRole('grid', { name: 'Financial results' })
    await expect(grid).toHaveAttribute('aria-activedescendant', /moneyflow-row-/)
    await page.keyboard.press('Space')
    await expect(await activeRow(page)).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('columnheader', { name: 'Amount' })).toHaveAttribute(
      'aria-sort',
      'descending',
    )
    await expect(page.locator('[aria-live="polite"]')).not.toHaveCount(0)

    const mark = page
      .getByRole('complementary', { name: 'Visualizations' })
      .getByRole('button')
      .first()
    await expect(mark).toHaveAccessibleName(/Example Housing, -1,200\.00/)
  })
}

for (const overlay of ['search', 'filters', 'help'] as const) {
  test(`${overlay} overlay traps focus, passes axe, and restores the grid`, async ({
    page,
    server,
  }) => {
    await openMoneyflow(page, server)
    await page.keyboard.press(overlay === 'search' ? '/' : overlay === 'filters' ? 'f' : '?')
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await expectNoAxeViolations(page)
    await page.keyboard.press('Tab')
    await expect(dialog).toContainText('')
    expect(await dialog.evaluate((node) => node.contains(document.activeElement))).toBe(true)
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  })
}

test('duplicate review and deletion confirmation pass axe and trap focus', async ({ page }) => {
  const server = await startE2EServer({
    fixturePath: 'testdata/fixtures/duplicate_transactions.json',
  })
  try {
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
