import { expect, test } from '@playwright/test'

import { startOnboardingE2EServer } from '../scripts/e2e-server'

test('w then Enter prepares, writes, and finalizes Monarch edits without extra ceremony', async ({
  page,
}) => {
  const server = await startOnboardingE2EServer('/moneyflow/')
  try {
    await page.goto(server.url)
    await page.getByRole('button', { name: /Add profile/ }).click()
    await page.getByRole('button', { name: /Monarch Money/ }).click()
    await page.getByLabel('Profile name').fill('Write Test')
    await page.getByRole('button', { name: 'Create profile' }).click()
    await page.getByRole('button', { name: 'Continue with USD / 2' }).click()
    await page.getByLabel('Monarch email').fill('user@example.test')
    await page.getByLabel('Monarch password').fill('synthetic-password')
    await page.getByLabel('TOTP secret').fill('JBSWY3DPEHPK3PXP')
    await page
      .getByLabel('Moneyflow account password', { exact: true })
      .fill('synthetic-vault-password')
    await page.getByLabel('Confirm Moneyflow account password').fill('synthetic-vault-password')
    await page.getByRole('button', { name: 'Connect' }).click()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()

    await page.keyboard.press('h')
    await expect(page.getByText(/1 pending/)).toBeVisible()
    await page.keyboard.press('w')
    const review = page.getByRole('dialog', { name: 'Review pending changes' })
    await expect(review).toBeVisible()
    await page.keyboard.press('Enter')

    const write = page.getByRole('dialog', { name: 'Monarch write status' })
    await expect(write).toBeVisible()
    await expect(write.getByRole('status')).toContainText(/Provider write|Writing pending/)
    await expect(page.getByText(/0 pending/)).toBeVisible({ timeout: 10_000 })
    await expect(write.getByRole('status')).toContainText('Provider write complete')
  } finally {
    await server.stop()
  }
})
