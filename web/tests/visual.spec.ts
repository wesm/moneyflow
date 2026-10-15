import type { Locator, Page } from '@playwright/test'

import { cell, expect, openMoneyflow, test } from './fixtures'
import { startE2EServer } from '../scripts/e2e-server'

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
    await page.getByRole('searchbox', { name: 'Search transactions' }).fill('noodle')
    await expect(page).toHaveURL(/[?&]q=noodle(?:&|$)/)
    return
  }
  if (state === 'filters') {
    await page.keyboard.press('f')
    await expect(page.getByRole('button', { name: 'Show transfers' })).toBeVisible()
    return
  }
  await page.keyboard.press('?')
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

async function expectThemeContract(page: Page, theme: (typeof themes)[number]): Promise<void> {
  const state = await page.locator('html').evaluate((element) => {
    const body = getComputedStyle(document.body)
    const match = body.backgroundColor.match(/\d+(?:\.\d+)?/g)
    return {
      dark: element.classList.contains('dark'),
      kitTheme: element.getAttribute('data-kit-theme'),
      backgroundChannels: (match ?? []).slice(0, 3).map(Number),
    }
  })
  expect(state.dark).toBe(theme === 'dark')
  expect(state.kitTheme).toBe('moneyflow')
  expect(state.backgroundChannels).toHaveLength(3)
  const brightness = state.backgroundChannels.reduce((sum, channel) => sum + channel, 0)
  if (theme === 'dark') {
    expect(brightness).toBeLessThan(384)
  } else {
    expect(brightness).toBeGreaterThan(384)
  }
}

async function expectFullyWithinViewport(
  locator: Locator,
  viewport: (typeof viewports)[number],
): Promise<void> {
  await expect
    .poll(async () => {
      const box = await locator.boundingBox()
      return (
        box !== null &&
        box.x >= 0 &&
        box.y >= 0 &&
        box.x + box.width <= viewport.width + 1 &&
        box.y + box.height <= viewport.height + 1
      )
    })
    .toBe(true)
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
        await expectThemeContract(page, theme)
        if (state === 'help') {
          await expectFullyWithinViewport(page.getByRole('dialog'), viewport)
        } else if (state === 'filters') {
          for (const name of ['Filters', 'Show hidden transactions', 'Show transfers']) {
            await expectFullyWithinViewport(page.getByRole('button', { name }), viewport)
          }
        } else {
          // The grid is as tall as its result; the workspace that scrolls it must fit.
          const grid = page.getByRole('grid', { name: 'Financial results' })
          await expect(grid).toBeVisible()
          await expectFullyWithinViewport(page.locator('.mf-workspace'), viewport)
          if (viewport.name === 'narrow') {
            // Phone rows wrap onto two lines instead of scrolling sideways.
            const sideways = await grid.evaluate((element) => {
              const workspace = element.closest<HTMLElement>('.mf-workspace')!
              return workspace.scrollWidth - workspace.clientWidth
            })
            expect(sideways).toBe(0)
          }
        }
      })
    }
  }
}

/** The computed color of a theme custom property, such as `--mf-cursor`. */
async function themeColor(page: Page, property: string): Promise<string> {
  return await page.evaluate((name) => {
    const probe = document.createElement('div')
    probe.style.backgroundColor = `var(${name})`
    document.body.append(probe)
    const color = getComputedStyle(probe).backgroundColor
    probe.remove()
    return color
  }, property)
}

/** The computed shadow of a theme custom property, such as `--shadow-raised`. */
async function themeShadow(page: Page, property: string): Promise<string> {
  return await page.evaluate((name) => {
    const probe = document.createElement('div')
    probe.style.boxShadow = `var(${name})`
    document.body.append(probe)
    const shadow = getComputedStyle(probe).boxShadow
    probe.remove()
    return shadow
  }, property)
}

for (const theme of themes) {
  test(`${theme} controls, fields, and panels take their depth from the theme`, async ({
    page,
    server,
  }) => {
    await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), theme)
    await openMoneyflow(page, server)
    const raised = await themeShadow(page, '--shadow-raised')
    const sunken = await themeShadow(page, '--shadow-sunken')
    const panel = await themeShadow(page, '--shadow-panel')
    for (const shadow of [raised, sunken, panel]) expect(shadow).not.toBe('none')
    expect(raised, 'a raised control must differ from a sunken one').not.toBe(sunken)
    if (theme === 'dark') {
      const highlight = await themeColor(page, '--edge-highlight')
      expect(highlight, 'the dark theme sets its own edge highlight').not.toBe('rgb(255, 255, 255)')
    }
    const banner = page.getByRole('banner')
    await expect(banner.locator('.kit-filter-dropdown__btn')).toHaveCSS('box-shadow', raised)
    await expect(banner.locator('.kit-text-input')).toHaveCSS('box-shadow', sunken)
    const grid = page.getByRole('grid', { name: 'Financial results' })
    await expect(grid).toHaveCSS('box-shadow', panel)
  })

  test(`${theme} row actions stay flat while top-bar icon buttons are raised`, async ({
    page,
    server,
  }) => {
    await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), theme)
    const grid = page.getByRole('grid', { name: 'Financial results' })
    const rail = page.getByRole('complementary', { name: 'Moneyflow sidebar' })
    // Buttons repeated down a column: the detail view's pencils on a wide screen, the icon rail's
    // entries between, and the rows' own buttons on a phone.
    for (const [width, repeated] of [
      [1440, grid.getByRole('button', { name: 'Edit merchant' })],
      [1024, rail.getByRole('button', { name: 'Categories', exact: true })],
      [390, grid.getByRole('button', { name: /^Actions for / })],
    ] as const) {
      await page.setViewportSize({ width, height: 900 })
      await openMoneyflow(page, server)
      await page.keyboard.press('d')
      const raised = await themeShadow(page, '--shadow-raised')
      expect(raised, `${width} px`).not.toBe('none')
      const topBarButton = page.getByRole('button', { name: 'Previous period' })
      await expect(topBarButton, `${width} px`).toHaveCSS('box-shadow', raised)
      await expect(repeated.first(), `${width} px`).toHaveCSS('box-shadow', 'none')
    }
  })

  test(`${theme} review drawer table is not framed a second time`, async ({ page }) => {
    const server = await startE2EServer()
    try {
      await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), theme)
      await openMoneyflow(page, server)
      await page.keyboard.press('h')
      await expect(page.getByText(/1 pending edit/).first()).toBeAttached()
      await page.keyboard.press('w')
      const review = page.getByRole('dialog', { name: 'Review pending changes' })
      const wrapper = review.getByRole('table', { name: 'Affected transactions' }).locator('..')
      await expect(wrapper).toHaveCSS('box-shadow', 'none')
    } finally {
      await server.stop()
    }
  })
}

