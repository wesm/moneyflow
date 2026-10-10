import { expect, test } from '@playwright/test'

import { activeAggregateCount, openMoneyflow } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

test('review keeps redo separate, expands bounded targets, and commits its captured revision', async ({
  page,
}) => {
  const server = await startE2EServer()
  try {
    await openMoneyflow(page, server)
    const affected = await activeAggregateCount(page, 2)
    const pageSize = 100
    await page.keyboard.press('h')
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
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
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    const commits: string[] = []
    page.on('request', (request) => {
      if (request.url().endsWith('/commit')) commits.push(request.url())
    })
    await page.keyboard.press('w')
    await expect(review).toBeVisible()
    const commit = review.getByRole('button', { name: 'Commit reviewed changes' })
    await expect(commit).toBeFocused()

    // Enter on a control with its own Enter does that action and does not commit.
    const operation = review.getByRole('button', { name: /^Toggle report visibility/ })
    await operation.focus()
    await page.keyboard.press('Enter')
    await expect(operation).toHaveAttribute('aria-expanded', 'true')
    await expect(review.getByRole('row')).toHaveCount(Math.min(affected, pageSize) + 1)
    await expect(
      review.getByText(`Showing 1–${Math.min(affected, pageSize)} of ${affected}`),
    ).toBeVisible()
    const next = review.getByRole('button', { name: 'Next' })
    if (affected <= pageSize) await expect(next).toBeDisabled()
    else await expect(next).toBeEnabled()
    await review.getByRole('button', { name: 'Close' }).focus()
    await page.keyboard.press('Enter')
    await expect(review).toBeHidden()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    expect(commits).toEqual([])

    // Enter on the focused Commit commits once: the browser does not also click it.
    await page.keyboard.press('w')
    await expect(commit).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(review).toBeHidden()
    await expect(page.getByText(/0 pending edit/)).toBeVisible()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    expect(commits).toHaveLength(1)
  } finally {
    await server.stop()
  }
})

test('an expanded operation shows each amount with its currency, right-aligned, at 390 px too', async ({
  page,
}) => {
  const server = await startE2EServer()
  try {
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 844 })
      await openMoneyflow(page, server)
      if (width === 1440) await page.keyboard.press('h')
      await expect(page.getByText(/1 pending edit/).first()).toBeAttached()
      await page.keyboard.press('w')
      const review = page.getByRole('dialog', { name: 'Review pending changes' })
      await review.getByRole('button', { name: /^Toggle report visibility/ }).click()
      const table = review.getByRole('table', { name: 'Affected transactions' })
      await expect(table.getByRole('columnheader')).toHaveText([
        'Date',
        'Merchant',
        'Category',
        'Amount',
      ])
      const amount = table.getByRole('row').nth(1).getByRole('cell').last()
      await expect(amount).toHaveText('-2,150.00 USD')
      await expect(amount).toHaveCSS('text-align', 'right')
      // The table and its rows fit the drawer without scrolling sideways.
      const overflow = await table.evaluate((node) => {
        const box = node.parentElement!.getBoundingClientRect()
        return Math.max(
          ...[node, ...node.querySelectorAll('tr')].map(
            (element) => element.getBoundingClientRect().right - box.right,
          ),
        )
      })
      expect(overflow, `${width} px`).toBeLessThanOrEqual(0)
      await page.keyboard.press('Escape')
    }
  } finally {
    await server.stop()
  }
})
