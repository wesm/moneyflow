import { expect, test } from '@playwright/test'

import { startOnboardingE2EServer } from '../scripts/e2e-server'

const amazonCSV = `Order ID,Order Date,Product Name,Quantity,Total Owed,Order Status,Shipment Status,ASIN,Currency,Unit Price
order-example,2026-08-19,Example Product,1,12.34,Closed,Delivered,ASIN-EXAMPLE,USD,12.34
`

test('@smoke Amazon profile imports, exposes item details, and repeats from r', async ({
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

    await page.keyboard.press('r')
    await expect(
      page.getByRole('heading', { name: 'Choose order-history CSV files' }),
    ).toBeVisible()
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