test('hover keeps the cursor and selection tints; hidden rows keep ☐ and flags bright', async ({
  page,
  server,
}) => {
  await openMoneyflow(page, server)
  const grid = page.getByRole('grid', { name: 'Financial results' })
  const row = (index: number) => grid.locator(`[role="row"][aria-rowindex="${index}"]`)
  const tint = (locator: Locator) =>
    locator.evaluate((element) => getComputedStyle(element).backgroundColor)
  await cell(row(4), 'select').click()
  await expect(row(4)).toHaveAttribute('aria-selected', 'true')

  await cell(row(2), 'count').hover()
  expect(await tint(row(2))).toBe(await themeColor(page, '--mf-cursor'))
  await cell(row(4), 'count').hover()
  expect(await tint(row(4))).toBe(await themeColor(page, '--mf-selected'))
  await cell(row(3), 'count').hover()
  expect(await tint(row(3))).toBe(await themeColor(page, '--bg-surface-hover'))

  await page.keyboard.press('d')
  await expect(grid.getByRole('columnheader', { name: 'Date' })).toBeVisible()
  await page.keyboard.press('/')
  await page.getByRole('searchbox', { name: 'Search transactions' }).fill('noodle')
  await expect(page).toHaveURL(/[?&]q=noodle(?:&|$)/)
  await page.keyboard.press('Enter')
  const hidden = grid.locator('.mf-table__row--hidden').first()
  await expect(hidden).toBeVisible()
  const opacity = (key: string) =>
    cell(hidden, key).evaluate((element) => Number(getComputedStyle(element).opacity))
  expect(await opacity('select')).toBe(1)
  expect(await opacity('flags')).toBe(1)
  expect(await opacity('date')).toBeLessThan(1)
})

test('a hidden phone row dims everything but its flags', async ({ page, server }) => {
  await page.setViewportSize(viewports[2])
  // The demo hides a few late-month Noodle House visits early in 2026.
  await openMoneyflow(page, server, 'cur=USD%3A2&from=2026-01-01&to=2026-03-31&v=1')
  const grid = page.getByRole('grid', { name: 'Financial results' })
  await page.keyboard.press('d')
  await expect(grid.locator('[data-column="merchant"]').first()).toBeVisible()
  await page.keyboard.press('/')
  await page.getByRole('searchbox', { name: 'Search transactions' }).fill('noodle')
  await expect(page).toHaveURL(/[?&]q=noodle(?:&|$)/)
  await page.keyboard.press('Enter')
  const hidden = grid.locator('.mf-table__row--hidden').first()
  await expect(hidden).toBeVisible()
  // The opacity a cell shows with, including any dimmed wrapper inside the row.
  const shown = (key: string) =>
    cell(hidden, key).evaluate((element) => {
      let opacity = 1
      for (
        let node: Element | null = element;
        node && node.role !== 'row';
        node = node.parentElement
      )
        opacity *= Number(getComputedStyle(node).opacity)
      return opacity
    })
  expect(await shown('flags')).toBe(1)
  for (const key of ['merchant', 'amount', 'date', 'category', 'account']) {
    expect(await shown(key), key).toBeLessThan(1)
  }
})

for (const theme of themes) {
  test(`chart drawer ${theme} narrow visual contract`, async ({ page, server }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), theme)
    await openMoneyflow(page, server)
    await page.getByRole('switch', { name: 'Charts' }).check()
    await expect(page.getByRole('dialog', { name: 'Moneyflow visualizations' })).toBeVisible()
    await expectLayoutContract(page, viewports[2])
    await expectThemeContract(page, theme)
    await expectFullyWithinViewport(
      page.getByRole('dialog', { name: 'Moneyflow visualizations' }),
      viewports[2],
    )
  })
}

for (const theme of themes) {
  for (const surface of [
    { opener: 'Open navigation', dialog: 'Navigation' },
    { opener: 'View', dialog: 'View' },
  ]) {
    test(`phone ${surface.dialog} ${theme} visual contract`, async ({ page, server }) => {
      await page.setViewportSize(viewports[2])
      await page.addInitScript((mode) => localStorage.setItem('kit-ui-theme', mode), theme)
      await openMoneyflow(page, server)
      await page.getByRole('button', { name: surface.opener, exact: true }).click()
      const dialog = page.getByRole('dialog', { name: surface.dialog })
      await expect(dialog).toBeVisible()
      await expectLayoutContract(page, viewports[2])
      await expectThemeContract(page, theme)
      await expectFullyWithinViewport(dialog, viewports[2])
    })
  }
}
