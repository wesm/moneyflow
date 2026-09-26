import { expect, test } from '@playwright/test'
import { startOnboardingE2EServer } from '../scripts/e2e-server'

test('SimpleFIN imports through TLS and persists local merchant edits', async ({
  page,
}, testInfo) => {
  const server = await startOnboardingE2EServer('/moneyflow/')
  try {
    await page.goto(server.url)
    await page.keyboard.press('a')
    await page.keyboard.press('s')
    await page.getByLabel('Profile name').fill('Bank Import')
    await page.getByLabel('Profile name').press('Enter')
    await expect(page.getByLabel('Currency')).toBeFocused()
    await page.getByRole('button', { name: 'Continue', exact: true }).click()
    const input = page.getByLabel('Setup token or Access URL')
    await expect(input).toBeFocused()
    await page.screenshot({ path: testInfo.outputPath('simplefin-desktop.png') })
    await page.setViewportSize({ width: 390, height: 844 })
    await page.screenshot({ path: testInfo.outputPath('simplefin-mobile.png') })
    await page.setViewportSize({ width: 1440, height: 900 })
    await input.fill('synthetic-simplefin-normal')
    await input.press('Enter')
    const grid = page.getByRole('grid', { name: 'Financial results' })
    await expect(grid).toBeFocused()
    await page.keyboard.press('m')
    await page.getByLabel('Merchant name').fill('Local Merchant')
    await page.getByLabel('Merchant name').press('Enter')
    await expect(grid).toBeFocused()
    await page.keyboard.press('w')
    await expect(page.getByRole('dialog', { name: 'Review pending changes' })).toBeVisible()
    await page.keyboard.press('Enter')
    await expect(page.getByText('Changes saved in Moneyflow.')).toBeVisible()
    await page.reload()
    await expect(grid).toContainText('Local Merchant')
    const counts = await (
      await page.request.get(`${server.origin}/__moneyflow_test/simplefin`)
    ).json()
    expect(counts.claims).toBe(1)
    expect(counts.writes).toBe(0)
  } finally {
    await server.stop()
  }
})

for (const mode of ['empty', 'retry']) {
  test(`SimpleFIN ${mode} import keeps its saved connection`, async ({ page }) => {
    const server = await startOnboardingE2EServer('/moneyflow/')
    try {
      await page.goto(server.url)
      await page.keyboard.press('a')
      await page.keyboard.press('s')
      await page.getByLabel('Profile name').fill('Import Example')
      await page.getByLabel('Profile name').press('Enter')
      await page.getByRole('button', { name: 'Continue', exact: true }).click()
      await page.getByLabel('Setup token or Access URL').fill(`synthetic-simplefin-${mode}`)
      await page.getByLabel('Setup token or Access URL').press('Enter')
      if (mode === 'retry') {
        await expect(page.getByRole('heading', { name: 'Setup needs attention' })).toBeVisible()
        await page.request.post(`${server.origin}/__moneyflow_test/simplefin/advance`)
        await page.getByRole('button', { name: 'Retry', exact: true }).click()
      }
      await expect(page).toHaveURL(/\/p\/profile_[a-z2-7]{26}\//)
      await page.reload()
      if (mode === 'empty')
        await expect(page.getByText('No transactions', { exact: true })).toBeVisible()
      else await expect(page.getByRole('grid', { name: 'Financial results' })).toBeVisible()
      const counts = await (
        await page.request.get(`${server.origin}/__moneyflow_test/simplefin`)
      ).json()
      expect(counts.claims).toBe(1)
    } finally {
      await server.stop()
    }
  })
}
