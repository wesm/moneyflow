import { expect, test } from '@playwright/test'

import { activeRow, openMoneyflow } from './fixtures'
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
    await expect(page.getByText(/1 pending/)).toBeVisible()

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
    await expect(page.getByText(/2 pending/)).toBeVisible()
    await expect(await activeRow(page)).toContainText('pending')
    await page.keyboard.press('u')
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await expect(page.getByText(/1 redo/)).toBeVisible()
    await page.keyboard.press('Shift+U')
    await expect(page.getByText(/2 pending/)).toBeVisible()
    await page.keyboard.press('w')
    await expect(page.getByRole('dialog', { name: 'Review pending changes' })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
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
    await page.keyboard.press('j')
    await page.keyboard.press('Space')
    await expect(await activeRow(page)).toHaveAttribute('aria-selected', 'true')
    await page.keyboard.press('j')
    await page.keyboard.press('Space')
    await expect(page.getByRole('row', { selected: true })).toHaveCount(2)
    await page.keyboard.press('m')
    await expect(page.getByRole('combobox', { name: /Selected transactions/ })).toBeVisible()
    await expect(page.getByRole('dialog', { name: 'Edit merchant' })).toContainText(
      '2 transactions affected',
    )
    const name = page.getByLabel('Merchant name')
    await name.fill('Normalized Merchant')
    await name.press('Enter')
    await expect(page.getByRole('dialog', { name: 'Edit merchant' })).toBeHidden()
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    await expect(page.getByText(/\b2 transactions\b/)).toBeVisible()
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
    const merchant = await (await activeRow(page)).getByRole('gridcell').nth(2).innerText()
    await page.keyboard.press('x')
    const confirmation = page.getByRole('dialog', { name: 'Confirm deletion' })
    await expect(confirmation).toContainText('Delete 1 transaction?')
    await expect(confirmation).not.toContainText(merchant)
    await page.keyboard.press('Escape')
    await expect(page.getByText(/0 pending/)).toBeVisible()

    await page.keyboard.press('x')
    await page.keyboard.press('Enter')
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await page.keyboard.press('w')
    const review = page.getByRole('dialog', { name: 'Review pending changes' })
    await expect(review).toContainText('Delete transaction')
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  } finally {
    await server.stop()
  }
})
