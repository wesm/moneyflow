import type { Page } from '@playwright/test'

import { expect, openMoneyflow, test } from './fixtures'

test.skip(({ browserName }) => browserName !== 'chromium', 'Layout contracts are Chromium-only.')

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

async function expectLayoutContract(
  page: Page,
  viewport: (typeof viewports)[number],
): Promise<void> {
  const body = page.locator('body')
  const main = page.locator('main').last()
  await expect(main).toBeVisible()
  const metrics = await body.evaluate((element) => {
    const style = getComputedStyle(element)
    const mainElements = document.querySelectorAll('main')
    const mainElement = mainElements.item(mainElements.length - 1)
    const mainBox = mainElement?.getBoundingClientRect()
    return {
      scrollWidth: element.scrollWidth,
      clientWidth: element.clientWidth,
      background: style.backgroundColor,
      color: style.color,
      fontFamily: style.fontFamily,
      mainLeft: mainBox?.left ?? -1,
      mainRight: mainBox?.right ?? Number.MAX_SAFE_INTEGER,
      mainTop: mainBox?.top ?? -1,
      mainBottom: mainBox?.bottom ?? Number.MAX_SAFE_INTEGER,
    }
  })
  expect(metrics.scrollWidth).toBeLessThanOrEqual(metrics.clientWidth)
  expect(metrics.background).not.toBe('rgba(0, 0, 0, 0)')
  expect(metrics.color).not.toBe('rgba(0, 0, 0, 0)')
  expect(metrics.fontFamily).not.toBe('')
  expect(metrics.mainLeft).toBeGreaterThanOrEqual(0)
  expect(metrics.mainRight).toBeLessThanOrEqual(viewport.width)
  expect(metrics.mainTop).toBeGreaterThanOrEqual(0)
  expect(metrics.mainBottom).toBeLessThanOrEqual(viewport.height + 1)
}

for (const viewport of viewports) {
  for (const theme of themes) {
    for (const state of states) {
      test(`${state} ${theme} ${viewport.name} visual contract`, async ({ page, server }) => {
        await page.setViewportSize(viewport)
        await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), theme)
        await openMoneyflow(page, server)
        await prepareState(page, state)
        await expectLayoutContract(page, viewport)
        if (state === 'filters' || state === 'help') {
          await expect(page.getByRole('dialog')).toBeInViewport()
        } else {
          await expect(page.getByRole('grid', { name: 'Financial results' })).toBeInViewport()
        }
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
    await expectLayoutContract(page, viewports[2])
    await expect(page.getByRole('dialog', { name: 'Moneyflow visualizations' })).toBeInViewport()
  })
}
