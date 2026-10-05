import { expect, test } from '@playwright/test'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { activeRow, openMoneyflow } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

test('pending edits and revision survive a real server restart on one explicit profile', async ({
  page,
}) => {
  const profileHome = await mkdtemp(join(tmpdir(), 'moneyflow-restart-profile-'))
  let first = await startE2EServer({ profileHome, seedProfile: true })
  try {
    await openMoneyflow(page, first)
    await page.keyboard.press('d')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    const grid = page.getByRole('grid', { name: 'Financial results' })
    const rowCount = Number(await grid.getAttribute('aria-rowcount'))
    expect(rowCount).toBeGreaterThan(1)
    const deletedRow = await activeRow(page)
    await page.keyboard.press('x')
    await page.keyboard.press('Enter')
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await expect(grid).toHaveAttribute('aria-rowcount', String(rowCount))
    await expect(deletedRow).toContainText('pending')
    await first.stop()

    const second = await startE2EServer({ profileHome })
    first = second
    await openMoneyflow(page, second)
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await expect(page.getByText('profile revision 2')).toBeVisible()
    await page.keyboard.press('d')
    await expect(grid).toHaveAttribute('aria-rowcount', String(rowCount))
    await expect(deletedRow).toContainText('pending')
    await page.keyboard.press('u')
    await expect(page.getByText(/0 pending/)).toBeVisible()
    await expect(grid).toHaveAttribute('aria-rowcount', String(rowCount))
    await expect(deletedRow).not.toContainText('pending')
    await page.keyboard.press('Shift+U')
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await expect(grid).toHaveAttribute('aria-rowcount', String(rowCount))
    await expect(deletedRow).toContainText('pending')

    await page.keyboard.press('w')
    const review = page.getByRole('dialog', { name: 'Review pending changes' })
    await expect(review).toContainText('Delete transaction')
    await review.getByRole('button', { name: 'Commit reviewed changes' }).focus()
    await page.keyboard.press('Enter')
    await expect(review).toBeHidden()
    await expect(page.getByText(/0 pending/)).toBeVisible()
    await expect(grid).toHaveAttribute('aria-rowcount', String(rowCount - 1))
    await expect(deletedRow).toHaveCount(0)
  } finally {
    await first.stop()
    await rm(profileHome, { recursive: true, force: true })
  }
})
