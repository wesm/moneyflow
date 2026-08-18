import { expect, test, type Page } from '@playwright/test'

import { startOnboardingE2EServer } from '../scripts/e2e-server'

test('two profile tabs keep routes, data, and revisions independent', async ({ browser }) => {
  const server = await startOnboardingE2EServer()
  try {
    const context = await browser.newContext()
    const firstID = await createConnectedProfile(await context.newPage(), server.url, 'Alpha')
    const secondID = await createConnectedProfile(await context.newPage(), server.url, 'Bravo')
    const first = await context.newPage()
    const second = await context.newPage()

    await first.goto(`${server.url}p/${firstID}/`)
    await second.goto(`${server.url}p/${secondID}/`)
    await expect(first.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    await expect(second.getByRole('grid', { name: 'Financial results' })).toBeFocused()
    await first.keyboard.press('h')

    await expect(first.getByText(/1 pending/)).toBeVisible()
    await expect(second.getByText(/0 pending/)).toBeVisible()
    await expect(first).toHaveURL(new RegExp(`/p/${firstID}/`))
    await expect(second).toHaveURL(new RegExp(`/p/${secondID}/`))
    await context.close()
  } finally {
    await server.stop()
  }
})

test('recovery evicts the cached service before recreating the profile', async ({ page }) => {
  const server = await startOnboardingE2EServer('/', { recoveryProfile: true })
  try {
    await page.goto(server.url)
    await page.getByText('Recovery Profile').click()
    await expect(page.getByRole('heading', { name: /recovery/i })).toBeVisible()
    await page.getByRole('button', { name: /recreate/i }).click()
    await expect(page.getByRole('heading', { name: 'Confirm import settings' })).toBeVisible()
    expect(await server.logs()).not.toContain('profile is currently in use')
  } finally {
    await server.stop()
  }
})

test('session expiry enters reconnect without losing analytical URL or cursor state', async ({
  page,
}) => {
  const server = await startOnboardingE2EServer()
  try {
    const profileID = await createConnectedProfile(page, server.url, 'Reconnect')
    await page.goto(`${server.url}p/${profileID}/?group=category&v=1`)
    const grid = page.getByRole('grid', { name: 'Financial results' })
    await expect(grid).toBeFocused()
    await page.keyboard.press('j')
    const activeBefore = await grid.getAttribute('aria-activedescendant')
    expect(activeBefore).toBeTruthy()
    const urlBefore = page.url()

    await server.expire(profileID)
    await page.keyboard.press('r')
    await expect(page.getByRole('button', { name: 'Reconnect provider' })).toBeVisible()
    await page.getByRole('button', { name: 'Reconnect provider' }).click()
    await expect(
      page.getByRole('heading', { name: /Unlock saved credentials|Connect Monarch/ }),
    ).toBeVisible()
    await page.getByRole('button', { name: 'Cancel' }).click()
    await expect(grid).toBeFocused()
    expect(page.url()).toBe(urlBefore)
    expect(await grid.getAttribute('aria-activedescendant')).toBe(activeBefore)
  } finally {
    await server.stop()
  }
})

async function createConnectedProfile(page: Page, baseURL: string, name: string): Promise<string> {
  await page.goto(baseURL)
  await page.keyboard.press('a')
  await page.keyboard.press('m')
  await page.getByLabel('Profile name').fill(name)
  await page.getByLabel('Profile name').press('Enter')
  await expect(page.getByRole('heading', { name: 'Confirm import settings' })).toBeVisible()
  await page.getByRole('button', { name: 'Continue with USD / 2' }).click()
  await expect(page.getByRole('heading', { name: 'Connect Monarch Money' })).toBeVisible()
  await page.getByLabel('Monarch email').fill(`${name.toLowerCase()}@example.test`)
  await page.getByLabel('Monarch password').fill('synthetic-password')
  await page.getByLabel('TOTP secret').fill('JBSWY3DPEHPK3PXP')
  await page
    .getByLabel('Moneyflow account password', { exact: true })
    .fill('synthetic-vault-password')
  await page.getByLabel('Confirm Moneyflow account password').fill('synthetic-vault-password')
  await page.getByRole('button', { name: 'Connect' }).click()
  await expect(page.getByRole('grid', { name: 'Financial results' })).toBeFocused()
  const match = /\/p\/(profile_[a-z2-7]{26})\//.exec(page.url())
  if (!match?.[1]) throw new Error('Onboarding did not reach a canonical profile route.')
  return match[1]
}
