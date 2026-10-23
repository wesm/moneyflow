import { expect, test } from '@playwright/test'

import { cell } from './fixtures'
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
    await page.getByLabel('Authenticator setup key').fill('JBSWY3DPEHPK3PXP')
    await page.getByLabel('Account password', { exact: true }).fill('synthetic-vault-password')
    await page.getByLabel('Confirm account password').fill('synthetic-vault-password')
    await page.getByRole('button', { name: 'Connect' }).press('Enter')

    await expect(page.getByText(/of 4 visible/)).toBeVisible()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    await expect(page).toHaveURL(/\/moneyflow\/p\/profile_[a-z2-7]{26}\/\?cur=USD%3A2&v=1$/)
    const merchant = page.getByRole('row', { name: /Example Merchant/ })
    await expect(merchant).toBeVisible()
    await expect(cell(merchant, 'count')).toHaveText('4')
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

test('@smoke YNAB setup, category clear, w Enter, and refresh preserve committed edits', async ({
  page,
}) => {
  const server = await startOnboardingE2EServer('/moneyflow/')
  try {
    await page.goto(server.url)
    await page.getByRole('button', { name: /Add profile/ }).click()
    await page.getByRole('button', { name: /^YNAB/ }).click()
    await page.getByLabel('Profile name').fill('YNAB Profile')
    await page.getByRole('button', { name: 'Create profile' }).click()

    await expect(page.getByRole('heading', { name: 'Connect YNAB' })).toBeVisible()
    const token = page.getByLabel('YNAB personal access token')
    await expect(token).toHaveAttribute('type', 'password')
    await token.fill('synthetic-token')
    await page.getByLabel('Account password', { exact: true }).fill('vault-password')
    await page.getByLabel('Confirm account password').fill('vault-password')
    await page.getByRole('button', { name: 'Connect' }).click()

    await expect(page.getByRole('heading', { name: 'Choose a YNAB budget' })).toBeVisible()
    await page.getByRole('button', { name: /Example Budget/ }).press('Enter')
    await expect(page.getByRole('heading', { name: 'Confirm import settings' })).toBeVisible()
    await expect(page.getByLabel('Currency')).toHaveValue('USD')
    await expect(page.getByLabel('Currency')).toHaveAttribute('readonly', '')
    await expect(page.getByLabel('Decimal places')).toHaveValue('2')
    await page.getByRole('button', { name: 'Continue with USD / 2' }).press('Enter')

    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    await expect(page).toHaveURL(/\/moneyflow\/p\/profile_[a-z2-7]{26}\/\?cur=USD%3A2&v=1$/)
    await expect(page.getByRole('row', { name: /Example Payee/ })).toBeVisible()
    await expect(page).not.toHaveURL(/synthetic-token|vault-password/)
    const browserStorage = await page.evaluate(() => ({
      local: JSON.stringify(localStorage),
      session: JSON.stringify(sessionStorage),
      history: JSON.stringify(history.state),
    }))
    expect(JSON.stringify(browserStorage)).not.toContain('synthetic-token')
    expect(JSON.stringify(browserStorage)).not.toContain('vault-password')

    await page.keyboard.press('c')
    const category = page.getByRole('dialog', { name: 'Change category' })
    const field = category.getByRole('combobox', { name: 'Category' })
    await field.fill('Uncategorized')
    await field.press('Enter')
    await expect(category).toBeHidden()
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await page.keyboard.press('w')
    await expect(page.getByRole('dialog', { name: 'Review pending changes' })).toBeVisible()
    await page.keyboard.press('Enter')
    await expect(page.getByText(/0 pending edit/)).toBeVisible()
    await expect(page.getByRole('status', { name: 'Notice' })).toHaveText(
      /^Saved [1-9]\d* changes? to YNAB\.$/,
    )
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await page.keyboard.press('d')
    await expect(page.getByRole('row', { name: /Uncategorized/ })).toBeVisible()
    await page.keyboard.press('r')
    await expect(page.getByRole('row', { name: /Uncategorized/ })).toBeVisible()
  } finally {
    await server.stop()
  }
})
