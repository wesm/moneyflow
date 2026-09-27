import { expect, test } from '@playwright/test'

import { startOnboardingE2EServer } from '../scripts/e2e-server'

const amazonCSV = `Order ID,Order Date,Product Name,Quantity,Total Owed,Order Status,Shipment Status,ASIN,Currency,Unit Price
order-example,2026-08-19,Example Product,1,12.34,Closed,Delivered,ASIN-EXAMPLE,USD,12.34
`

test('@smoke Amazon import preserves committed edits through reimport and reload', async ({
  page,
}) => {
  const server = await startOnboardingE2EServer()
  try {
    await page.goto(server.url)
    await page.keyboard.press('a')
    await page.keyboard.press('a')
    await page.getByLabel('Profile name').fill('Amazon Orders')
    await page.getByLabel('Profile name').press('Enter')

    await expect(page.getByRole('heading', { name: 'Confirm import settings' })).toBeVisible()
    await page.getByRole('button', { name: 'Continue' }).click()
    await page.getByLabel('Amazon CSV files').setInputFiles({
      name: 'Retail.OrderHistory.1.csv',
      mimeType: 'text/csv',
      buffer: Buffer.from(amazonCSV),
    })
    await page.getByRole('button', { name: 'Import' }).click()
    await expect(page.getByRole('heading', { name: 'Amazon import complete' })).toBeVisible()
    await page.getByRole('button', { name: 'Open profile' }).click()

    const grid = page.getByRole('grid', { name: 'Financial results' })
    await expect(grid).toBeFocused()
    await page.keyboard.press('d')
    await page.keyboard.press('i')
    await expect(page.getByRole('dialog', { name: 'Transaction information' })).toContainText(
      'Example Product',
    )
    await page.keyboard.press('Escape')

    await page.keyboard.press('m')
    const merchant = page.getByLabel('Merchant name')
    await merchant.fill('Edited purchase')
    await expect(page.getByRole('button', { name: 'Save pending change' })).toBeEnabled()
    await merchant.press('Enter')
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await page.keyboard.press('w')
    await page.getByRole('button', { name: 'Commit reviewed changes' }).click()
    await expect(page.getByText(/0 pending/)).toBeVisible()
    await expect(grid).toBeFocused()

    await page.keyboard.press('r')
    await expect(
      page.getByRole('heading', { name: 'Choose order-history CSV files' }),
    ).toBeVisible()
    await page.getByLabel('Amazon CSV files').setInputFiles({
      name: 'Retail.OrderHistory.1.csv',
      mimeType: 'text/csv',
      buffer: Buffer.from(amazonCSV),
    })
    await page.getByRole('button', { name: 'Import' }).click()
    await expect(page.getByRole('heading', { name: 'Amazon import complete' })).toBeVisible()
    await page.getByRole('button', { name: 'Open profile' }).click()
    await page.reload()
    await expect(grid).toBeFocused()
    await page.keyboard.press('d')
    await expect(grid.getByRole('row')).toHaveCount(2) // Header and one purchase, not a duplicate.
    await expect(grid).toContainText('Edited purchase')
    await expect(grid).toContainText('-12.34')
    await page.keyboard.press('i')
    await expect(page.getByRole('dialog', { name: 'Transaction information' })).toContainText(
      'Example Product',
    )
  } finally {
    await server.stop()
  }
})

test('invalid Amazon rows show actionable coordinates to the initiating tab', async ({ page }) => {
  const server = await startOnboardingE2EServer()
  try {
    await page.goto(server.url)
    await page.keyboard.press('a')
    await page.keyboard.press('a')
    await page.getByLabel('Profile name').fill('Invalid Import')
    await page.getByLabel('Profile name').press('Enter')
    await page.getByRole('button', { name: 'Continue' }).click()
    await page.getByLabel('Amazon CSV files').setInputFiles({
      name: 'Retail.OrderHistory.bad.csv',
      mimeType: 'text/csv',
      buffer: Buffer.from(amazonCSV.replace('12.34,Closed', 'not-money,Closed')),
    })
    await page.getByRole('button', { name: 'Import' }).click()
    await expect(page.getByRole('alert')).toContainText('record 1')
    await expect(page.getByRole('alert')).toContainText('Total Owed')
  } finally {
    await server.stop()
  }
})
