import type { Page } from '@playwright/test'

import { expect, openMoneyflow, test } from './fixtures'

const viewports = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'medium', width: 1024, height: 768 },
  { name: 'narrow', width: 390, height: 844 },
] as const
const themes = ['light', 'dark'] as const
const states = ['aggregate', 'detail', 'search', 'filters', 'help'] as const

async function prepareState(page: Page, state: (typeof states)[number]): Promise<void> {
  if (state === 'aggregate') return
  if (state === 'detail') {
    await page.keyboard.press('d')
    await expect(page.getByRole('columnheader', { name: 'Date' })).toBeVisible()
    return
  }
  if (state === 'search') {
    await page.keyboard.press('/')
    await page.getByRole('searchbox', { name: 'Search transactions' }).fill('grocer')
    await expect(page.getByRole('navigation', { name: 'Active refinements' })).toContainText(
      'Search: grocer',
    )
    return
  }
  await page.keyboard.press(state === 'filters' ? 'f' : '?')
  await expect(page.getByRole('dialog')).toBeVisible()
}

for (const viewport of viewports) {
  for (const theme of themes) {
    for (const state of states) {
      test(`${state} ${theme} ${viewport.name} visual contract`, async ({ page, server }) => {
        await page.setViewportSize(viewport)
        await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), theme)
        await openMoneyflow(page, server)
        await prepareState(page, state)
        await expect(page).toHaveScreenshot(`${state}-${theme}-${viewport.name}.png`)
      })
    }
  }
}

for (const theme of themes) {
  test(`chart drawer ${theme} narrow visual contract`, async ({ page, server }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), theme)
    await openMoneyflow(page, server)
    await page.getByRole('switch', { name: 'Charts' }).check()
    await expect(page.getByRole('dialog', { name: 'Moneyflow visualizations' })).toBeVisible()
    await expect(page).toHaveScreenshot(`chart-drawer-${theme}-narrow.png`)
  })
}
