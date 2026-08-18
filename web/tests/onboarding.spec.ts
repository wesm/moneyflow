import { expect, test } from '@playwright/test'

import { startOnboardingE2EServer } from '../scripts/e2e-server'

test('keyboard onboarding creates, connects, imports, and opens one profile', async ({ page }) => {
  const server = await startOnboardingE2EServer('/moneyflow/')
  try {
    await page.goto(server.url)
    await expect(page.getByRole('heading', { name: 'Choose a Moneyflow profile' })).toBeVisible()

    await page.keyboard.press('a')
    await expect(page.getByRole('heading', { name: 'Choose a provider' })).toBeVisible()
    await page.keyboard.press('m')
    const name = page.getByLabel('Profile name')
    await expect(name).toBeFocused()
    await name.pressSequentially('Household')
    await name.press('Enter')

    await expect(page.getByRole('heading', { name: 'Confirm import settings' })).toBeVisible()
    await page.getByRole('button', { name: 'Continue with USD / 2' }).press('Enter')
    await expect(page.getByRole('heading', { name: 'Connect Monarch Money' })).toBeVisible()
    await page.getByLabel('Monarch email').fill('user@example.test')
    await page.getByLabel('Monarch password').fill('synthetic-password')
    await page.getByLabel('TOTP secret').fill('JBSWY3DPEHPK3PXP')
    await page
      .getByLabel('Moneyflow account password', { exact: true })
      .fill('synthetic-vault-password')
    await page.getByLabel('Confirm Moneyflow account password').fill('synthetic-vault-password')
    await page.getByRole('button', { name: 'Connect' }).press('Enter')

    await expect(page.getByText(/of 4 visible/)).toBeVisible()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    await expect(page).toHaveURL(/\/moneyflow\/p\/profile_[a-z2-7]{26}\/(?:\?v=1)?$/)
    await expect(page.getByRole('row', { name: /Example Merchant 4/ })).toBeVisible()
  } finally {
    await server.stop()
  }
})

test('canceling a newly added profile leaves no orphan in the catalog', async ({ page }) => {
  const server = await startOnboardingE2EServer('/moneyflow/')
  try {
    await page.goto(server.url)
    await page.getByRole('button', { name: /Add profile/ }).click()
    await page.getByRole('button', { name: /Monarch Money/ }).click()
    await page.getByLabel('Profile name').fill('Canceled Setup')
    await page.getByRole('button', { name: 'Create profile' }).click()
    await expect(page.getByRole('heading', { name: 'Confirm import settings' })).toBeVisible()

    await page.getByRole('button', { name: 'Cancel' }).click()

    await expect(page.getByRole('heading', { name: 'Choose a Moneyflow profile' })).toBeVisible()
    await expect(page.getByRole('button', { name: /Canceled Setup/ })).toHaveCount(0)
  } finally {
    await server.stop()
  }
})
