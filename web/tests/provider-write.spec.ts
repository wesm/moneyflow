import { expect, test } from '@playwright/test'

import { startOnboardingE2EServer } from '../scripts/e2e-server'

test('w then Enter prepares, writes, and finalizes Monarch edits without extra ceremony', async ({
  page,
}) => {
  const server = await startOnboardingE2EServer('/moneyflow/')
  const syntheticPassword = 'synthetic-password'
  const syntheticVaultPassword = 'synthetic-vault-password'
  const syntheticTotpSecret = 'JBSWY3DPEHPK3PXP'
  const consoleMessages: string[] = []
  page.on('console', (message) => consoleMessages.push(message.text()))
  try {
    await page.goto(server.url)
    await page.getByRole('button', { name: /Add profile/ }).click()
    await page.getByRole('button', { name: /Monarch Money/ }).click()
    await page.getByLabel('Profile name').fill('Write Test')
    await page.getByRole('button', { name: 'Create profile' }).click()
    await page.getByRole('button', { name: 'Continue with USD / 2' }).click()
    await page.getByLabel('Monarch email').fill('user@example.test')
    await page.getByLabel('Monarch password').fill(syntheticPassword)
    await page.getByLabel('TOTP secret').fill(syntheticTotpSecret)
    await page
      .getByLabel('Moneyflow account password', { exact: true })
      .fill(syntheticVaultPassword)
    await page.getByLabel('Confirm Moneyflow account password').fill(syntheticVaultPassword)
    await page.getByRole('button', { name: 'Connect' }).click()
    await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()

    await page.keyboard.press('h')
    await expect(page.getByText(/1 pending edit/)).toBeVisible()
    await page.keyboard.press('w')
    const review = page.getByRole('dialog', { name: 'Review pending changes' })
    await expect(review).toBeVisible()
    const drawer = await review.elementHandle()
    await page.keyboard.press('Enter')

    // The same drawer turns into write status, with focus still inside it.
    const write = page.getByRole('dialog', { name: 'Monarch write status' })
    await expect(write).toBeVisible()
    await expect(review).toHaveCount(0)
    expect(await drawer?.evaluate((node) => node.isConnected)).toBe(true)
    expect(await write.evaluate((node) => node.contains(document.activeElement))).toBe(true)
    await expect(write.getByRole('button', { name: 'Commit reviewed changes' })).toHaveCount(0)
    await expect(write.getByRole('status')).toContainText(/Provider write|Writing pending/)
    await expect(page.getByText(/0 pending edit/)).toBeVisible({ timeout: 10_000 })
    await expect(write.getByRole('status')).toContainText('Provider write complete')
    const exposed = [await page.locator('body').innerText(), page.url(), ...consoleMessages].join(
      '\n',
    )
    for (const forbidden of [syntheticPassword, syntheticVaultPassword, syntheticTotpSecret]) {
      expect(exposed).not.toContain(forbidden)
    }
  } finally {
    await server.stop()
  }
})
