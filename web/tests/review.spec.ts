import { expect, test } from '@playwright/test'

import { openMoneyflow } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

test('review keeps redo separate, expands bounded targets, and commits its captured revision', async ({
  page,
}) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    await page.keyboard.press('h')
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await page.keyboard.press('u')
    await expect(page.getByText(/1 redo/)).toBeVisible()
    await page.keyboard.press('w')
    const review = page.getByRole('dialog', { name: 'Review pending changes' })
    await expect(review).toBeVisible()
    await expect(review.getByRole('heading', { name: 'Inactive redo operations' })).toBeVisible()
    await expect(review).toContainText('Commit permanently discards this redo history.')
    await page.keyboard.press('Escape')
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()

    await page.keyboard.press('Shift+U')
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await page.keyboard.press('w')
    await expect(review).toBeVisible()
    const operation = review.locator('.review-list button').first()
    await operation.focus()
    await expect(operation).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(review.locator('.review-paging')).toContainText('Showing 1–1 of 1')
    await expect(review.getByRole('button', { name: 'Next' })).toBeDisabled()
    await expect(operation).toHaveAttribute('aria-expanded', 'true')

    const commit = review.getByRole('button', { name: 'Commit reviewed changes' })
    await expect(commit).toBeEnabled()
    await commit.focus()
    await page.keyboard.press('Enter')
    await expect(review).toBeHidden()
    await expect(page.getByText(/0 pending/)).toBeVisible()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  } finally {
    await server.stop()
  }
})
